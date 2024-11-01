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

func TestProbeJsonRPC(t *testing.T) {
	ethRpcUrl := "https://rpc.ankr.com/eth"
	suiRpcUrl := "https://fullnode.mainnet.sui.io"
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
				"module":         []string{"jsonrpc"},
				"method":         []string{"eth_getBalance"},
				"arg":            []string{"0x0000000000000000000000000000000000000000"},
				"decimal":        []string{"18"},
				"tag":            []string{"WalletZero"},
				"resultJMESPath": []string{""},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH eth_blockNumber",
			target: ethRpcUrl,
			params: url.Values{
				"module":         []string{"jsonrpc"},
				"method":         []string{"eth_blockNumber"},
				"arg":            []string{""},
				"decimal":        []string{"0"},
				"tag":            []string{"BlockNumber"},
				"resultJMESPath": []string{""},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH eth_call",
			target: ethRpcUrl,
			params: url.Values{
				"module":         []string{"jsonrpc"},
				"method":         []string{"eth_call"},
				"arg":            []string{"{from:,to:0x3c3a81e81dc49a522a592e7622a7e711c06bf354,data:0x8da5cb5b},latest"},
				"decimal":        []string{"0"},
				"tag":            []string{"MNTTokenOwner"},
				"resultJMESPath": []string{""},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "ETH multiple methods",
			target: ethRpcUrl,
			params: url.Values{
				"module":         []string{"jsonrpc"},
				"method":         []string{"eth_getBalance", "eth_blockNumber"},
				"arg":            []string{"0x0000000000000000000000000000000000000000", ""},
				"decimal":        []string{"18", "0"},
				"tag":            []string{"WalletZero", "BlockNumber"},
				"resultJMESPath": []string{"", ""},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "SUI sui_getLatestCheckpointSequenceNumber",
			target: suiRpcUrl,
			params: url.Values{
				"module":         []string{"jsonrpc"},
				"disableBatch":   []string{"true"},
				"method":         []string{"sui_getLatestCheckpointSequenceNumber"},
				"arg":            []string{""},
				"decimal":        []string{"0"},
				"tag":            []string{"CheckpointSequenceNumber"},
				"resultJMESPath": []string{""},
			},
			module:         config.Module{},
			expectedResult: true,
		},
		{
			name:   "SUI suix_getBalance",
			target: suiRpcUrl,
			params: url.Values{
				"module":         []string{"jsonrpc"},
				"disableBatch":   []string{"true"},
				"method":         []string{"suix_getBalance"},
				"arg":            []string{"0x0000000000000000000000000000000000000000000000000000000000000000,0x2::sui::SUI"},
				"decimal":        []string{"6"},
				"tag":            []string{"WalletZero"},
				"resultJMESPath": []string{"totalBalance"},
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
