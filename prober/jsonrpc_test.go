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

func TestResultToFloat64WithDecimals(t *testing.T) {
	testCases := []struct {
		name     string
		result   string
		decimals int64
		expected float64
	}{
		{
			name:     "With 0x prefix",
			result:   "0x3635C9ADC5DEA00000",
			decimals: 18,
			expected: 1000.0,
		},
		{
			name:     "Decimals 6",
			result:   "3b9aca00",
			decimals: 6,
			expected: 1000.0,
		},
		{
			name:     "Zero value",
			result:   "0x0",
			decimals: 18,
			expected: 0.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := resultToFloat64WithDecimals(tc.result, tc.decimals)
			if result != tc.expected {
				t.Errorf("Expected %f, but got %f", tc.expected, result)
			}
		})
	}
}

func TestStringsToSlice(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected []interface{}
	}{
		{
			name:     "Mixed types",
			input:    "42,true,hello,0xabc",
			expected: []interface{}{int64(42), true, "hello", "0xabc"},
		},
		{
			name:     "Only integers",
			input:    "1,2,3,4,5",
			expected: []interface{}{int64(1), int64(2), int64(3), int64(4), int64(5)},
		},
		{
			name:     "Only booleans",
			input:    "true,false,true",
			expected: []interface{}{true, false, true},
		},
		{
			name:     "Only strings",
			input:    "foo,bar,baz",
			expected: []interface{}{"foo", "bar", "baz"},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: []interface{}{""},
		},
		{
			name:     "With spaces",
			input:    " 10 , true , hello ",
			expected: []interface{}{int64(10), true, "hello"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := stringsToSlice(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("Expected slice length %d, but got %d", len(tc.expected), len(result))
				return
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("At index %d, expected %v (%T), but got %v (%T)", i, tc.expected[i], tc.expected[i], v, v)
				}
			}
		})
	}
}

func TestProbeJsonRPC(t *testing.T) {
	ethRpcUrl := "https://rpc.ankr.com/eth"
	suiRpcUrl := "https://sui-mainnet-endpoint.blockvision.org"
	// Mock dependencies
	mockLogger := log.NewNopLogger()

	// Create a test server
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`)
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
			name:   "ETH eth_getBalance for Wallet Zero",
			target: ethRpcUrl,
			params: url.Values{
				"module":   []string{"jsonrpc"},
				"method":   []string{"eth_getBalance"},
				"args":     []string{"0x0000000000000000000000000000000000000000"},
				"decimals": []string{"18"},
				"tag":      []string{"WalletZero"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH eth_blockNumber",
			target: ethRpcUrl,
			params: url.Values{
				"module":  []string{"jsonrpc"},
				"method":  []string{"eth_blockNumber"},
				"arg":     []string{""},
				"decimal": []string{"0"},
				"tag":     []string{"BlockNumber"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH multiple methods",
			target: ethRpcUrl,
			params: url.Values{
				"module":  []string{"jsonrpc"},
				"method":  []string{"eth_getBalance", "eth_blockNumber"},
				"arg":     []string{"0x0000000000000000000000000000000000000000", ""},
				"decimal": []string{"18", "0"},
				"tag":     []string{"WalletZero", "BlockNumber"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "SUI sui_getLatestCheckpointSequenceNumber",
			target: suiRpcUrl,
			params: url.Values{
				"module":       []string{"jsonrpc"},
				"disableBatch": []string{"true"},
				"method":       []string{"sui_getLatestCheckpointSequenceNumber"},
				"arg":          []string{""},
				"decimal":      []string{"0"},
				"tag":          []string{"CheckpointSequenceNumber"},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:           "Empty parameters",
			target:         testServer.URL,
			params:         url.Values{},
			module:         config.Module{},
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new registry for each test case
			testRegistry := prometheus.NewRegistry()

			ctx := context.Background()
			result := ProbeJSONRPC(ctx, tc.target, tc.params, tc.module, testRegistry, mockLogger)

			if result != tc.expectedResult {
				t.Errorf("Expected result %v, but got %v", tc.expectedResult, result)
			}
		})
	}
}
