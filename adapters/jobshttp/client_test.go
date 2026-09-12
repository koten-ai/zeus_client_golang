// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func requireCode(t *testing.T, err error, code domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("got %v want %s", err, code)
	}
}

func drainWatch(t *testing.T, ch <-chan ports.JobEvent) []ports.JobEvent {
	t.Helper()
	var out []ports.JobEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func sseBody(payloads ...string) string {
	var b strings.Builder
	for _, p := range payloads {
		b.WriteString("data: ")
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	return b.String()
}

func TestWatchSSEFromSeqAndOrder(t *testing.T) {
	var gotFrom []string
	var gotLast []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/jobs/j1/events" {
			http.NotFound(w, r)
			return
		}
		gotFrom = append(gotFrom, r.URL.Query().Get(FromSeqParam))
		gotLast = append(gotLast, r.Header.Get(LastEventIDHeader))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody(
			`{"seq":1,"type":"job.started","job_id":"j1","ts":0}`,
			`{"seq":2,"type":"job.finished","job_id":"j1","ts":1}`,
		))
	}))
	t.Cleanup(srv.Close)

	rt := New(srv.URL, Options{HTTP: srv.Client()})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })

	ch, err := rt.Watch(context.Background(), "j1", 0)
	if err != nil {
		t.Fatal(err)
	}
	events := drainWatch(t, ch)
	ch2, err := rt.Watch(context.Background(), "j1", 1)
	if err != nil {
		t.Fatal(err)
	}
	skipped := drainWatch(t, ch2)
	if len(gotFrom) != 2 || gotFrom[0] != "0" || gotFrom[1] != "1" {
		t.Fatalf("from_seq %v", gotFrom)
	}
	if len(gotLast) != 2 || gotLast[0] != "0" || gotLast[1] != "1" {
		t.Fatalf("Last-Event-ID %v", gotLast)
	}
	if len(events) != 2 || events[0].Type != "job.started" || events[1].Seq != 2 {
		t.Fatalf("events %+v", events)
	}
	if len(skipped) != 1 || skipped[0].Seq != 2 {
		t.Fatalf("skipped %+v", skipped)
	}
}

func TestWatch404Is130004(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	rt := New(srv.URL, Options{HTTP: srv.Client()})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	_, err := rt.Watch(context.Background(), "missing", 0)
	requireCode(t, err, domain.CodeJobsNotFound)
}

func TestWatch503Is130001(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	rt := New(srv.URL, Options{HTTP: srv.Client()})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	_, err := rt.Watch(context.Background(), "j1", 0)
	requireCode(t, err, domain.CodeJobsUnavailable)
}

func TestMalformedSSEIs130005(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: not-json\n\n")
	}))
	t.Cleanup(srv.Close)
	rt := New(srv.URL, Options{HTTP: srv.Client()})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	_, err := rt.Watch(context.Background(), "j1", 0)
	requireCode(t, err, domain.CodeJobsWatchFailed)
}

func TestRunGetCancelUnavailable(t *testing.T) {
	rt := New("http://jobs.test:7090", Options{})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	_, err := rt.Run(context.Background(), map[string]any{"goal": "x"})
	requireCode(t, err, domain.CodeJobsUnavailable)
	_, err = rt.Get(context.Background(), "j1")
	requireCode(t, err, domain.CodeJobsUnavailable)
	_, err = rt.Cancel(context.Background(), "j1")
	requireCode(t, err, domain.CodeJobsUnavailable)
}

func TestRunRejectsRawAPIKey(t *testing.T) {
	rt := New("http://jobs.test:7090", Options{})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	_, err := rt.Run(context.Background(), map[string]any{"api_key": "sk-secret"})
	requireCode(t, err, domain.CodeJobsInvalidUnitMap)
}

func TestEventsURL(t *testing.T) {
	got := EventsURL("http://jobs.test:7090/", "j1")
	if got != "http://jobs.test:7090/v1/jobs/j1/events" {
		t.Fatal(got)
	}
	if JobsEventsPath != "/v1/jobs/{job_id}/events" {
		t.Fatal(JobsEventsPath)
	}
}

func TestWatchCtxCancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	rt := New(srv.URL, Options{HTTP: srv.Client()})
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	_, err := rt.Watch(ctx, "j1", 0)
	if err == nil {
		t.Fatal("expected cancel")
	}
}
