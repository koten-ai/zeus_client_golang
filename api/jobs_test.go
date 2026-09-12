// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/jobsfake"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type stubJobsPort struct {
	nRun int
}

func (s *stubJobsPort) Run(context.Context, map[string]any) (ports.JobHandle, error) {
	s.nRun++
	return ports.JobHandle{JobID: "job_stub", Status: "running", Seq: 1}, nil
}

func (s *stubJobsPort) Watch(context.Context, string, int) (<-chan ports.JobEvent, error) {
	return nil, domain.NewJob(domain.CodeJobsNotFound, "test.jobs")
}

func (s *stubJobsPort) Get(context.Context, string) (ports.JobSnapshot, error) {
	return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsNotFound, "test.jobs")
}

func (s *stubJobsPort) Cancel(context.Context, string) (ports.JobSnapshot, error) {
	return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsNotFound, "test.jobs")
}

func jobsWithFake(llm ports.LlmPort, zeus ports.ZeusPort) (*JobsAPI, *journal.InMemoryJournal) {
	u, j := unitsAPI(llm, zeus)
	fake := jobsfake.New(UnitHost{Units: u})
	return NewJobsAPIWith("host", JobsOptions{
		Jobs:    fake,
		Journal: j,
		Clock:   ports.SystemClock{},
	}), j
}

