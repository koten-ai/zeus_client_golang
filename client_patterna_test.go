// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

package zeusclient

import (
	"context"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/jobsma"
	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func waitClientJob(t *testing.T, c *Client, jobID string) domain.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var snap domain.JobSnapshot
	var err error
	for time.Now().Before(deadline) {
		snap, err = c.Jobs().Get(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Status != "" && snap.Status != "running" {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for job %s (last %+v)", jobID, snap)
	return snap
}

func patternAClient(t *testing.T, llm ports.LlmPort, zeus ports.ZeusPort) *Client {
	t.Helper()
	cfg := config.RuntimeConfig{
		Target: config.DataTarget{Bucket: "west", Scope: "s", Collection: "c"},
		Zeus:   config.ZeusEndpointConfig{URL: "http://127.0.0.1:8080"},
		LLM: config.LlmProviderConfig{
			Model:     "fast-worker",
			APIKeyEnv: "LLM_WORKER_KEY",
			Roles: map[string]config.LlmRoleConfig{
				"orchestrator": {Model: "strong-planner", APIKeyEnv: "LLM_ORCH_KEY"},
				"advisor":      {Model: "strong-planner", APIKeyEnv: "LLM_ADV_KEY"},
				"worker":       {Model: "fast-worker", APIKeyEnv: "LLM_WORKER_KEY"},
			},
		},
		Settings: config.ClientSettings{DurableSessions: false, Mode: "analytics", MaxRounds: 4},
	}
	j := journal.NewInMemoryJournal(nil)
	c, err := New(Options{Config: &cfg, Zeus: zeus, LLM: llm, Journal: j, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.BindJobs(jobsma.New(api.UnitHost{Units: c.Units()})); err != nil {
		t.Fatal(err)
	}
	return c
}

func demoAgentUnit(id, bucket, scope string) domain.UnitConfig {
	return domain.UnitConfig{
		UnitID:      id,
		Kind:        domain.UnitKindAgentTurn,
		Goal:        "shortlist fruit beers for sales",
		ZeusURL:     "http://127.0.0.1:8080",
		Bucket:      bucket,
		Scope:       scope,
		Collection:  "_default",
		CatalogMode: "analytics",
		BaseID:      "base-5-mock",
		ChatRequest: map[string]any{
			"messages": []any{map[string]any{
				"role":    "system",
				"content": "## SCOPE BRIEF\nbucket: " + bucket + "\nscope: " + scope + "\n\n## MINI-SCHEMA\nentity_type: Beer\n",
			}},
		},
	}
}

type scriptedJobsLLM struct {
	n int
}

func (s *scriptedJobsLLM) Complete(_ context.Context, _ ports.LlmRequest) (ports.LlmResponse, error) {
	s.n++
	if s.n == 1 {
		return ports.LlmResponse{
			ToolCalls: []map[string]any{
				{
					"id":   "c1",
					"type": "function",
					"function": map[string]any{
						"name":      "find",
						"arguments": `{"entity_type": "Beer"}`,
					},
				},
			},
		}, nil
	}
	return ports.LlmResponse{Content: "sales shortlist: cherry lambic"}, nil
}

func TestClientJobsPatternA(t *testing.T) {
	zeus := &recordingJobsZeus{}
	c := patternAClient(t, &scriptedJobsLLM{}, zeus)
	handle, err := c.Jobs().Run(context.Background(), "two scopes", api.JobsRunParams{
		Units: []domain.UnitConfig{
			clientDirectUnit("u1", "east", "find"),
			clientDirectUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitClientJob(t, c, handle.JobID)
	if snap.Status != "ok" || snap.Partial {
		t.Fatalf("snap %+v", snap)
	}
	zeus.mu.Lock()
	defer zeus.mu.Unlock()
	got := map[string]bool{}
	for _, call := range zeus.calls {
		got[call.Target.Bucket] = true
	}
	if !got["east"] || !got["north"] {
		t.Fatalf("hops %+v", zeus.calls)
	}
}

func TestClientPatternADemoDirectAndAgentTurn(t *testing.T) {
	zeus := &recordingJobsZeus{}
	c := patternAClient(t, &scriptedJobsLLM{}, zeus)
	bag := domain.DefaultJobBudgets()
	handle, err := c.Jobs().Run(context.Background(),
		"Fruit beer sales shortlist + inventory risk",
		api.JobsRunParams{
			Pack:    "desk",
			Budgets: &bag,
			Units: []domain.UnitConfig{
				demoAgentUnit("unit_sales", "beer-sample", "sales"),
				clientDirectUnit("unit_inventory", "inventory", "find"),
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitClientJob(t, c, handle.JobID)
	if snap.Status != "ok" || snap.Partial {
		t.Fatalf("snap %+v", snap)
	}
	zeus.mu.Lock()
	defer zeus.mu.Unlock()
	got := map[string]bool{}
	for _, call := range zeus.calls {
		got[call.Target.Bucket] = true
	}
	if !got["beer-sample"] || !got["inventory"] {
		t.Fatalf("hops %+v", zeus.calls)
	}
}

func TestClientPatternADemoPartial(t *testing.T) {
	c := patternAClient(t, &scriptedJobsLLM{}, &recordingJobsZeus{})
	handle, err := c.Jobs().Run(context.Background(), "partial", api.JobsRunParams{
		Units: []domain.UnitConfig{
			demoAgentUnit("unit_sales", "beer-sample", "sales"),
			clientDirectUnit("unit_inventory", "inventory", "pipeline"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitClientJob(t, c, handle.JobID)
	if !snap.Partial || snap.Status != "partial" {
		t.Fatalf("snap %+v", snap)
	}
	got := map[string]bool{}
	for _, s := range snap.UnitSummaries {
		st, _ := s["status"].(string)
		got[st] = true
	}
	if !got["error"] || !got["ok"] {
		t.Fatalf("summaries %v", snap.UnitSummaries)
	}
}
