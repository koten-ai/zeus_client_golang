// SPDX-License-Identifier: BUSL-1.1

package config

import "testing"

func TestWorkerInheritsDefaultThenRoleThenUnit(t *testing.T) {
	base := LlmProviderConfig{
		Model:     "fast-worker",
		APIKeyEnv: "LLM_DEFAULT_KEY",
		Roles: map[string]LlmRoleConfig{
			"worker":       {Model: "fast-worker", APIKeyEnv: "LLM_WORKER_KEY"},
			"orchestrator": {Model: "strong-planner", APIKeyEnv: "LLM_ORCH_KEY"},
		},
	}
	got := ResolveLlmSlice(base, ResolveLlmOpts{
		Role: LlmRoleWorker,
		Jobs: &JobsConfig{Models: map[string]any{"worker": map[string]any{"model": "fast-worker-v2"}}},
		JobModels: map[string]any{
			"units": map[string]any{"u1": map[string]any{"model": "fast-worker-v3"}},
		},
		UnitLLM: map[string]any{"temperature": 0.1},
		UnitID:  "u1",
	})
	if got.Model != "fast-worker-v3" {
		t.Fatalf("model %q", got.Model)
	}
	if got.APIKeyEnv != "LLM_WORKER_KEY" {
		t.Fatalf("key env %q", got.APIKeyEnv)
	}
	if got.Temperature == nil || *got.Temperature != 0.1 {
		t.Fatalf("temp %v", got.Temperature)
	}
	if got.Role != LlmRoleWorker {
		t.Fatalf("role %q", got.Role)
	}
	pub := got.ToPublicDict()
	if pub["role"] != "worker" || pub["model"] != "fast-worker-v3" {
		t.Fatalf("%v", pub)
	}
	if _, ok := pub["api_key"]; ok {
		t.Fatal("api_key must not appear")
	}
}

func TestOrchestratorIgnoresUnitLLM(t *testing.T) {
	base := LlmProviderConfig{
		Model:     "fast-worker",
		APIKeyEnv: "LLM_DEFAULT_KEY",
		Roles: map[string]LlmRoleConfig{
			"orchestrator": {Model: "strong-planner", APIKeyEnv: "LLM_ORCH_KEY"},
		},
	}
	got := ResolveLlmSlice(base, ResolveLlmOpts{
		Role:    LlmRoleOrchestrator,
		UnitLLM: map[string]any{"model": "should-not-win", "api_key_env": "LLM_UNIT_KEY"},
		UnitID:  "u1",
	})
	if got.Model != "strong-planner" || got.APIKeyEnv != "LLM_ORCH_KEY" || got.Role != LlmRoleOrchestrator {
		t.Fatalf("%+v", got)
	}
}

func TestEmptyRolesUsesDefaultLLM(t *testing.T) {
	base := LlmProviderConfig{Model: "fast-worker", APIKeyEnv: "LLM_DEFAULT_KEY"}
	got := ResolveLlmSlice(base, ResolveLlmOpts{Role: LlmRoleWorker})
	if got.Model != "fast-worker" || got.APIKeyEnv != "LLM_DEFAULT_KEY" || got.Role != "worker" {
		t.Fatalf("%+v", got)
	}
}
