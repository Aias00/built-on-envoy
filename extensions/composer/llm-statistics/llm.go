// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

const (
	KindOpenAI    = "openai"
	KindAnthropic = "anthropic"
)

type LLMUsage struct {
	InputTokens  uint32
	OutputTokens uint32
	TotalTokens  uint32
}

type LLMRequest interface {
	GetModel() string
	IsStream() bool
	GetQuestion() string
	GetSystem() string
}

type LLMResponse interface {
	GetUsage() LLMUsage
	GetAnswer() string
	GetReasoning() string
	GetToolCalls() any
	GetReasoningTokens() uint32
	GetCachedTokens() uint32
	GetInputTokenDetails() any
	GetOutputTokenDetails() any
}

type LLMResponseChunk interface {
	GetUsage() LLMUsage
	GetAnswer() string
	GetReasoning() string
	GetToolCalls() any
	HasTextToken() bool
}

type SSEParser interface {
	Feed(data []byte) error
	Finish() (LLMResponse, error)
	SeenTextToken() bool
}

type LLMFactory interface {
	ParseRequest(body []byte) (LLMRequest, error)
	ParseResponse(body []byte) (LLMResponse, error)
	NewSSEParser() SSEParser
}
