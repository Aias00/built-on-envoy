// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/tetratelabs/built-on-envoy/extensions/composer/pkg"
)

func newTestStats(ctrl *gomock.Controller) statisticsMetrics {
	cfgHandle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	id := shared.MetricID(0)
	nextID := func(_ string, _ ...string) (shared.MetricID, shared.MetricsResult) {
		id++
		return id, shared.MetricsSuccess
	}
	cfgHandle.EXPECT().DefineCounter(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(5)
	cfgHandle.EXPECT().DefineHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(2)
	return newStatisticsMetrics(cfgHandle)
}

const (
	idRequestsTotal shared.MetricID = 1
	idRequestsError shared.MetricID = 2
	idInputTokens   shared.MetricID = 3
	idOutputTokens  shared.MetricID = 4
	idTotalTokens   shared.MetricID = 5
	idDuration      shared.MetricID = 6
	idFirstToken    shared.MetricID = 7
)

func TestConfigFactory_Create_EmptyConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	handle := mocks.NewMockHttpFilterConfigHandle(ctrl)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Times(1)
	id := shared.MetricID(0)
	nextID := func(_ string, _ ...string) (shared.MetricID, shared.MetricsResult) {
		id++
		return id, shared.MetricsSuccess
	}
	handle.EXPECT().DefineCounter(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(5)
	handle.EXPECT().DefineHistogram(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(nextID).Times(2)

	factory := &statisticsConfigFactory{}
	ff, err := factory.Create(handle, nil)
	require.NoError(t, err)

	filterFactory, ok := ff.(*statisticsFilterFactory)
	require.True(t, ok)
	require.Equal(t, defaultMetadataNamespace, filterFactory.config.MetadataNamespace)
	require.False(t, filterFactory.config.UseDefaultResponseAttributes)
	require.Empty(t, filterFactory.config.SessionIDHeader)
}

func TestWellKnownHttpFilterConfigFactories(t *testing.T) {
	factories := WellKnownHttpFilterConfigFactories()
	require.Contains(t, factories, ExtensionName)
}

func TestStatisticsFilter_OnRequestHeaders_StripsQueryStringForPathMatching(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantKind string
	}{
		{
			name:     "openai with query string",
			path:     "/v1/chat/completions?api-version=2024-10-21",
			wantKind: KindOpenAI,
		},
		{
			name:     "anthropic with query string",
			path:     "/v1/messages?beta=true",
			wantKind: KindAnthropic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			handle := mocks.NewMockHttpFilterHandle(ctrl)
			handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
				Return(pkg.UnsafeBufferFromString(tt.path), true)

			filter := &statisticsFilter{handle: handle}
			headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})

			require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
			require.True(t, filter.matched)
			require.Equal(t, tt.wantKind, filter.kind)
			require.NotNil(t, filter.factory)
		})
	}
}

func TestStatisticsFilter_OpenAINonStreaming_LightweightLogAndMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":false}`)
	responseBody := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(map[string][]string{
		"x-session-id": {"sess-123"},
	})).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "gpt-4o", entry["model"])
		require.Equal(t, "sess-123", entry["session_id"])
		require.EqualValues(t, 10, entry["input_token"])
		require.EqualValues(t, 20, entry["output_token"])
		require.EqualValues(t, 30, entry["total_token"])
		require.Equal(t, "nonstream", entry["response_type"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:            defaultMetadataNamespace,
			UseDefaultResponseAttributes: true,
			SessionIDHeader:              "x-session-id",
		},
		metrics:       newTestStats(ctrl),
		requestSentAt: time.Now().Add(-50 * time.Millisecond),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_OpenAIStreaming_LightweightLogAndMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":true}`)
	chunk := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n")
	done := []byte("data: [DONE]\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(8), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(4), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(12), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idFirstToken, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "gpt-4o", entry["model"])
		require.Equal(t, "stream", entry["response_type"])
		require.EqualValues(t, 8, entry["input_token"])
		require.EqualValues(t, 4, entry["output_token"])
		require.Contains(t, entry, "llm_first_token_duration_ms")
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:            defaultMetadataNamespace,
			UseDefaultResponseAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(done), true))
}

