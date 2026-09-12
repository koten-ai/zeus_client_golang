// SPDX-License-Identifier: BUSL-1.1

package application

import "testing"

func TestNormalizeUsageNestedCached(t *testing.T) {
	u := NormalizeUsage(map[string]any{
		"prompt_tokens":         100,
		"completion_tokens":     10,
		"total_tokens":          110,
		"prompt_tokens_details": map[string]any{"cached_tokens": 40},
	})
	if u["prompt"] != 100 || u["completion"] != 10 || u["total"] != 110 || u["cached"] != 40 {
		t.Fatalf("%v", u)
	}
}

func TestSumMultiRoundLikeHubRecordAIHop(t *testing.T) {
	steps := []map[string]any{
		{"type": "llm", "round": 1, "usage": map[string]any{
			"prompt_tokens": 7800, "completion_tokens": 400, "total_tokens": 8200,
		}},
		{"type": "tool", "round": 1},
		{"type": "llm", "round": 2, "usage": map[string]any{
			"prompt_tokens": 7581, "completion_tokens": 31, "total_tokens": 7612,
		}},
	}
	got := SumProviderTokens(steps, nil)
	if got["prompt"] != 7800+7581 || got["completion"] != 400+31 || got["total"] != 8200+7612 {
		t.Fatalf("%v", got)
	}
	if got["ok"] != true {
		t.Fatal("ok")
	}
}

func TestTotPrefersProviderTotalEvenWhenGtInPlusOut(t *testing.T) {
	steps := []map[string]any{
		{"type": "llm", "usage": map[string]any{
			"prompt_tokens": 100, "completion_tokens": 10, "total_tokens": 150,
		}},
	}
	got := SumProviderTokens(steps, nil)
	if got["total"] != 150 || got["extra"] != 40 {
		t.Fatalf("%v", got)
	}
}

func TestForceFinalStepCounted(t *testing.T) {
	steps := []map[string]any{
		{"type": "llm", "usage": map[string]any{
			"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120,
		}},
		{"type": "force_final", "usage": map[string]any{
			"prompt_tokens": 50, "completion_tokens": 30, "total_tokens": 80,
		}},
	}
	got := SumProviderTokens(steps, nil)
	if got["prompt"] != 150 || got["completion"] != 50 || got["total"] != 200 {
		t.Fatalf("%v", got)
	}
}

func TestFallbackAIResponsesWhenStepUsageMissingNoDoubleCount(t *testing.T) {
	steps := []map[string]any{
		{"type": "llm", "round": 1, "usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12,
		}},
		{"type": "force_final", "round": 1},
	}
	ai := []map[string]any{
		{"round": 1, "body": map[string]any{"usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12,
		}}},
		{"round": 1, "force_final": true, "body": map[string]any{"usage": map[string]any{
			"prompt_tokens": 40, "completion_tokens": 12, "total_tokens": 52,
		}}},
	}
	got := SumProviderTokens(steps, ai)
	if got["prompt"] != 50 || got["completion"] != 14 || got["total"] != 64 {
		t.Fatalf("%v", got)
	}
}

func TestAttachTraceTokensSetsHubShape(t *testing.T) {
	trace := map[string]any{
		"steps": []any{
			map[string]any{"type": "llm", "usage": map[string]any{
				"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12,
			}},
			map[string]any{"type": "force_final", "usage": map[string]any{
				"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10,
			}},
		},
		"ai_responses": []any{},
	}
	out := AttachTraceTokens(trace)
	tok, _ := trace["tokens"].(map[string]any)
	if out["prompt"] != tok["prompt"] || tok["prompt"] != 15 || tok["total"] != 22 {
		t.Fatalf("%v", tok)
	}
}

func TestTokensForSessionTraceSumsTwoRounds(t *testing.T) {
	steps := []map[string]any{
		{"type": "llm", "round": 1, "usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11,
		}},
		{"type": "tool", "round": 1},
		{"type": "llm", "round": 2, "usage": map[string]any{
			"prompt_tokens": 20, "completion_tokens": 2, "total_tokens": 22,
		}},
	}
	bag := TokensForSessionTrace(steps)
	if bag == nil {
		t.Fatal("nil")
	}
	if bag["prompt"] != 30 || bag["completion"] != 3 || bag["total"] != 33 || bag["rounds"] != 2 {
		t.Fatalf("%v", bag)
	}
	if bag["ok"] != true {
		t.Fatal("ok")
	}
	if _, ok := bag["cached"]; ok {
		t.Fatal("cached")
	}
	if _, ok := bag["extra"]; ok {
		t.Fatal("extra")
	}
}

func TestTokensForSessionTraceOmitsWhenNoLLM(t *testing.T) {
	if TokensForSessionTrace([]map[string]any{{"type": "tool"}}) != nil {
		t.Fatal("tool")
	}
	if TokensForSessionTrace(nil) != nil {
		t.Fatal("nil")
	}
	if TokensForSessionTrace([]map[string]any{}) != nil {
		t.Fatal("empty")
	}
}

func TestTokensForSessionTraceOmitsUnknownZeroUsage(t *testing.T) {
	if TokensForSessionTrace([]map[string]any{{"type": "llm", "usage": map[string]any{}}}) != nil {
		t.Fatal("zero usage must omit")
	}
}

func TestTokensForSessionTraceOmitsCachedUnlessReported(t *testing.T) {
	bag := TokensForSessionTrace([]map[string]any{
		{"type": "llm", "usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11,
			"prompt_tokens_details": map[string]any{"cached_tokens": 4},
		}},
	})
	if bag == nil || bag["cached"] != 4 {
		t.Fatalf("%v", bag)
	}
}

func TestTokensForSessionTraceOKFalseOnLLMError(t *testing.T) {
	bag := TokensForSessionTrace([]map[string]any{
		{"type": "llm", "round": 1, "usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11,
		}},
		{"type": "tool", "round": 1},
		{"type": "llm_error", "round": 2, "code": "050001"},
	})
	if bag == nil || bag["prompt"] != 10 || bag["rounds"] != 1 || bag["ok"] != false {
		t.Fatalf("%v", bag)
	}
}
