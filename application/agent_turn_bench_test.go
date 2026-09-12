// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"testing"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// BenchmarkRunAgentTurnDefaultConfig is the product Mode 1 path: scripted LLM,
// Default() debug policy. After A this must not build Detective.
func BenchmarkRunAgentTurnDefaultConfig(b *testing.B) {
	ctx := context.Background()
	req := TurnRequest{
		Message: "what beers are made from fruit?",
		ChatID:  "chat_bench",
		ChatRequest: map[string]any{
			"messages": []any{
				map[string]any{
					"role": "system",
					"content": "You are a helpful Zeus data assistant.\n\n" +
						"## SCOPE BRIEF\nbucket=beer-sample scope=_default\n\n" +
						"## MINI-SCHEMA\nBeer: name, abv\n",
				},
			},
			"verbs": []any{map[string]any{"function": map[string]any{"name": "find"}}},
		},
	}
	opts := RunAgentTurnOpts{
		LLM:         &scriptedLLM{script: nil}, // Complete returns empty content after script drain
		DebugPolicy: config.Default().Debug,
		Version:     "0.1.0",
		StampUser:   "zeus_client",
		Env:         map[string]string{},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "Two fruit beers in Tampa."}}}
		opts.LLM = llm
		got := RunAgentTurn(ctx, req, opts)
		if got.Answer == "" {
			b.Fatal("empty answer")
		}
	}
}

// BenchmarkRunAgentTurnDetectiveOn is Hub Debug Chat gather (explicit on).
func BenchmarkRunAgentTurnDetectiveOn(b *testing.B) {
	ctx := context.Background()
	req := TurnRequest{
		Message: "what beers are made from fruit?",
		ChatID:  "chat_bench",
		ChatRequest: map[string]any{
			"messages": []any{
				map[string]any{
					"role": "system",
					"content": "You are a helpful Zeus data assistant.\n\n" +
						"## SCOPE BRIEF\nbucket=beer-sample scope=_default\n\n" +
						"## MINI-SCHEMA\nBeer: name, abv\n",
				},
			},
			"verbs": []any{map[string]any{"function": map[string]any{"name": "find"}}},
		},
	}
	opts := RunAgentTurnOpts{
		DebugPolicy: config.DebugPolicy{DetectiveBriefing: true},
		Version:     "0.1.0",
		StampUser:   "admin",
		Env:         map[string]string{"ZEUS_CLIENT_DETECTIVE": "1"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts.LLM = &scriptedLLM{script: []any{ports.LlmResponse{Content: "Two fruit beers in Tampa."}}}
		got := RunAgentTurn(ctx, req, opts)
		if got.Debug.Detective == nil {
			b.Fatal("detective")
		}
	}
}
