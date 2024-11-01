// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prober

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	pconfig "github.com/prometheus/common/config"

	"github.com/prometheus/blackbox_exporter/config"
)

var c = &config.Config{
	Modules: map[string]config.Module{
		"http_2xx": {
			Prober:  "http",
			Timeout: 10 * time.Second,
			HTTP: config.HTTPProbe{
				HTTPClientConfig: pconfig.HTTPClientConfig{
					BearerToken: "mysecret",
				},
			},
		},
	},
}

func TestPrometheusTimeoutHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", "?target="+ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", "1")

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	})

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("probe request handler returned wrong status code: %v, want %v", status, http.StatusOK)
	}
}

func TestPrometheusConfigSecretsHidden(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer ts.Close()

	req, err := http.NewRequest("GET", "?debug=true&target="+ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	})
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "mysecret") {
		t.Errorf("Secret exposed in debug config output: %v", body)
	}
	if !strings.Contains(body, "<secret>") {
		t.Errorf("Hidden secret missing from debug config output: %v", body)
	}
}

func TestDebugOutputSecretsHidden(t *testing.T) {
	module := c.Modules["http_2xx"]
	out := DebugOutput(&module, &bytes.Buffer{}, prometheus.NewRegistry())

	if strings.Contains(out, "mysecret") {
		t.Errorf("Secret exposed in debug output: %v", out)
	}
	if !strings.Contains(out, "<secret>") {
		t.Errorf("Hidden secret missing from debug output: %v", out)
	}
}

func TestTimeoutIsSetCorrectly(t *testing.T) {
	var tests = []struct {
		inModuleTimeout     time.Duration
		inPrometheusTimeout string
		inOffset            float64
		outTimeout          float64
	}{
		{0 * time.Second, "15", 0.5, 14.5},
		{0 * time.Second, "15", 0, 15},
		{20 * time.Second, "15", 0.5, 14.5},
		{20 * time.Second, "15", 0, 15},
		{5 * time.Second, "15", 0, 5},
		{5 * time.Second, "15", 0.5, 5},
		{10 * time.Second, "", 0.5, 10},
		{10 * time.Second, "10", 0.5, 9.5},
		{9500 * time.Millisecond, "", 0.5, 9.5},
		{9500 * time.Millisecond, "", 1, 9.5},
		{0 * time.Second, "", 0.5, 119.5},
		{0 * time.Second, "", 0, 120},
	}

	for _, v := range tests {
		request, _ := http.NewRequest("GET", "", nil)
		request.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", v.inPrometheusTimeout)
		module := config.Module{
			Timeout: v.inModuleTimeout,
		}

		timeout, _ := getTimeout(request, module, v.inOffset)
		if timeout != v.outTimeout {
			t.Errorf("timeout is incorrect: %v, want %v", timeout, v.outTimeout)
		}
	}
}

func TestHostnameParam(t *testing.T) {
	headers := map[string]string{}
	c := &config.Config{
		Modules: map[string]config.Module{
			"http_2xx": {
				Prober:  "http",
				Timeout: 10 * time.Second,
				HTTP: config.HTTPProbe{
					Headers:            headers,
					IPProtocolFallback: true,
				},
			},
		},
	}

	// check that 'hostname' parameter make its way to Host header
	hostname := "foo.example.com"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != hostname {
			t.Errorf("Unexpected Host: expected %q, got %q.", hostname, r.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	requrl := fmt.Sprintf("?debug=true&hostname=%s&target=%s", hostname, ts.URL)

	req, err := http.NewRequest("GET", requrl, nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	})

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("probe request handler returned wrong status code: %v, want %v", status, http.StatusOK)
	}

	// check that ts got the request to perform header check
	if !strings.Contains(rr.Body.String(), "probe_success 1") {
		t.Errorf("probe failed, response body: %v", rr.Body.String())
	}

	// check that host header both in config and in parameter will result in 400
	c.Modules["http_2xx"].HTTP.Headers["Host"] = hostname + ".something"

	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	})

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("probe request handler returned wrong status code: %v, want %v", status, http.StatusBadRequest)
	}
}

func TestTCPHostnameParam(t *testing.T) {
	c := &config.Config{
		Modules: map[string]config.Module{
			"tls_connect": {
				Prober:  "tcp",
				Timeout: 10 * time.Second,
				TCP: config.TCPProbe{
					TLS:        true,
					IPProtocol: "ip4",
					TLSConfig:  pconfig.TLSConfig{InsecureSkipVerify: true},
				},
			},
		},
	}

	// check that 'hostname' parameter make its way to server_name in the tls_config
	hostname := "foo.example.com"

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != hostname {
			t.Errorf("Unexpected Host: expected %q, got %q.", hostname, r.Host)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	requrl := fmt.Sprintf("?module=tls_connect&debug=true&hostname=%s&target=%s", hostname, ts.Listener.Addr().(*net.TCPAddr).IP.String()+":"+strconv.Itoa(ts.Listener.Addr().(*net.TCPAddr).Port))

	req, err := http.NewRequest("GET", requrl, nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	})

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("probe request handler returned wrong status code: %v, want %v", status, http.StatusOK)
	}

	// check debug output to confirm the server_name is set in tls_config and matches supplied hostname
	if !strings.Contains(rr.Body.String(), "server_name: "+hostname) {
		t.Errorf("probe failed, response body: %v", rr.Body.String())
	}

}

