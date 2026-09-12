// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
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

func TestAgentRunTurnDirectAnswer(t *testing.T) {
	a := NewAgentAPIWith("host", AgentOptions{
		LLM: stubLLM{resp: ports.LlmResponse{Content: "Hello from Zeus."}},
		Config: config.RuntimeConfig{
			Settings: config.ClientSettings{MaxRounds: 4, Mode: "analytics"},
		},
		Version: "0.1.0-dev",
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
}

func boolPtr(v bool) *bool { return &v }
