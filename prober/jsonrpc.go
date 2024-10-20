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
	"math"
	"math/big"
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

// strings is a comma separated string, split it by comma, convert it to a slice of interface
// if the string is a number, convert it to a int64
// if the string is a string, convert it to a string
// if the string is a boolean, convert it to a bool

func stringsToSlice(s string) []interface{} {

	if s == "" {
		return []interface{}{}
	}

	// Split the string by comma
	parts := strings.Split(s, ",")

	result := make([]interface{}, len(parts))

	for i, part := range parts {
		// Trim spaces
		part = strings.TrimSpace(part)

		// Try to convert to int64
		if intVal, err := strconv.ParseInt(part, 10, 64); err == nil {
			result[i] = intVal
			continue
		}

		// Try to convert to bool
		if boolVal, err := strconv.ParseBool(part); err == nil {
			result[i] = boolVal
			continue
		}

		// If it's neither a number nor a boolean, keep it as a string
		result[i] = part
	}

	return result
}

func resultToFloat64WithDecimals(result string, decimals int64) float64 {
	// Remove "0x" prefix if present
	resultInt := new(big.Int)
	if strings.HasPrefix(result, "0x") {
		resultInt.SetString(strings.TrimPrefix(result, "0x"), 16)
	} else {
		resultInt.SetString(result, 10)
	}
	level.Debug(log.NewNopLogger()).Log("resultInt", resultInt)
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(resultInt), big.NewFloat(math.Pow10(int(decimals)))).Float64()
	return f
}

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
				var result interface{}
				err := eth.Client().Call(&result, m, stringsToSlice(args[i])...)
				if err != nil {
					level.Error(logger).Log("msg", "call failed, "+err.Error())
					return false
				}

				level.Debug(logger).Log("msg", "Raw result", "result", result)

				var r string
				if resultJMESPath[i] != "" {
					sr, err := jmespath.Search(resultJMESPath[i], result)
					if err != nil {
						level.Error(logger).Log("msg", "jmespath failed, "+err.Error())
						return false
					}
					r = fmt.Sprintf("%v", sr)
				} else {
					jsonBytes, err := json.Marshal(result)
					if err != nil {
						level.Error(logger).Log("msg", "JSON marshaling failed", "error", err)
						return false
					}
					r = string(jsonBytes)
				}

				decimalsInt, _ := strconv.ParseInt(decimals[i], 10, 64)
				level.Debug(logger).Log("msg", "result "+r)

				value := resultToFloat64WithDecimals(r, decimalsInt)

				jsonrpcGaugeVec.WithLabelValues(
					target,
					methods[i],
					args[i],
					tags[i],
				).Set(value)
			}
		} else {

			var batch []rpc.BatchElem
			for i, m := range methods {
				var result string
				batch = append(batch, rpc.BatchElem{
					Method: m,
					Args:   stringsToSlice(args[i]),
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
				if resultJMESPath[i] != "" {
					sr, err := jmespath.Search(resultJMESPath[i], e.Result)
					if err != nil {
						level.Error(logger).Log("msg", "jmespath failed, "+err.Error())
						return false
					}
					r = fmt.Sprintf("%v", sr)
				} else {
					r = *e.Result.(*string)
				}
				level.Debug(logger).Log("msg", "result "+r)
				value := resultToFloat64WithDecimals(r, decimalsInt)
				jsonrpcGaugeVec.WithLabelValues(
					target,
					methods[i],
					args[i],
					tags[i],
				).Set(value)
			}
		}
	}

	return true
}
