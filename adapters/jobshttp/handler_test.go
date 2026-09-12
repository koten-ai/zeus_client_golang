// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type memJobs struct {
	events map[string][]ports.JobEvent
}

func (m *memJobs) Run(context.Context, map[string]any) (ports.JobHandle, error) {
	return ports.JobHandle{}, domain.NewJob(domain.CodeJobsUnavailable, "test")
}

func (m *memJobs) Get(context.Context, string) (ports.JobSnapshot, error) {
	return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsUnavailable, "test")
}

func (m *memJobs) Cancel(context.Context, string) (ports.JobSnapshot, error) {
	return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsUnavailable, "test")
}

func (m *memJobs) Watch(_ context.Context, jobID string, afterSeq int) (<-chan ports.JobEvent, error) {
	evs, ok := m.events[jobID]
	if !ok {
		return nil, domain.NewJob(domain.CodeJobsNotFound, "test")
	}
	var buf []ports.JobEvent
	for _, ev := range evs {
		if ev.Seq > afterSeq {
			buf = append(buf, ev)
		}
	}
	ch := make(chan ports.JobEvent, len(buf))
	for _, ev := range buf {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func sampleJob() *memJobs {
	return &memJobs{events: map[string][]ports.JobEvent{
		"j1": {
			{Seq: 1, Type: "job.started", JobID: "j1", Payload: map[string]any{"summary": "go"}},
			{Seq: 2, Type: "unit.finished", JobID: "j1", UnitID: "u1", PlanEpoch: 1, Payload: map[string]any{"status": "ok", "dead_end": false}},
			{Seq: 3, Type: "job.finished", JobID: "j1", Payload: map[string]any{"status": "ok"}},
		},
	}}
}

func TestHandlerRoundTripWireAndResume(t *testing.T) {
	h := &Handler{Jobs: sampleJob()}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	rt := New(srv.URL, Options{HTTP: srv.Client()})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	ch, err := rt.Watch(context.Background(), "j1", 0)
	if err != nil {
		t.Fatal(err)
	}
	all := drainWatch(t, ch)
	if len(all) != 3 || all[0].Type != "job.started" || all[1].UnitID != "u1" || all[2].Type != "job.finished" {
		t.Fatalf("%+v", all)
	}
	if all[1].Payload["status"] != "ok" || all[0].Payload["summary"] != "go" {
		t.Fatalf("payload %#v %#v", all[0].Payload, all[1].Payload)
	}

	ch, err = rt.Watch(context.Background(), "j1", 1)
	if err != nil {
		t.Fatal(err)
	}
	skipped := drainWatch(t, ch)
	if len(skipped) != 2 || skipped[0].Seq != 2 {
		t.Fatalf("resume %+v", skipped)
	}

	_, err = rt.Watch(context.Background(), "missing", 0)
	requireCode(t, err, domain.CodeJobsNotFound)
}

func TestHandlerFromSeqWinsOverLastEventID(t *testing.T) {
	h := &Handler{Jobs: sampleJob()}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/jobs/j1/events?from_seq=2", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(LastEventIDHeader, "0")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events, err := readSSEEvents(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("from_seq should win: %+v", events)
	}
}

func TestHandlerLastEventIDWhenNoQuery(t *testing.T) {
	h := &Handler{Jobs: sampleJob()}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/jobs/j1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(LastEventIDHeader, "2")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	events, err := readSSEEvents(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("Last-Event-ID resume: %+v", events)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	h := &Handler{Jobs: sampleJob()}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Post(srv.URL+"/v1/jobs/j1/events", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
