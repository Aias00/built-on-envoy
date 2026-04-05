// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

import "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

type statisticsMetrics struct {
	requestsTotal shared.MetricID
	requestsError shared.MetricID
	inputTokens   shared.MetricID
	outputTokens  shared.MetricID
	totalTokens   shared.MetricID
	duration      shared.MetricID
	firstToken    shared.MetricID
}

func newStatisticsMetrics(h shared.HttpFilterConfigHandle) statisticsMetrics {
	requestsTotal, _ := h.DefineCounter("llm_statistics_requests_total", "kind", "model", "response_type")
	requestsError, _ := h.DefineCounter("llm_statistics_request_error_total", "kind", "model", "response_type")
	inputTokens, _ := h.DefineCounter("llm_statistics_input_tokens_total", "kind", "model", "response_type")
	outputTokens, _ := h.DefineCounter("llm_statistics_output_tokens_total", "kind", "model", "response_type")
	totalTokens, _ := h.DefineCounter("llm_statistics_total_tokens_total", "kind", "model", "response_type")
	duration, _ := h.DefineHistogram("llm_statistics_request_duration_ms", "kind", "model", "response_type")
	firstToken, _ := h.DefineHistogram("llm_statistics_first_token_duration_ms", "kind", "model", "response_type")

	return statisticsMetrics{
		requestsTotal: requestsTotal,
		requestsError: requestsError,
		inputTokens:   inputTokens,
		outputTokens:  outputTokens,
		totalTokens:   totalTokens,
		duration:      duration,
		firstToken:    firstToken,
	}
}
