// SPDX-License-Identifier: BUSL-1.1

package llmopenai

import (
	"strings"

	"github.com/koten-ai/zeus_client_golang/ports"
)

// CacheHints is provider-aware prompt-caching (Python cache_hints).
func CacheHints(providerID, baseURL, convID string) (headers map[string]string, bodyExtra map[string]any) {
	headers = map[string]string{}
	bodyExtra = map[string]any{}
	if convID == "" {
		return headers, bodyExtra
	}
	pid := strings.ToLower(providerID)
	url := strings.ToLower(baseURL)
	if pid == "grok" || pid == "xai" || strings.Contains(url, "x.ai") {
		headers["x-grok-conv-id"] = convID
	} else if pid == "openai" || strings.Contains(url, "api.openai.com") {
		bodyExtra["prompt_cache_key"] = convID
	}
	return headers, bodyExtra
}

// CachedTokensOf reads prompt cache hits from an OpenAI-shaped usage object.
func CachedTokensOf(resp map[string]any) (int, bool) {
	if resp == nil {
		return 0, false
	}
	usage, _ := resp["usage"].(map[string]any)
	if usage == nil {
		return 0, false
	}
	if details, ok := usage["prompt_tokens_details"].(map[string]any); ok && details["cached_tokens"] != nil {
		if n, ok := asInt(details["cached_tokens"]); ok {
			return n, true
		}
	}
	if usage["cached_tokens"] != nil {
		if n, ok := asInt(usage["cached_tokens"]); ok {
			return n, true
		}
	}
	return 0, false
}

// BuildChatPayload is POST /chat/completions JSON (Python build_chat_payload).
// When tools is empty, tool_choice is omitted (providers 400 otherwise).
func BuildChatPayload(model string, messages, tools []map[string]any, temperature *float64, maxTokens *int, bodyExtra map[string]any) map[string]any {
	payload := map[string]any{
		"model":    model,
		"messages": copyMaps(messages),
	}
	if temperature != nil {
		payload["temperature"] = *temperature
	}
	if maxTokens != nil {
		payload["max_tokens"] = *maxTokens
	}
	if len(tools) > 0 {
		payload["tools"] = copyMaps(tools)
		payload["tool_choice"] = "auto"
	}
	for k, v := range bodyExtra {
		payload[k] = v
	}
	return payload
}

// ParseCompletionResponse maps an OpenAI-shaped body to LlmResponse.
func ParseCompletionResponse(body map[string]any) ports.LlmResponse {
	var content string
	toolCalls := []map[string]any{}
	choices, _ := body["choices"].([]any)
	if len(choices) > 0 {
		ch, _ := choices[0].(map[string]any)
		msg, _ := ch["message"].(map[string]any)
		if msg != nil {
			if c, ok := msg["content"].(string); ok {
				content = c
			} else if msg["content"] != nil {
				content = stringify(msg["content"])
			}
			rawTC := msg["tool_calls"]
			switch tc := rawTC.(type) {
			case []any:
				for _, item := range tc {
					if m, ok := item.(map[string]any); ok {
						toolCalls = append(toolCalls, copyMap(m))
					}
				}
			case []map[string]any:
				toolCalls = copyMaps(tc)
			}
		}
	}
	usage := map[string]any{}
	if u, ok := body["usage"].(map[string]any); ok && u != nil {
		usage = copyMap(u)
	}
	return ports.LlmResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Raw:       copyMap(body),
		Usage:     usage,
	}
}

func copyMaps(in []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, m := range in {
		out = append(out, copyMap(m))
	}
	return out
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	default:
		return 0, false
	}
}
