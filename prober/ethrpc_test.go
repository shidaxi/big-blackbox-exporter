package prober

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
)

func TestProbeETHRPC(t *testing.T) {
	ethRpcUrl := "https://rpc.ankr.com/eth"

	// Mock dependencies
	mockLogger := log.NewNopLogger()

	// Create a test server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1234"}`))
	}))
	defer testServer.Close()

	// Test cases
	testCases := []struct {
		name           string
		target         string
		params         url.Values
		module         config.Module
		expectedResult bool
	}{
		{
			name:   "ETH Chain Info",
			target: ethRpcUrl,
			params: url.Values{
				"module": []string{"chain_info"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH Balance",
			target: ethRpcUrl,
			params: url.Values{
				"module":  []string{"balance"},
				"account": []string{"deployer1:0x207E804758e28F2b3fD6E4219671B327100b82f8"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH ERC20 Balance",
			target: ethRpcUrl,
			params: url.Values{
				"module":  []string{"erc20balance"},
				"account": []string{"deployer1:0x207E804758e28F2b3fD6E4219671B327100b82f8"},
				"token":   []string{"0x3c3a81e81dc49a522a592e7622a7e711c06bf354"},
				"symbol":  []string{"MNT"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH Contract Call balanceOf",
			target: ethRpcUrl,
			params: url.Values{
				"module": []string{"contract_call"},
				"call": []string{
					"Token1|0x3c3a81e81dc49a522a592e7622a7e711c06bf354|[{\"inputs\":[{\"name\":\"\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"type\":\"uint256\"}],\"type\":\"function\"}]|0x207E804758e28F2b3fD6E4219671B327100b82f8",
					"Token1|0x3c3a81e81dc49a522a592e7622a7e711c06bf354|[{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"type\":\"address\"}],\"type\":\"function\"}]",
					"Token1|0x3c3a81e81dc49a522a592e7622a7e711c06bf354|[{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"type\":\"uint256\"}],\"type\":\"function\"}]",
					"Token1|0x3c3a81e81dc49a522a592e7622a7e711c06bf354|[{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[],\"type\":\"function\"}]",
					"Token2|0xE6829d9a7eE3040e1276Fa75293Bde931859e8fA|[{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"type\":\"uint256\"}],\"type\":\"function\"}]",
				},
			},
			module:         config.Module{},
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			result := ProbeETHRPC(context.Background(), tc.target, tc.params, tc.module, registry, mockLogger)
			mfs, err := registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			for _, mf := range mfs {
				fmt.Printf("Metric: %s\n", mf.GetName())
				for _, m := range mf.GetMetric() {
					fmt.Printf("\033[32m  Metric: %v\033[0m\n", m)
				}
			}
			if result != tc.expectedResult {
				t.Errorf("Expected result %v, got %v", tc.expectedResult, result)
			}
		})
	}
}