func collectJobEvents(t *testing.T, ch <-chan domain.JobEvent) []domain.JobEvent {
	t.Helper()
	if ch == nil {
		t.Fatal("nil watch channel")
	}
	var out []domain.JobEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func TestJobsUnavailableWithoutHost(t *testing.T) {
	j := NewJobsAPIWith("host", JobsOptions{})
	_, err := j.Run(context.Background(), "fan-out", JobsRunParams{
		Units: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	requireAPICode(t, err, domain.CodeJobsUnavailable)
}

func TestJobsNilAPIIs130001(t *testing.T) {
	var j *JobsAPI
	_, err := j.Run(context.Background(), "fan-out", JobsRunParams{
		Units: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	requireAPICode(t, err, domain.CodeJobsUnavailable)
}

func TestFakeJobTwoTargetsAndMonotonicSeq(t *testing.T) {
	zeus := &recordingUnitZeus{}
	jobs, j := jobsWithFake(&scriptedUnitLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "two scopes", JobsRunParams{
		Units: []domain.UnitConfig{
			directUnit("u1", "east", "find"),
			directUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := jobs.Watch(context.Background(), handle.JobID, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := collectJobEvents(t, ch)
	snap, err := jobs.Get(context.Background(), handle.JobID)
	if err != nil {
		t.Fatal(err)
	}
	calls := zeus.snapshot()
	if len(calls) != 2 || calls[0].Target.Bucket != "east" || calls[1].Target.Bucket != "north" {
		t.Fatalf("hops %+v", calls)
	}
	seqs := make([]int, len(events))
	for i, e := range events {
		seqs[i] = e.Seq
		if i > 0 && e.Seq != events[i-1].Seq+1 {
			t.Fatalf("seq not monotonic %v", seqs)
		}
	}
	if len(seqs) == 0 || seqs[0] != 1 || seqs[len(seqs)-1] != len(seqs) {
		t.Fatalf("seq %v", seqs)
	}
	if events[0].Type != journal.EventJobStarted {
		t.Fatalf("first %q", events[0].Type)
	}
	if events[len(events)-1].Type != journal.EventJobFinished {
		t.Fatalf("last %q", events[len(events)-1].Type)
	}
	if snap.Status != "ok" || snap.Partial {
		t.Fatalf("snap %+v", snap)
	}
	foundStart := false
	for _, e := range j.Events() {
		if e.Type == journal.EventJobStarted && e.Component == "api.jobs" {
			foundStart = true
			if e.Data["unit_count"] != 2 {
				t.Fatalf("unit_count %v", e.Data["unit_count"])
			}
		}
	}
	if !foundStart {
		t.Fatal("missing journal job.started")
	}
}

func TestFakeJobPartialOnOneUnitError(t *testing.T) {
	zeus := &recordingUnitZeus{}
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "partial", JobsRunParams{
		Units: []domain.UnitConfig{
			directUnit("u1", "east", "pipeline"),
			directUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := jobs.Get(context.Background(), handle.JobID)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := jobs.Watch(context.Background(), handle.JobID, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := collectJobEvents(t, ch)
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
	finished := false
	for _, e := range events {
		if e.Type == journal.EventJobFinished {
			finished = true
		}
	}
	if !finished {
		t.Fatal("missing job.finished")
	}
}

func TestFakeJobForwardsUnitModelsToAgentTurn(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	jobs, _ := jobsWithFake(llm, zeus)
	_, err := jobs.Run(context.Background(), "fan-out", JobsRunParams{
		Units: []domain.UnitConfig{agentUnit("u1", true)},
		Models: map[string]any{
			"units": map[string]any{
				"u1": map[string]any{"model": "fast-worker-v3"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.calls) == 0 {
		t.Fatal("llm not called")
	}
	if llm.calls[0].Model != "fast-worker-v3" {
		t.Fatalf("model %q", llm.calls[0].Model)
	}
}

func TestFakeJobCancelSetsStatus(t *testing.T) {
	zeus := &recordingUnitZeus{}
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "cancel-me", JobsRunParams{
		Units: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := jobs.Cancel(context.Background(), handle.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "cancelled" {
		t.Fatalf("status %q", snap.Status)
	}
}

func TestJobsInvalidBudgetIs130003BeforePort(t *testing.T) {
	stub := &stubJobsPort{}
	jobs := NewJobsAPIWith("host", JobsOptions{Jobs: stub})
	bag := domain.JobBudgets{MaxWorkers: 0, WallMS: 1, MaxWaves: 1}
	_, err := jobs.Run(context.Background(), "x", JobsRunParams{
		Budgets: &bag,
		Units:   []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	requireAPICode(t, err, domain.CodeJobsBudgetInvalid)
	if stub.nRun != 0 {
		t.Fatal("port.Run must not run on invalid budget")
	}
}

func TestJobsInvalidMapIs130002BeforePort(t *testing.T) {
	stub := &stubJobsPort{}
	jobs := NewJobsAPIWith("host", JobsOptions{Jobs: stub})
	_, err := jobs.Run(context.Background(), "x", JobsRunParams{})
	requireAPICode(t, err, domain.CodeJobsInvalidUnitMap)
	if stub.nRun != 0 {
		t.Fatal("port.Run must not run on invalid map")
	}
}

func TestJobsScopeMapFallback(t *testing.T) {
	zeus := &recordingUnitZeus{}
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, zeus)
	handle, err := jobs.Run(context.Background(), "scope-map", JobsRunParams{
		ScopeMap: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := jobs.Get(context.Background(), handle.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "ok" {
		t.Fatalf("snap %+v", snap)
	}
	if len(zeus.snapshot()) != 1 {
		t.Fatalf("hops %d", len(zeus.snapshot()))
	}
}

func TestJobsUnknownJobIs130004(t *testing.T) {
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, &recordingUnitZeus{})
	_, err := jobs.Get(context.Background(), "job_missing")
	requireAPICode(t, err, domain.CodeJobsNotFound)
	_, err = jobs.Cancel(context.Background(), "job_missing")
	requireAPICode(t, err, domain.CodeJobsNotFound)
	_, err = jobs.Watch(context.Background(), "job_missing", 0)
	requireAPICode(t, err, domain.CodeJobsNotFound)
}

func TestJobEventHasNoSecrets(t *testing.T) {
	zeus := &recordingUnitZeus{}
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, zeus)
	unit := directUnit("u1", "east", "find")
	unit.PasswordEnv = "ZEUS_PASSWORD"
	unit.TokenEnv = "ZEUS_TOKEN"
	unit.LLM = map[string]any{"model": "grok", "api_key": "sk-secret"}
	handle, err := jobs.Run(context.Background(), "scan east", JobsRunParams{Units: []domain.UnitConfig{unit}})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := jobs.Watch(context.Background(), handle.JobID, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := collectJobEvents(t, ch)
	blob := strings.ToLower(fmt.Sprint(events))
	for _, needle := range []string{"sk-secret", "password", "api_key", "zeus_token"} {
		if strings.Contains(blob, needle) {
			t.Fatalf("secret %q in events: %v", needle, events)
		}
	}
}

func TestJobsWatchAfterSeq(t *testing.T) {
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, &recordingUnitZeus{})
	handle, err := jobs.Run(context.Background(), "two", JobsRunParams{
		Units: []domain.UnitConfig{
			directUnit("u1", "east", "find"),
			directUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := jobs.Watch(context.Background(), handle.JobID, 1)
	if err != nil {
		t.Fatal(err)
	}
	events := collectJobEvents(t, ch)
	if len(events) == 0 {
		t.Fatal("expected events after seq 1")
	}
	for _, e := range events {
		if e.Seq <= 1 {
			t.Fatalf("seq %d", e.Seq)
		}
	}
}

func TestJobsGetWatchRace(t *testing.T) {
	jobs, _ := jobsWithFake(&scriptedUnitLLM{}, &recordingUnitZeus{})
	handle, err := jobs.Run(context.Background(), "race", JobsRunParams{
		Units: []domain.UnitConfig{directUnit("u1", "east", "find")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = jobs.Get(context.Background(), handle.JobID)
			ch, err := jobs.Watch(context.Background(), handle.JobID, 0)
			if err != nil {
				return
			}
			for range ch {
			}
		}()
	}
	wg.Wait()
}