func TestStatisticsFilter_OpenAINonStreaming_DefaultAttributes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{
		"model":"gpt-4o",
		"stream":false,
		"messages":[
			{"role":"system","content":"You are concise."},
			{"role":"user","content":"What is 2+2?"}
		]
	}`)
	responseBody := []byte(`{
		"choices":[
			{"message":{"role":"assistant","content":"4","reasoning_content":"Simple arithmetic"}}
		],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "What is 2+2?", entry["question"])
		require.Equal(t, "4", entry["answer"])
		require.Equal(t, "Simple arithmetic", entry["reasoning"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_AnthropicNonStreaming_DefaultAttributes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{
		"model":"claude-sonnet-4-20250514",
		"stream":false,
		"messages":[{"role":"user","content":"hello anthropic"}]
	}`)
	responseBody := []byte(`{
		"role":"assistant",
		"content":[{"type":"text","text":"Hello from Claude."}],
		"usage":{"input_tokens":9,"output_tokens":5}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/messages"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(9), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(14), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "hello anthropic", entry["question"])
		require.Equal(t, "Hello from Claude.", entry["answer"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_OpenAIStreaming_DefaultAttributes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"stream please"}]}`)
	chunk1 := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\",\"reasoning_content\":\"Think\"}}]}\n")
	chunk2 := []byte("data: {\"choices\":[{\"delta\":{\"content\":\" world\",\"reasoning_content\":\" more\"}}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n")
	done := []byte("data: [DONE]\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(8), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(4), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(12), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idFirstToken, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "stream please", entry["question"])
		require.Equal(t, "Hello world", entry["answer"])
		require.Equal(t, "Think more", entry["reasoning"])
		require.Equal(t, "stream", entry["response_type"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk1), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk2), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(done), true))
}

func TestStatisticsFilter_OpenAINonStreaming_DefaultAttributes_ToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"weather?"}]}`)
	responseBody := []byte(`{
		"choices":[
			{"message":{
				"content":"",
				"tool_calls":[
					{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}
				]
			}}
		],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		toolCalls, ok := entry["tool_calls"].([]any)
		require.True(t, ok)
		require.Len(t, toolCalls, 1)
		first, ok := toolCalls[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "call_1", first["id"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_OpenAIStreaming_DefaultAttributes_ToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"weather?"}]}`)
	chunk1 := []byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n")
	chunk2 := []byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"loc\\\":\\\"NYC\\\"}\"}}]}}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n")
	done := []byte("data: [DONE]\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(8), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(4), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(12), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		toolCalls, ok := entry["tool_calls"].([]any)
		require.True(t, ok)
		require.Len(t, toolCalls, 1)
		first, ok := toolCalls[0].(map[string]any)
		require.True(t, ok)
		fn, ok := first["function"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "{\"loc\":\"NYC\"}", fn["arguments"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk1), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk2), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(done), true))
}

