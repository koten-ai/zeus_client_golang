// SPDX-License-Identifier: BUSL-1.1

package application

// Hub-identical provider token rollup (Python application/tokens.py).
// Numeric behaviour matches Hub multi-round sum tests.

var llmStepTypes = map[string]struct{}{
	"llm":         {},
	"force_final": {},
}

const llmErrorType = "llm_error"

func tokenAsInt(v any) int {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	default:
		return 0
	}
}

// NormalizeUsage maps a provider usage blob to prompt/completion/total/cached.
func NormalizeUsage(usage map[string]any) map[string]int {
	u := usage
	if u == nil {
		u = map[string]any{}
	}
	details, _ := u["prompt_tokens_details"].(map[string]any)
	if details == nil {
		details = map[string]any{}
	}
	cached := details["cached_tokens"]
	if cached == nil {
		cached = u["cached_tokens"]
	}
	return map[string]int{
		"prompt":     tokenAsInt(u["prompt_tokens"]),
		"completion": tokenAsInt(u["completion_tokens"]),
		"total":      tokenAsInt(u["total_tokens"]),
		"cached":     tokenAsInt(cached),
	}
}

// UsageFromAIResponse reads usage from a recorded AI hop (body.usage or usage).
func UsageFromAIResponse(entry map[string]any) map[string]int {
	if entry == nil {
		return nil
	}
	if body, ok := entry["body"].(map[string]any); ok {
		if u, ok := body["usage"].(map[string]any); ok {
			return NormalizeUsage(u)
		}
	}
	if u, ok := entry["usage"].(map[string]any); ok {
		return NormalizeUsage(u)
	}
	return nil
}

func hasBilling(n map[string]int) bool {
	if n == nil {
		return false
	}
	return n["prompt"] != 0 || n["completion"] != 0 || n["total"] != 0
}

func stepUsage(step map[string]any) map[string]int {
	u, ok := step["usage"].(map[string]any)
	if !ok {
		return nil
	}
	n := NormalizeUsage(u)
	if !hasBilling(n) {
		return nil
	}
	return n
}

// SumProviderTokens sums LLM + force_final usage across rounds (Python sum_provider_tokens).
func SumProviderTokens(steps []map[string]any, aiResponses []map[string]any) map[string]any {
	var llmish []map[string]any
	for _, s := range steps {
		if s == nil {
			continue
		}
		t, _ := s["type"].(string)
		if _, ok := llmStepTypes[t]; ok {
			llmish = append(llmish, s)
		}
	}
	var responses []map[string]any
	for _, r := range aiResponses {
		if r != nil {
			responses = append(responses, r)
		}
	}

	prompt, completion, total, cached := 0, 0, 0, 0
	if len(llmish) == 0 {
		for _, entry := range responses {
			n := UsageFromAIResponse(entry)
			if !hasBilling(n) {
				continue
			}
			prompt += n["prompt"]
			completion += n["completion"]
			total += n["total"]
			cached += n["cached"]
		}
	} else {
		for i, s := range llmish {
			n := stepUsage(s)
			if n == nil && i < len(responses) {
				cand := UsageFromAIResponse(responses[i])
				if hasBilling(cand) {
					n = cand
				}
			}
			if n == nil {
				n = map[string]int{"prompt": 0, "completion": 0, "total": 0, "cached": 0}
			}
			prompt += n["prompt"]
			completion += n["completion"]
			total += n["total"]
			cached += n["cached"]
		}
	}
	if total == 0 && (prompt != 0 || completion != 0) {
		total = prompt + completion
	}
	extra := total - prompt - completion
	if extra < 0 {
		extra = 0
	}
	ok := total != 0 || prompt != 0 || completion != 0
	return map[string]any{
		"prompt":     prompt,
		"completion": completion,
		"total":      total,
		"cached":     cached,
		"extra":      extra,
		"ok":         ok,
	}
}

// AttachTraceTokens sets trace["tokens"] from steps + ai_responses (Python attach_trace_tokens).
func AttachTraceTokens(trace map[string]any) map[string]any {
	if trace == nil {
		return nil
	}
	steps := mapsFromAnySlice(trace["steps"])
	ai := mapsFromAnySlice(trace["ai_responses"])
	tokens := SumProviderTokens(steps, ai)
	trace["tokens"] = tokens
	return tokens
}

func mapsFromAnySlice(v any) []map[string]any {
	switch x := v.(type) {
	case []map[string]any:
		return x
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func usageReportsCached(usage map[string]any) bool {
	if usage == nil {
		return false
	}
	if _, ok := usage["cached_tokens"]; ok {
		return true
	}
	details, _ := usage["prompt_tokens_details"].(map[string]any)
	if details == nil {
		return false
	}
	_, ok := details["cached_tokens"]
	return ok
}

// TokensForSessionTrace is Hub join zeus_response.tokens.
// Nil when the LLM was never called or usage is unknown (never send 0 as fake).
func TokensForSessionTrace(steps []map[string]any) map[string]any {
	var returned []map[string]any
	var errored []map[string]any
	for _, s := range steps {
		if s == nil {
			continue
		}
		t, _ := s["type"].(string)
		if _, ok := llmStepTypes[t]; ok {
			returned = append(returned, s)
		}
		if t == llmErrorType {
			errored = append(errored, s)
		}
	}
	if len(returned) == 0 && len(errored) == 0 {
		return nil
	}
	summed := SumProviderTokens(returned, nil)
	prompt := tokenAsInt(summed["prompt"])
	completion := tokenAsInt(summed["completion"])
	total := tokenAsInt(summed["total"])
	if prompt == 0 && completion == 0 && total == 0 {
		return nil
	}
	out := map[string]any{
		"prompt":     prompt,
		"completion": completion,
		"total":      total,
		"rounds":     len(returned),
		"ok":         len(errored) == 0,
	}
	for _, s := range returned {
		u, _ := s["usage"].(map[string]any)
		if usageReportsCached(u) {
			out["cached"] = tokenAsInt(summed["cached"])
			break
		}
	}
	return out
}
