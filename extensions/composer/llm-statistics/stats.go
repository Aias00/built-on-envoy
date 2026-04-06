// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

import (
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

type statisticsMetrics struct {
	requestsTotal *pkg.Metric
	requestsError *pkg.Metric
	inputTokens   *pkg.Metric
	outputTokens  *pkg.Metric
	totalTokens   *pkg.Metric
	duration      *pkg.Metric
	firstToken    *pkg.Metric
}

func newStatisticsMetrics(h shared.HttpFilterConfigHandle) statisticsMetrics {
	return statisticsMetrics{
		requestsTotal: pkg.NewMetric(h, h.DefineCounter, "llm_statistics_requests_total", "kind", "model", "response_type"),
		requestsError: pkg.NewMetric(h, h.DefineCounter, "llm_statistics_request_error_total", "kind", "model", "response_type"),
		inputTokens:   pkg.NewMetric(h, h.DefineCounter, "llm_statistics_input_tokens_total", "kind", "model", "response_type"),
		outputTokens:  pkg.NewMetric(h, h.DefineCounter, "llm_statistics_output_tokens_total", "kind", "model", "response_type"),
		totalTokens:   pkg.NewMetric(h, h.DefineCounter, "llm_statistics_total_tokens_total", "kind", "model", "response_type"),
		duration:      pkg.NewMetric(h, h.DefineHistogram, "llm_statistics_request_duration_ms", "kind", "model", "response_type"),
		firstToken:    pkg.NewMetric(h, h.DefineHistogram, "llm_statistics_first_token_duration_ms", "kind", "model", "response_type"),
	}
}