func TestStatisticsFilter_AnthropicNonStreaming_DefaultAttributes_ToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"claude-sonnet-4-20250514","stream":false,"messages":[{"role":"user","content":"weather?"}]}`)
	responseBody := []byte(`{
		"role":"assistant",
		"content":[
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"location":"NYC"}}
		],
		"usage":{"input_tokens":9,"output_tokens":5}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/messages"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(9), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(14), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		toolCalls, ok := entry["tool_calls"].([]any)
		require.True(t, ok)
		require.Len(t, toolCalls, 1)
		first, ok := toolCalls[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "toolu_1", first["id"])
		require.Equal(t, "get_weather", first["name"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_AnthropicStreaming_DefaultAttributes_ToolCalls(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"weather?"}]}`)
	chunk1 := []byte("event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":9}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\",\"input\":{}}}\n\n")
	chunk2 := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"location\\\":\\\"NYC\\\"}\"}}\n\nevent: message_delta\ndata: {\"usage\":{\"output_tokens\":5}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/messages"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(9), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(14), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	// Tool-call-only streaming chunks should not emit first-token timing.
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		toolCalls, ok := entry["tool_calls"].([]any)
		require.True(t, ok)
		require.Len(t, toolCalls, 1)
		first, ok := toolCalls[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "toolu_1", first["id"])
		require.Equal(t, "get_weather", first["name"])
		require.Equal(t, "{\"location\":\"NYC\"}", first["input"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk1), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk2), true))
}

func TestStatisticsFilter_OpenAINonStreaming_DefaultAttributes_TokenDetailsAndSystem(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{
		"model":"gpt-4o",
		"stream":false,
		"messages":[
			{"role":"system","content":"You are a helpful assistant."},
			{"role":"user","content":"Explain caching."}
		]
	}`)
	responseBody := []byte(`{
		"choices":[{"message":{"role":"assistant","content":"Caching stores reused context."}}],
		"usage":{
			"prompt_tokens":100,
			"completion_tokens":50,
			"total_tokens":150,
			"prompt_tokens_details":{"cached_tokens":80},
			"completion_tokens_details":{"reasoning_tokens":25}
		}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(100), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(50), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(150), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "You are a helpful assistant.", entry["system"])
		require.EqualValues(t, 25, entry["reasoning_tokens"])
		require.EqualValues(t, 80, entry["cached_tokens"])
		require.Contains(t, entry, "input_token_details")
		require.Contains(t, entry, "output_token_details")
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_AnthropicNonStreaming_DefaultAttributes_SystemAndCacheDetails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{
		"model":"claude-sonnet-4-20250514",
		"stream":false,
		"system":"You are a helpful assistant.",
		"messages":[{"role":"user","content":"Explain caching."}]
	}`)
	responseBody := []byte(`{
		"role":"assistant",
		"content":[{"type":"text","text":"Anthropic caching response."}],
		"usage":{"input_tokens":9,"output_tokens":5,"cache_creation_input_tokens":20,"cache_read_input_tokens":30}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/messages"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(9), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(14), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "You are a helpful assistant.", entry["system"])
		require.EqualValues(t, 30, entry["cached_tokens"])
		require.Contains(t, entry, "input_token_details")
		require.NotContains(t, entry, "output_token_details")
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_OpenAIArrayContent_ExtractsQuestionAndSystem(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{
		"model":"gpt-4o",
		"stream":false,
		"messages":[
			{"role":"system","content":[{"type":"text","text":"You are a helpful assistant."}]},
			{"role":"user","content":[{"type":"text","text":"What is 2+2?"}]}
		]
	}`)
	responseBody := []byte(`{
		"choices":[{"message":{"role":"assistant","content":"4"}}],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "What is 2+2?", entry["question"])
		require.Equal(t, "You are a helpful assistant.", entry["system"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_AnthropicArrayContent_ExtractsQuestion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{
		"model":"claude-sonnet-4-20250514",
		"stream":false,
		"messages":[{"role":"user","content":[{"type":"text","text":"hello anthropic"}]}]
	}`)
	responseBody := []byte(`{
		"role":"assistant",
		"content":[{"type":"text","text":"Hello from Claude."}],
		"usage":{"input_tokens":9,"output_tokens":5}
	}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/messages"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(9), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(14), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "anthropic", "claude-sonnet-4-20250514", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		require.Equal(t, "hello anthropic", entry["question"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_AnthropicStreaming_SparseIndexes_AreNotDropped(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"weather?"}]}`)
	chunk1 := []byte("event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":9}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_2\",\"name\":\"get_weather\",\"input\":{}}}\n\n")
	chunk2 := []byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"location\\\":\\\"NYC\\\"}\"}}\n\nevent: message_delta\ndata: {\"usage\":{\"output_tokens\":5}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/messages"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(9), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(5), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(14), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "anthropic", "claude-sonnet-4-20250514", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelInfo, gomock.Any(), gomock.Any()).Do(func(_ shared.LogLevel, _ string, payload string) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(payload), &entry))
		toolCalls, ok := entry["tool_calls"].([]any)
		require.True(t, ok)
		require.Len(t, toolCalls, 1)
		first := toolCalls[0].(map[string]any)
		require.Equal(t, "toolu_2", first["id"])
	}).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace:    defaultMetadataNamespace,
			UseDefaultAttributes: true,
		},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk1), false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk2), true))
}

func TestStatisticsFilter_RequestParseFailure_SkipsResponseProcessing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{bad json}`)
	responseBody := []byte(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsError, uint64(1), "openai", "", "nonstream").Return(shared.MetricsSuccess).Times(1)
	// No response metrics should be emitted after request parse failure.

	filter := &statisticsFilter{
		handle:  handle,
		config:  &statisticsConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_FirstTokenDuration_NotSetByToolCallOnlyChunk(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"weather?"}]}`)
	chunk1 := []byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]}}]}\n")
	chunk2 := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n")
	done := []byte("data: [DONE]\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(8), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(4), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(12), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idDuration, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().RecordHistogramValue(idFirstToken, gomock.Any(), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	// Default config does not emit structured logs.

	filter := &statisticsFilter{
		handle:  handle,
		config:  &statisticsConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk1), false))
	require.True(t, filter.firstChunkAt.IsZero(), "tool-call-only chunk must not count as first token")
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(chunk2), false))
	require.False(t, filter.firstChunkAt.IsZero(), "first text token should set firstChunkAt")
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(done), true))
}