func TestJsonrpcModuleProbeParams(t *testing.T) {
	// Start a local geth node for testing
	gethCmd := exec.Command("anvil",
		"--host", "127.0.0.1",
		"--port", "58545", // Default HTTP-RPC port
	)

	// Start geth in background
	if err := gethCmd.Start(); err != nil {
		t.Fatalf("Failed to start geth: %v", err)
	}

	// Ensure geth is stopped after test
	defer func() {
		if err := gethCmd.Process.Kill(); err != nil {
			t.Errorf("Failed to kill geth process: %v", err)
		}
	}()

	// Load test config
	c = &config.Config{
		Modules: map[string]config.Module{
			"jsonrpc": {
				Prober:  "jsonrpc",
				Timeout: 10 * time.Second,
			},
		},
	}

	// Start test prometheus server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	}))
	defer ts.Close()

	// Wait for geth to be ready
	time.Sleep(2 * time.Second)

	wantResultRegex := "probe_jsonrpc{method=.*,params=.*,rpc=.*,tag=.*} .*"
	tests := []struct {
		name       string
		reqURL     string
		wantStatus int
	}{
		{
			name: "eth_call contract owner address",
			reqURL: url.Values{
				"arg": []string{
					"{to:0x3c3a81e81dc49a522a592e7622a7e711c06bf354,data:0x8da5cb5b},latest",
					"{to:0x3c3a81e81dc49a522a592e7622a7e711c06bf354,data:0x8da5cb5b},latest",
				},
				"disableBatch":   []string{"true"},
				"decimal":        []string{"40", "40"},
				"method":         []string{"eth_call", "eth_call"},
				"module":         []string{"jsonrpc", "jsonrpc"},
				"resultJMESPath": []string{" ", " "},
				"tag":            []string{"MntOwner1", "MntOwner2"},
				"target":         []string{"https://rpc.ankr.com/eth", "https://1rpc.io/eth"},
			}.Encode(),
			wantStatus: http.StatusOK,
		},
		{
			name: "eth_call contract owner address with batch",
			reqURL: url.Values{
				"arg": []string{
					"{to:0x3c3a81e81dc49a522a592e7622a7e711c06bf354,data:0x8da5cb5b},latest",
					"{to:0x3c3a81e81dc49a522a592e7622a7e711c06bf354,data:0x8da5cb5b},latest",
				},
				"disableBatch":   []string{"false"},
				"decimal":        []string{"40", "40"},
				"method":         []string{"eth_call", "eth_call"},
				"module":         []string{"jsonrpc", "jsonrpc"},
				"resultJMESPath": []string{" ", " "},
				"tag":            []string{"MntOwner1", "MntOwner2"},
				"target":         []string{"https://rpc.ankr.com/eth", "https://1rpc.io/eth"},
			}.Encode(),
			wantStatus: http.StatusOK,
		},
		{
			name: "eth_getBlockByNumber with resultJMESPath",
			reqURL: url.Values{
				"arg":            []string{"safe,false"},
				"decimal":        []string{"0"},
				"disableBatch":   []string{"false"},
				"method":         []string{"eth_getBlockByNumber"},
				"module":         []string{"jsonrpc"},
				"resultJMESPath": []string{"number"},
				"tag":            []string{"SafeBlockNumber"},
				"target":         []string{"https://rpc.ankr.com/eth"},
			}.Encode(),
			wantStatus: http.StatusOK,
		},
		{
			name: "eth_getBlockByNumber local test",
			reqURL: url.Values{
				"arg":            []string{"safe,false"},
				"decimal":        []string{"0"},
				"disableBatch":   []string{"true"},
				"method":         []string{"eth_getBlockByNumber"},
				"module":         []string{"jsonrpc"},
				"resultJMESPath": []string{"number"},
				"tag":            []string{"SafeBlockNumber"},
				"target":         []string{"http://localhost:58545"},
			}.Encode(),
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// Define expected result pattern
			req, err := http.NewRequest("GET", "?"+tt.reqURL, nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
			})

			handler.ServeHTTP(rr, req)

			fmt.Println(rr.Body.String())

			if status := rr.Code; status != tt.wantStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tt.wantStatus)
			}

			matched, err := regexp.MatchString(wantResultRegex, rr.Body.String())
			if err != nil {
				t.Fatalf("Failed to match regex: %v", err)
			}

			if !matched {
				t.Errorf("Response body does not match expected pattern.\nGot: %s\nWant regex: %s",
					rr.Body.String(), wantResultRegex)
			}
		})
	}
}

