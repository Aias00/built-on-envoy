# LLM Statistics Plugin

This directory contains a Go plugin for Envoy HTTP filters using the Composer Dynamic Module system.
It provides LLM-oriented observability for OpenAI Chat Completions and Anthropic Messages APIs.

## Features

The plugin can emit:

- Token usage metrics: input, output, and total tokens
- Latency metrics: request duration and time to first token for streaming
- Structured logs for LLM attributes
- Dynamic metadata for downstream filters, access logs, or routing logic

It supports:

- OpenAI `POST /v1/chat/completions`
- Anthropic `POST /v1/messages`
- Non-streaming JSON responses
- Streaming SSE responses

## Configuration

### Lightweight mode

```bash
boe run \
  --extension llm-statistics \
  --config '{
    "use_default_response_attributes": true,
    "session_id_header": "x-session-id"
  }'
```

This mode emits compact logs with:

- `model`
- `input_token`
- `output_token`
- `total_token`
- `llm_service_duration_ms`
- `llm_first_token_duration_ms` for streaming
- `session_id` when configured

### Full built-in attributes

```bash
boe run \
  --extension llm-statistics \
  --config '{
    "use_default_attributes": true,
    "session_id_header": "x-session-id"
  }'
```

This mode adds:

- `question`
- `system`
- `answer`
- `reasoning`
- `tool_calls`
- `reasoning_tokens`
- `cached_tokens`
- `input_token_details`
- `output_token_details`

## Dynamic Metadata

By default the plugin writes values under the namespace
`io.builtonenvoy.llm-statistics`.

Example keys include:

- `kind`
- `model`
- `response_type`
- `session_id`
- `question`
- `system`
- `answer`
- `reasoning`
- `tool_calls`
- `input_token`
- `output_token`
- `total_token`
- `reasoning_tokens`
- `cached_tokens`
- `input_token_details`
- `output_token_details`
- `llm_service_duration_ms`
- `llm_first_token_duration_ms`

These values can be consumed by:

- other dynamic module plugins
- access log formatters
- route selection logic after clearing route cache

## Access Log Example

The default `boe run` access log does not print dynamic metadata fields. To inspect them,
export the generated config and customize the access log format.

### 1. Export config and artifacts

```bash
boe gen-config \
  --local ./extensions/composer/llm-statistics \
  --config '{"use_default_attributes": true, "session_id_header": "x-session-id"}' \
  --output /tmp/boe-export
```

### 2. Edit `/tmp/boe-export/envoy.yaml`

Replace the default stdout access logger with a structured one:

```yaml
access_log:
  - name: envoy.access_loggers.stdout
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog
      log_format:
        typed_json_format:
          method: "%REQ(:METHOD)%"
          path: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"
          llm_kind: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:kind)%"
          llm_model: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:model)%"
          llm_question: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:question)%"
          llm_system: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:system)%"
          llm_answer: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:answer)%"
          llm_reasoning: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:reasoning)%"
          llm_tool_calls: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:tool_calls)%"
          llm_input_tokens: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:input_token)%"
          llm_output_tokens: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:output_token)%"
          llm_total_tokens: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:total_token)%"
          llm_reasoning_tokens: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:reasoning_tokens)%"
          llm_cached_tokens: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:cached_tokens)%"
          llm_input_token_details: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:input_token_details)%"
          llm_output_token_details: "%DYNAMIC_METADATA(io.builtonenvoy.llm-statistics:output_token_details)%"
```

### 3. Run exported Envoy

```bash
cd /tmp/boe-export
export GODEBUG=cgocheck=0
func-e run -c envoy.yaml --log-level info --component-log-level dynamic_modules:debug
```

The stdout access log will then include `llm-statistics` metadata fields.

## Notes

- `DYNAMIC_METADATA(namespace:key)` renders metadata produced by the plugin.
- When the referenced metadata value is a struct or list, Envoy prints JSON for that value.