func TestStatisticsFilter_StreamingParseFailure_SkipsFinish(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"weather?"}]}`)
	malformedChunk := []byte("data: {not valid json}\n")
	done := []byte("data: [DONE]\n")

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsError, uint64(1), "openai", "gpt-4o", "stream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)
	// No response metrics or structured logs should be emitted after a streaming parse failure.

	filter := &statisticsFilter{
		handle:  handle,
		config:  &statisticsConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	sseHeaders := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"text/event-stream"}})
	require.Equal(t, shared.HeadersStatusContinue, filter.OnResponseHeaders(sseHeaders, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(malformedChunk), false))
	require.True(t, filter.streamParseFailed, "first streaming parse error should mark the response as failed")
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(done), true))
}

func TestStatisticsFilter_ResponseParseFailure_IncrementsErrorMetric(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	requestBody := []byte(`{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hello"}]}`)
	responseBody := []byte(`{not valid json}`)

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	handle.EXPECT().GetAttributeString(shared.AttributeIDRequestPath).
		Return(pkg.UnsafeBufferFromString("/v1/chat/completions"), true)
	handle.EXPECT().SetMetadata(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	handle.EXPECT().RequestHeaders().Return(fake.NewFakeHeaderMap(nil)).AnyTimes()
	handle.EXPECT().BufferedRequestBody().Return(fake.NewFakeBodyBuffer(requestBody)).AnyTimes()
	handle.EXPECT().ReceivedRequestBody().Return(nil).AnyTimes()
	handle.EXPECT().BufferedResponseBody().Return(fake.NewFakeBodyBuffer(responseBody)).AnyTimes()
	handle.EXPECT().ReceivedResponseBody().Return(nil).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsError, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().Log(shared.LogLevelDebug, gomock.Any(), gomock.Any()).Times(1)
	// No success metrics should be emitted after response parse failure.

	filter := &statisticsFilter{
		handle:  handle,
		config:  &statisticsConfig{MetadataNamespace: defaultMetadataNamespace},
		metrics: newTestStats(ctrl),
	}

	headers := fake.NewFakeHeaderMap(map[string][]string{"content-type": {"application/json"}})
	require.Equal(t, shared.HeadersStatusStop, filter.OnRequestHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnRequestBody(fake.NewFakeBodyBuffer(requestBody), true))
	require.Equal(t, shared.HeadersStatusStop, filter.OnResponseHeaders(headers, false))
	require.Equal(t, shared.BodyStatusContinue, filter.OnResponseBody(fake.NewFakeBodyBuffer(responseBody), true))
}

func TestStatisticsFilter_Finish_WritesStructuredMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	handle := mocks.NewMockHttpFilterHandle(ctrl)
	captured := map[string]any{}
	handle.EXPECT().SetMetadata(defaultMetadataNamespace, gomock.Any(), gomock.Any()).Do(
		func(_ string, key string, value any) {
			captured[key] = value
		},
	).AnyTimes()
	handle.EXPECT().IncrementCounterValue(idRequestsTotal, uint64(1), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idInputTokens, uint64(10), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idOutputTokens, uint64(20), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)
	handle.EXPECT().IncrementCounterValue(idTotalTokens, uint64(30), "openai", "gpt-4o", "nonstream").Return(shared.MetricsSuccess).Times(1)

	filter := &statisticsFilter{
		handle: handle,
		config: &statisticsConfig{
			MetadataNamespace: defaultMetadataNamespace,
		},
		metrics: newTestStats(ctrl),
		kind:    "openai",
		model:   "gpt-4o",
	}

	resp := &openAILLMResponse{
		usage: LLMUsage{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
		toolCalls: []openAIToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: openAIToolCallFunction{
					Name:      "get_weather",
					Arguments: "{\"loc\":\"NYC\"}",
				},
			},
		},
		inputTokenDetails: &openAIPromptTokensDetails{
			CachedTokens: 8,
		},
		outputTokenDetails: &openAICompletionTokensDetails{
			ReasoningTokens: 5,
			AudioTokens:     0,
		},
	}

	filter.finish(resp, responseTypeNonStream)

	toolCalls, ok := captured["tool_calls"].([]openAIToolCall)
	require.True(t, ok)
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call_1", toolCalls[0].ID)

	inputDetails, ok := captured["input_token_details"].(*openAIPromptTokensDetails)
	require.True(t, ok)
	require.EqualValues(t, 8, inputDetails.CachedTokens)

	outputDetails, ok := captured["output_token_details"].(*openAICompletionTokensDetails)
	require.True(t, ok)
	require.EqualValues(t, 5, outputDetails.ReasoningTokens)
}