func TestEthRpcModuleProbeParams(t *testing.T) {
	// Load test config
	c = &config.Config{
		Modules: map[string]config.Module{
			"contract_call": {
				Prober:  "ethrpc",
				Timeout: 10 * time.Second,
			},
			"chain_info": {
				Prober:  "ethrpc",
				Timeout: 10 * time.Second,
			},
			"balance": {
				Prober:  "ethrpc",
				Timeout: 10 * time.Second,
			},
			"erc20balance": {
				Prober:  "ethrpc",
				Timeout: 10 * time.Second,
			},
		},
	}

	// Start test prometheus server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
	}))
	defer ts.Close()

	// Create ABI methods
	ownerMethod := ABIMethod{
		Name: "owner",
		Type: "function",
		Outputs: []ABIOutput{{
			Name: "",
			Type: "address",
		}},
	}

	balanceOfMethod := ABIMethod{
		Name: "balanceOf",
		Type: "function",
		Inputs: []ABIInput{{
			Name: "",
			Type: "address",
		}},
		Outputs: []ABIOutput{{
			Name: "",
			Type: "int256",
		}},
	}

	// Marshal to JSON
	ownerABI, err := json.Marshal([]ABIMethod{ownerMethod})
	if err != nil {
		t.Fatalf("Failed to marshal owner ABI: %v", err)
	}

	balanceOfABI, err := json.Marshal([]ABIMethod{balanceOfMethod})
	if err != nil {
		t.Fatalf("Failed to marshal balanceOf ABI: %v", err)
	}

	tests := []struct {
		name            string
		reqURL          string
		wantStatus      int
		wantResultRegex string
	}{
		{
			name: "contract_call owner test",
			reqURL: url.Values{
				"module": []string{"contract_call"},
				"target": []string{"https://rpc.ankr.com/eth"},
				"call":   []string{fmt.Sprintf("MntToken|0x3c3a81e81dc49a522a592e7622a7e711c06bf354|%s", string(ownerABI))},
			}.Encode(),
			wantStatus:      http.StatusOK,
			wantResultRegex: "probe_ethrpc_contract_call{.*} .*",
		},
		{
			name: "contract_call balanceOf test",
			reqURL: url.Values{
				"module": []string{"contract_call"},
				"target": []string{"https://rpc.ankr.com/eth"},
				"call":   []string{fmt.Sprintf("MntToken|0x3c3a81e81dc49a522a592e7622a7e711c06bf354|%s|0x207E804758e28F2b3fD6E4219671B327100b82f8", string(balanceOfABI))},
			}.Encode(),
			wantStatus:      http.StatusOK,
			wantResultRegex: "probe_ethrpc_contract_call{.*} .*",
		},
		{
			name: "chain_info test",
			reqURL: url.Values{
				"module": []string{"chain_info"},
				"target": []string{"https://rpc.ankr.com/eth"},
			}.Encode(),
			wantStatus:      http.StatusOK,
			wantResultRegex: "probe_ethrpc_block_number{.*} .*",
		},
		{
			name: "balance test",
			reqURL: url.Values{
				"module":  []string{"balance"},
				"target":  []string{"https://rpc.ankr.com/eth"},
				"account": []string{"deployer2:0x207E804758e28F2b3fD6E4219671B327100b82f8", "deployer3:0x207E804758e28F2b3fD6E4219671B327100b82f8"},
			}.Encode(),
			wantStatus:      http.StatusOK,
			wantResultRegex: "probe_ethrpc_balance{.*} .*",
		},
		{
			name: "erc20balance test",
			reqURL: url.Values{
				"module":  []string{"erc20balance"},
				"target":  []string{"https://rpc.ankr.com/eth"},
				"token":   []string{"0x3c3a81e81dc49a522a592e7622a7e711c06bf354"},
				"symbol":  []string{"MNT"},
				"account": []string{"deployer2:0x207E804758e28F2b3fD6E4219671B327100b82f8", "deployer3:0x207E804758e28F2b3fD6E4219671B327100b82f8"},
			}.Encode(),
			wantStatus:      http.StatusOK,
			wantResultRegex: "probe_ethrpc_erc20balance{.*} .*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "?"+tt.reqURL, nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Handler(w, r, c, log.NewNopLogger(), &ResultHistory{}, 0.5, nil, nil, level.AllowNone())
			})

			handler.ServeHTTP(rr, req)

			fmt.Println("http://localhost:9115/probe" + req.URL.String())
			fmt.Println(rr.Body.String())

			if status := rr.Code; status != tt.wantStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tt.wantStatus)
			}

			// Check if response body matches expected regex pattern
			matched, err := regexp.MatchString(tt.wantResultRegex, rr.Body.String())
			if err != nil {
				t.Fatalf("Failed to match regex: %v", err)
			}
			if !matched {
				t.Errorf("Response body does not match expected pattern.\nGot: %s\nWant regex: %s",
					rr.Body.String(), tt.wantResultRegex)
			}
		})
	}
}
