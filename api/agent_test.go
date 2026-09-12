// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/observability"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type stubLLM struct {
	resp ports.LlmResponse
}

func (s stubLLM) Complete(context.Context, ports.LlmRequest) (ports.LlmResponse, error) {
	return s.resp, nil
}

func TestAgentRunTurnRequiresLLM(t *testing.T) {
	a := NewAgentAPIWith("host", AgentOptions{})
	_, err := a.RunTurn(context.Background(), "hi", RunTurnParams{})
	if err == nil {
		t.Fatal("expected error")
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeNotImplemented {
		t.Fatalf("got %v", err)
	}
}

func TestAgentDefaultMiddlewareIncludesSecurityHooks(t *testing.T) {
	a := NewAgentAPIWith("host", AgentOptions{})
	mw := a.Middleware()
	if mw == nil || len(mw.Items) != 1 || mw.Items[0].Name() != "security" {
		t.Fatalf("%+v", mw)
	}
}

func TestAgentRunTurnDefaultSkipsDetective(t *testing.T) {
	a := NewAgentAPIWith("host", AgentOptions{
		LLM:     stubLLM{resp: ports.LlmResponse{Content: "Hello from Zeus."}},
		Config:  config.Default(),
		Version: "0.1.0",
	})
	got, err := a.RunTurn(context.Background(), "hi", RunTurnParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Debug.Detective != nil {
		t.Fatal("default Agent.RunTurn must not build detective")
	}
	if _, ok := got.Debug.PublicTrace["detective"]; ok {
		t.Fatal("public_trace.detective")
	}
}

func TestAgentRunTurnDirectAnswer(t *testing.T) {
	metrics := observability.NewInMemoryMetrics()
	a := NewAgentAPIWith("host", AgentOptions{
		LLM: stubLLM{resp: ports.LlmResponse{Content: "Hello from Zeus."}},
		Config: config.RuntimeConfig{
			Settings: config.ClientSettings{MaxRounds: 4, Mode: "analytics"},
		},
		Version: "0.1.0-dev",
		Metrics: metrics,
	})
	got, err := a.RunTurn(context.Background(), "hi", RunTurnParams{
		EnableSessions: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Answer != "Hello from Zeus." || got.Status != application.TurnOK {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Answer, "wish_i_knew") {
		t.Fatal("g2")
	}
	snap := metrics.Snapshot()
	counters, _ := snap["counters"].(map[string]any)
	if _, ok := counters["zeus_client_turns_total"]; !ok {
		t.Fatalf("metrics %v", snap)
	}
}

func boolPtr(v bool) *bool { return &v }

type stubRichCatalog struct {
	body   map[string]any
	schema map[string]any
}

func (s *stubRichCatalog) Load(context.Context, ports.CatalogKey) (ports.CatalogDocument, error) {
	return ports.CatalogDocument{Body: s.body}, nil
}

func (s *stubRichCatalog) Save(context.Context, ports.CatalogKey, ports.CatalogDocument) error {
	return nil
}

func (s *stubRichCatalog) LoadRich(context.Context, ports.CatalogKey) (domain.LoadedCatalog, error) {
	return domain.LoadedCatalog{Body: s.body, ResponseOutputSchema: s.schema, Source: "test"}, nil
}

func agentToolCall(name string, args map[string]any, id string) map[string]any {
	raw, _ := json.Marshal(args)
	return map[string]any{
		"id":   id,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": string(raw),
		},
	}
}

func TestAgentLoadForTurnBindsPackSchema(t *testing.T) {
	schema := map[string]any{
		"required": []any{"summary", "query_decomposition", "decomposition", "confidence"},
	}
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "## SCOPE BRIEF\nbucket=b\n"},
		},
		"verbs": []any{map[string]any{"function": map[string]any{"name": "return"}}},
	}
	returnTool := map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "return", "parameters": map[string]any{"type": "object"}},
	}
	llm := stubLLM{resp: ports.LlmResponse{ToolCalls: []map[string]any{
		agentToolCall("return", map[string]any{
			"summary":       "Done.",
			"decomposition": map[string]any{"steps": []any{}},
			"confidence":    "high",
		}, "c1"),
	}}}
	a := NewAgentAPIWith("host", AgentOptions{
		LLM:     llm,
		Zeus:    &recZeus{},
		Catalog: &stubRichCatalog{body: body, schema: schema},
		Config:  config.Default(),
		Version: "0.1.0",
	})
	got, err := a.RunTurn(context.Background(), "hi", RunTurnParams{
		EnableSessions: boolPtr(false),
		Tools:          []map[string]any{returnTool},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	if got.LayerA != nil {
		for _, e := range got.LayerA.Errors {
			if strings.Contains(e, "pack schema required field query_decomposition") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("want pack schema error, layer=%+v notes=%v", got.LayerA, got.Debug.Notes)
	}
	noteOK := false
	for _, n := range got.Debug.Notes {
		if strings.Contains(n, "chat_request:") {
			noteOK = true
		}
	}
	if !noteOK {
		t.Fatalf("notes %v", got.Debug.Notes)
	}
}

type recLLM struct {
	calls []ports.LlmRequest
	resp  ports.LlmResponse
}

func (r *recLLM) Complete(_ context.Context, req ports.LlmRequest) (ports.LlmResponse, error) {
	r.calls = append(r.calls, req)
	return r.resp, nil
}

func TestAgentSettingsOverlayHonorIgnoreFalse(t *testing.T) {
	rec := &recLLM{resp: ports.LlmResponse{Content: "ok"}}
	s := config.Default().Settings
	s.IgnoreUserToolPathHints = false
	s.AIProcessResult = false
	catalog := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "system",
				"content": "You are a helpful agent.\n\n" +
					"## SCOPE BRIEF\nbucket=beer\n\n" +
					"## MINI-SCHEMA\nBeer: name\n",
			},
		},
	}
	a := NewAgentAPIWith("host", AgentOptions{
		LLM: rec,
		Config: config.RuntimeConfig{
			Settings: config.Default().Settings,
		},
		Version: "0.1.0",
	})
	_, err := a.RunTurn(context.Background(), "do not use pipeline", RunTurnParams{
		EnableSessions: boolPtr(false),
		Settings:       &s,
		ChatRequest:    catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) == 0 {
		t.Fatal("no llm")
	}
	sys, _ := rec.calls[0].Messages[0]["content"].(string)
	if !strings.Contains(sys, "prefer that path if it remains legal") {
		t.Fatalf("want honor inject: %s", sys)
	}
}
