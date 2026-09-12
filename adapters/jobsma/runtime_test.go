// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

package jobsma_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koten-ai/koten_multi_agent_golang/multiagent"
	"github.com/koten-ai/zeus_client_golang/adapters/jobsma"
	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type recordingZeus struct {
	mu    sync.Mutex
	calls []ports.VerbRequest
}

func (r *recordingZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recordingZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return ports.VerbHopResult{OK: true, StatusCode: 200, ReqID: "req-" + req.Target.Bucket, Body: map[string]any{}}, nil
}

func (r *recordingZeus) buckets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = c.Target.Bucket
	}
	return out
}

type scriptedLLM struct {
	mu    sync.Mutex
	calls []ports.LlmRequest
}

func (s *scriptedLLM) Complete(_ context.Context, req ports.LlmRequest) (ports.LlmResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	return ports.LlmResponse{Content: "ok"}, nil
}

type gateZeus struct {
	entered chan struct{}
	once    sync.Once
}

func (g *gateZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (g *gateZeus) CallVerb(ctx context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	g.once.Do(func() { close(g.entered) })
	<-ctx.Done()
	return ports.VerbHopResult{}, ctx.Err()
}

type concZeus struct {
	cur, maxN atomic.Int32
}

func (z *concZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (z *concZeus) CallVerb(context.Context, ports.VerbRequest) (ports.VerbHopResult, error) {
	n := z.cur.Add(1)
	defer z.cur.Add(-1)
	for {
		m := z.maxN.Load()
		if n <= m || z.maxN.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(40 * time.Millisecond)
	return ports.VerbHopResult{OK: true, StatusCode: 200, ReqID: "req-c", Body: map[string]any{}}, nil
}

func directUnit(id, bucket, verb string) domain.UnitConfig {
	return domain.UnitConfig{
		UnitID:     id,
		Kind:       domain.UnitKindZeusDirect,
		Goal:       "scan " + bucket,
		ZeusURL:    "http://127.0.0.1:8080",
		Bucket:     bucket,
		Scope:      "sales",
		Collection: "_default",
		Call:       map[string]any{"verb": verb, "body": map[string]any{"entity_type": "Beer"}},
	}
}

func agentUnit(id string) domain.UnitConfig {
	return domain.UnitConfig{
		UnitID:      id,
		Kind:        domain.UnitKindAgentTurn,
		Goal:        "shortlist fruit beers",
		ZeusURL:     "http://127.0.0.1:8080",
		Bucket:      "beer-sample",
		Scope:       "sales",
		Collection:  "_default",
		CatalogMode: "analytics",
		ChatRequest: map[string]any{
			"messages": []any{map[string]any{"role": "system", "content": "## SCOPE BRIEF\nscope: b/s\n"}},
		},
	}
}

func testJobs(t *testing.T, llm ports.LlmPort, zeus ports.ZeusPort) (*api.JobsAPI, *jobsma.Runtime) {
	t.Helper()
	j := journal.NewInMemoryJournal(nil)
	cfg := config.RuntimeConfig{
		Target: config.DataTarget{Bucket: "west", Scope: "s", Collection: "c"},
		Zeus:   config.ZeusEndpointConfig{URL: "http://127.0.0.1:8080"},
		LLM: config.LlmProviderConfig{
			Model:     "default-model",
			APIKeyEnv: "LLM_DEFAULT_KEY",
			Roles: map[string]config.LlmRoleConfig{
				"worker": {Model: "fast-worker", APIKeyEnv: "LLM_DEFAULT_KEY"},
			},
		},
		Settings: config.ClientSettings{DurableSessions: false, Mode: "analytics", MaxRounds: 4},
	}
	u := api.NewUnitsAPIWith("host", api.UnitsOptions{
		LLM: llm, Zeus: zeus, Journal: j, Config: cfg, Version: "0.1.0", Clock: ports.SystemClock{},
	})
	rt := jobsma.New(api.UnitHost{Units: u})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	jobs := api.NewJobsAPIWith("host", api.JobsOptions{Jobs: rt, Journal: j, Clock: ports.SystemClock{}})
	return jobs, rt
}

func waitSnap(t *testing.T, jobs *api.JobsAPI, jobID string) domain.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last domain.JobSnapshot
	for time.Now().Before(deadline) {
		snap, err := jobs.Get(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		last = snap
		if snap.Status != "" && snap.Status != "running" {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for job %s (last %+v)", jobID, last)
	return last
}

func requireCode(t *testing.T, err error, code domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("got %v want %s", err, code)
	}
}

func TestPatternATwoIsolatedUnits(t *testing.T) {
	zeus := &recordingZeus{}
	jobs, _ := testJobs(t, &scriptedLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "two scopes", api.JobsRunParams{
		Units: []domain.UnitConfig{
			directUnit("u1", "east", "find"),
			directUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitSnap(t, jobs, handle.JobID)
	if snap.Status != "ok" || snap.Partial {
		t.Fatalf("snap %+v", snap)
	}
	got := map[string]bool{}
	for _, b := range zeus.buckets() {
		got[b] = true
	}
	if !got["east"] || !got["north"] {
		t.Fatalf("buckets %v", zeus.buckets())
	}
	ch, err := jobs.Watch(context.Background(), handle.JobID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	var last int
	for ev := range ch {
		if ev.Seq <= last {
			t.Fatalf("seq not monotonic %d after %d", ev.Seq, last)
		}
		last = ev.Seq
		types = append(types, ev.Type)
	}
	if len(types) == 0 || types[0] != "job.started" || types[len(types)-1] != "job.finished" {
		t.Fatalf("events %v", types)
	}
}

func TestPatternAPartialOnOneUnitError(t *testing.T) {
	zeus := &recordingZeus{}
	jobs, _ := testJobs(t, &scriptedLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "partial", api.JobsRunParams{
		Units: []domain.UnitConfig{
			directUnit("u1", "east", "pipeline"),
			directUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitSnap(t, jobs, handle.JobID)
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

func TestPatternAForwardsModelsToAgentTurn(t *testing.T) {
	llm := &scriptedLLM{}
	jobs, _ := testJobs(t, llm, &recordingZeus{})
	handle, err := jobs.Run(context.Background(), "fan-out", api.JobsRunParams{
		Units: []domain.UnitConfig{agentUnit("u1")},
		Models: map[string]any{
			"units": map[string]any{
				"u1": map[string]any{"model": "fast-worker-v3"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitSnap(t, jobs, handle.JobID)
	if snap.Status != "ok" {
		t.Fatalf("snap %+v", snap)
	}
	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.calls) == 0 {
		t.Fatal("llm not called")
	}
	if llm.calls[0].Model != "fast-worker-v3" {
		t.Fatalf("model %q", llm.calls[0].Model)
	}
}

func TestPatternACancelStopsInFlightHTTP(t *testing.T) {
	zeus := &gateZeus{entered: make(chan struct{})}
	jobs, _ := testJobs(t, &scriptedLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "cancel-me", api.JobsRunParams{
		Units: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-zeus.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("CallVerb never started")
	}
	snap, err := jobs.Cancel(context.Background(), handle.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "cancelled" {
		t.Fatalf("cancel snap %q", snap.Status)
	}
	done := waitSnap(t, jobs, handle.JobID)
	if done.Status != "cancelled" {
		t.Fatalf("final %q", done.Status)
	}
}

func TestPatternAStalePublishRejected(t *testing.T) {
	jobs, rt := testJobs(t, &scriptedLLM{}, &recordingZeus{})
	handle, err := jobs.Run(context.Background(), "fence", api.JobsRunParams{
		Units: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitSnap(t, jobs, handle.JobID)
	if err := rt.Store().SetPlanEpoch(handle.JobID, 1); err != nil {
		t.Fatal(err)
	}
	err = rt.Store().Publish(handle.JobID, multiagent.UnitResult{
		UnitID: "zombie", PlanEpoch: 0, Status: multiagent.UnitOK,
	})
	if err == nil {
		t.Fatal("expected stale publish reject")
	}
}

func TestPatternAMaxWorkersCapsConcurrency(t *testing.T) {
	zeus := &concZeus{}
	jobs, _ := testJobs(t, &scriptedLLM{}, zeus)
	bag := domain.DefaultJobBudgets()
	bag.MaxWorkers = 2
	units := make([]domain.UnitConfig, 4)
	for i := 0; i < 4; i++ {
		units[i] = directUnit("u"+string(rune('1'+i)), "east", "find")
		units[i].UnitID = "u" + string(rune('a'+i))
	}
	handle, err := jobs.Run(context.Background(), "cap", api.JobsRunParams{Budgets: &bag, Units: units})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitSnap(t, jobs, handle.JobID)
	if snap.Status != "ok" {
		t.Fatalf("snap %+v", snap)
	}
	if n := zeus.maxN.Load(); n > 2 {
		t.Fatalf("concurrent CallVerb %d want <= 2", n)
	}
	if n := zeus.maxN.Load(); n < 1 {
		t.Fatal("no hops")
	}
}

func TestPatternAJournalRace(t *testing.T) {
	zeus := &recordingZeus{}
	jobs, _ := testJobs(t, &scriptedLLM{}, zeus)
	units := make([]domain.UnitConfig, 8)
	for i := 0; i < 8; i++ {
		units[i] = directUnit("u", "east", "find")
		units[i].UnitID = "u" + string(rune('a'+i))
	}
	handle, err := jobs.Run(context.Background(), "race", api.JobsRunParams{Units: units})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitSnap(t, jobs, handle.JobID)
	if snap.Status != "ok" {
		t.Fatalf("snap %+v", snap)
	}
	if len(zeus.buckets()) != 8 {
		t.Fatalf("hops %d", len(zeus.buckets()))
	}
}

func TestPatternAUnknownJobIs130004(t *testing.T) {
	jobs, _ := testJobs(t, &scriptedLLM{}, &recordingZeus{})
	_, err := jobs.Get(context.Background(), "job_missing")
	requireCode(t, err, domain.CodeJobsNotFound)
	_, err = jobs.Cancel(context.Background(), "job_missing")
	requireCode(t, err, domain.CodeJobsNotFound)
	_, err = jobs.Watch(context.Background(), "job_missing", 0)
	requireCode(t, err, domain.CodeJobsNotFound)
}

func TestPatternANilHostIs130001(t *testing.T) {
	rt := jobsma.New(nil)
	defer rt.Close(context.Background())
	_, err := rt.Run(context.Background(), map[string]any{
		"goal":  "x",
		"units": []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	requireCode(t, err, domain.CodeJobsUnavailable)
}
