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
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-kit/log"
	"github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	"net/url"
	"os/exec"
	"strings"
)

func ProbeBTCRPC(ctx context.Context, target string, params url.Values, module config.Module, registry *prometheus.Registry, logger log.Logger) (success bool) {
	host := target
	if strings.HasPrefix(target, "http://") {
		host = strings.TrimLeft(target, "http://")
	} else if strings.HasPrefix(target, "https://") {
		host = strings.TrimLeft(target, "https://")
		disableTls = false
	} else {
	}

	url := host

	rpcUser := params.Get("user")
	rpcPass := params.Get("pass")

	switch params.Get("module") {
	case "btc_chain_info":
		var (
			blockNumberGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "probe_btcrpc_block_number",
				Help: "",
			}, []string{"target"})
		)
		registry.MustRegister(blockNumberGaugeVec)

		cmd := exec.Command("curl", "-s", "--user", fmt.Sprintf("%s:%s", rpcUser, rpcPass),
			fmt.Sprintf("%s", url), "-H", "content-type: text/plain;",
			"-d", `{"jsonrpc":"1.0","id":"curltext","method":"getblockchaininfo","params":[]}`)
		output, err := cmd.Output()
		if err != nil {
			fmt.Println("Error executing curl command:", err)
			return
		}
		// 解析输出的JSON数据
		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			fmt.Println("Error parsing JSON:", err)
			return
		}
		// 从结果中获取区块高度信息
		if chainInfo, ok := result["result"].(map[string]interface{}); ok {
			if blocks, ok := chainInfo["blocks"].(float64); ok {
				blockHeightGauge.WithLabelValues(network, url).Set(float64(blocks))
			} else {
				fmt.Println("Error retrieving block height from response")
			}
		} else {
			fmt.Println("Error retrieving chain info from response")
		}

		blockNumberGaugeVec.WithLabelValues(target).Set(float64(blockNumber))
	}

	return true
}
