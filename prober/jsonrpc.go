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
	"net/url"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jmespath/go-jmespath"
	"github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
)

func ProbeJSONRPC(ctx context.Context, target string, params url.Values, module config.Module, registry *prometheus.Registry, logger log.Logger) (success bool) {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "http://" + target
	}
	eth, err := ethclient.Dial(target)
	if err != nil {
		level.Error(logger).Log("msg", "Error dialing rpc", target, err)
		return false
	}

	methods := params["method"]
	args := params["arg"]
	decimals := params["decimal"]
	tags := params["tag"]
	resultJMESPath := params["resultJMESPath"]

	if len(methods) == 0 || len(args) == 0 || len(decimals) == 0 || len(tags) == 0 || len(resultJMESPath) == 0 {
		level.Error(logger).Log("msg", "methods is empty")
		return false
	}

	if len(methods) != len(args) || len(methods) != len(decimals) || len(methods) != len(tags) || len(methods) != len(resultJMESPath) {
		level.Error(logger).Log("msg", "methods, params, decimals, tags must be the same length")
		return false
	}

	switch params.Get("module") {
	case "jsonrpc":
		var (
			jsonrpcGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: "probe_jsonrpc",
				Help: "",
			}, []string{"rpc", "method", "params", "tag"})
		)
		registry.MustRegister(jsonrpcGaugeVec)

		disableBatch := params.Get("disableBatch") == "true"

		if disableBatch {
			for i, m := range methods {
				var result json.RawMessage
				argsSlice, err := parseJSONRPCParams(args[i])
				if err != nil {
					level.Error(logger).Log("msg", "parseJSONRPCParams failed, "+err.Error())
					return false
				}
				err = eth.Client().Call(&result, m, argsSlice...)
				if err != nil {
					level.Error(logger).Log("msg", "call failed, "+err.Error())
					return false
				}

				level.Debug(logger).Log("msg", "Raw result", "result", result)

				var r string

				if strings.TrimSpace(resultJMESPath[i]) != "" {
					var jsonData interface{}
					if err := json.Unmarshal(result, &jsonData); err != nil {
						level.Error(logger).Log("msg", "Failed to unmarshal JSON result: "+err.Error())
						return false
					}
					sr, err := jmespath.Search(resultJMESPath[i], jsonData)
					if err != nil {
						level.Error(logger).Log("msg", "jmespath failed, "+err.Error())
						return false
					}
					r = fmt.Sprintf("%v", sr)
				} else {
					if err := json.Unmarshal(result, &r); err != nil {
						level.Error(logger).Log("msg", "Failed to unmarshal JSON result: "+err.Error())
						return false
					}
				}

				decimalsInt, _ := strconv.ParseInt(decimals[i], 10, 64)
				level.Debug(logger).Log("msg", "result "+r)

				value := resultToFloat64WithDecimals(r, decimalsInt)

				jsonrpcGaugeVec.WithLabelValues(
					target,
					methods[i],
					strings.TrimSpace(args[i]),
					tags[i],
				).Set(value)
			}
		} else {

			var batch []rpc.BatchElem
			for i, m := range methods {
				var result json.RawMessage
				argsSlice, err := parseJSONRPCParams(args[i])
				if err != nil {
					level.Error(logger).Log("msg", "parseJSONRPCParams failed, "+err.Error())
					return false
				}
				batch = append(batch, rpc.BatchElem{
					Method: m,
					Args:   argsSlice,
					Result: &result,
					Error:  nil,
				})
			}

			err = eth.Client().BatchCall(batch)
			if err != nil {
				level.Error(logger).Log("msg", "batchcall failed, "+err.Error())
				return false
			}
			for i, e := range batch {
				decimalsInt, _ := strconv.ParseInt(decimals[i], 10, 64)
				var r string
				if strings.TrimSpace(resultJMESPath[i]) != "" {
					rawMsg := e.Result.(*json.RawMessage)
					var jsonData interface{}
					if err := json.Unmarshal(*rawMsg, &jsonData); err != nil {
						level.Error(logger).Log("msg", "Failed to unmarshal JSON result: "+err.Error())
						return false
					}
					sr, err := jmespath.Search(resultJMESPath[i], jsonData)
					if err != nil {
						level.Error(logger).Log("msg", "jmespath failed, "+err.Error())
						return false
					}
					r = fmt.Sprintf("%v", sr)
				} else {
					rawMsg := e.Result.(*json.RawMessage)
					if err := json.Unmarshal(*rawMsg, &r); err != nil {
						level.Error(logger).Log("msg", "Failed to unmarshal JSON result: "+err.Error())
						return false
					}
				}
				level.Debug(logger).Log("msg", "result "+r)
				value := resultToFloat64WithDecimals(r, decimalsInt)
				jsonrpcGaugeVec.WithLabelValues(
					target,
					methods[i],
					strings.TrimSpace(args[i]),
					tags[i],
				).Set(value)
			}
		}
	}

	return true
}
