// SPDX-License-Identifier: BUSL-1.1

package zeusclient

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/jobsfake"
	"github.com/koten-ai/zeus_client_golang/adapters/jobsma"
	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func isolatedOpts() Options {
	return Options{Env: map[string]string{}}
}

type fakeHTTP struct {
	mu     sync.Mutex
	closed bool
	closes int
}

func (f *fakeHTTP) Request(_ context.Context, _ ports.HTTPRequest) (ports.HTTPResponse, error) {
	return ports.HTTPResponse{Status: 200, Headers: map[string]string{}}, nil
}

func (f *fakeHTTP) Close(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	f.closed = true
	return nil
}

func (f *fakeHTTP) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeHTTP) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closes
}

type fakeZeus struct {
	closed atomic.Bool
}

func (f *fakeZeus) ResolveAuth(_ context.Context, _ config.DataTarget, _ bool) (ports.AuthContext, error) {
	return ports.AuthContext{Headers: map[string]string{"X-Test": "1"}, Mode: "none"}, nil
}

func (f *fakeZeus) CallVerb(_ context.Context, _ ports.VerbRequest) (ports.VerbHopResult, error) {
	return ports.VerbHopResult{}, domain.New(domain.CodeNotImplemented, "test.zeus")
}

type fakeZeusHop struct {
	n   atomic.Int32
	hop ports.VerbHopResult
}

func (f *fakeZeusHop) ResolveAuth(_ context.Context, _ config.DataTarget, _ bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (f *fakeZeusHop) CallVerb(_ context.Context, _ ports.VerbRequest) (ports.VerbHopResult, error) {
	f.n.Add(1)
	return f.hop, nil
}

func (f *fakeZeus) Close(_ context.Context) error {
	f.closed.Store(true)
	return nil
}

var (
	_ ports.HttpPort = (*fakeHTTP)(nil)
	_ ports.ZeusPort = (*fakeZeus)(nil)
	_ ports.ZeusPort = (*fakeZeusHop)(nil)
	_ ports.Closer   = (*fakeZeus)(nil)
	_ ports.Closer   = (*fakeHTTP)(nil)
)

func TestProductStampVersionTracksPackage(t *testing.T) {
	s := domain.ProductStamp(domain.StampOptions{Version: Version})
	if s["version"] != Version {
		t.Fatalf("stamp version %v package %s", s["version"], Version)
	}
	if s["user"] != domain.ProductUser {
		t.Fatalf("user %v", s["user"])
	}
	if err := domain.AssertProductStamp(s); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeFromConfigDevelopment(t *testing.T) {
	c, err := New(Options{Profile: "development", Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cfg := c.Config()
	if cfg.Zeus.URL == "" {
		t.Fatal("zeus.url")
	}
	if cfg.Profile != "development" {
		t.Fatalf("profile %q", cfg.Profile)
	}
	if c.Journal() == nil {
		t.Fatal("journal")
	}
	svc := c.Services()
	if svc.Secrets == nil {
		t.Fatal("secrets")
	}
	if svc.Clock == nil {
		t.Fatal("clock")
	}
	if svc.IDs == nil {
		t.Fatal("ids")
	}
	if svc.Redactor == nil {
		t.Fatal("redactor")
	}
	if svc.HTTP == nil {
		t.Fatal("default HTTP transport")
	}
	if svc.Zeus != nil || svc.LLM != nil || svc.Catalog != nil || svc.Jobs != nil {
		t.Fatal("zeus/llm/catalog/jobs must stay nil unless injected")
	}
	if c.Catalog() == nil {
		t.Fatal("Catalog handle")
	}
	if cfg.SemanticCache.Enabled {
		t.Fatal("semantic cache")
	}
}

func TestRuntimeClosesHTTPAndAdapters(t *testing.T) {
	http := &fakeHTTP{}
	zeus := &fakeZeus{}
	cfg := config.Default()
	c, err := New(Options{Config: &cfg, HTTP: http, Zeus: zeus})
	if err != nil {
		t.Fatal(err)
	}
	if http.isClosed() {
		t.Fatal("closed before Close")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if !http.isClosed() {
		t.Fatal("http not closed")
	}
	if !zeus.closed.Load() {
		t.Fatal("zeus not closed")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if http.closeCount() != 1 {
		t.Fatalf("Close must be idempotent, closes=%d", http.closeCount())
	}
}

func TestRuntimeOverrideInjection(t *testing.T) {
	zeus := &fakeZeus{}
	c, err := New(Options{Env: map[string]string{}, Zeus: zeus})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Services().Zeus != zeus {
		t.Fatal("injected Zeus is not the one on services")
	}
	auth, err := c.Services().Zeus.ResolveAuth(context.Background(), c.Config().Target, false)
	if err != nil {
		t.Fatal(err)
	}
	if auth.Headers["X-Test"] != "1" {
		t.Fatalf("%v", auth.Headers)
	}
}

func TestServicesBundleDefaults(t *testing.T) {
	c, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	svc := c.Services()
	if svc.Journal == nil {
		t.Fatal("journal")
	}
	if svc.Clock.NowMS(context.Background()) <= 0 {
		t.Fatal("clock.NowMS")
	}
	id := svc.IDs.TurnID(context.Background())
	if !domain.IsUUIDv4(id) {
		t.Fatalf("TurnID %q", id)
	}
}

func TestTwoClientsDoNotShareJournal(t *testing.T) {
	c1, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	j1, j2 := c1.Journal(), c2.Journal()
	if j1 == nil || j2 == nil {
		t.Fatal("journal")
	}
	if j1 == j2 {
		t.Fatal("New must not share one journal")
	}
	if c1.Services().HTTP == nil || c1.Services().HTTP == c2.Services().HTTP {
		t.Fatal("New must not share one HTTP client")
	}
	j1.Append(journal.JournalEvent{Type: journal.EventNote, Component: "test"})
	if n := len(j2.Events()); n != 0 {
		t.Fatalf("c2 journal saw %d events", n)
	}
}

func TestConfigSnapshotIsolation(t *testing.T) {
	c, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	snap := c.Config()
	if snap.Settings.StickyFlags == nil {
		t.Fatal("sticky flags")
	}
	snap.Settings.StickyFlags["injected"] = true
	snap2 := c.Config()
	if _, ok := snap2.Settings.StickyFlags["injected"]; ok {
		t.Fatal("Config snapshot leaked into runtime")
	}
}

func TestNewSnapshotsInjectedConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Settings.StickyFlags["keep"] = true
	c, err := New(Options{Config: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cfg.Settings.StickyFlags["later"] = true
	snap := c.Config()
	if snap.Settings.StickyFlags["later"] {
		t.Fatal("injected config maps aliased")
	}
	if !snap.Settings.StickyFlags["keep"] {
		t.Fatal("lost keep")
	}
}

func TestClientZeusSearchAndPipeline(t *testing.T) {
	z := &fakeZeusHop{
		hop: ports.VerbHopResult{
			OK: true, StatusCode: 200,
			ReqID: "b2000000-0000-4000-8000-000000000002",
			Body:  map[string]any{"hits": []any{}, "total": 0},
		},
	}
	c, err := New(Options{Env: map[string]string{}, Zeus: z})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	result, err := c.Zeus().Search(context.Background(), map[string]any{"q": "fruit beer"}, api.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.ReqID != "b2000000-0000-4000-8000-000000000002" {
		t.Fatalf("%+v", result)
	}
	_, err = c.Zeus().Call(context.Background(), "pipeline", nil, api.CallOptions{})
	if err == nil {
		t.Fatal("pipeline")
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeZeusPipelineNotOnDirect {
		t.Fatalf("%v", err)
	}
	if z.n.Load() != 1 {
		t.Fatalf("pipeline must not CallVerb, n=%d", z.n.Load())
	}
}

func TestZeusAgentHandles(t *testing.T) {
	c, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	z := c.Zeus()
	a := c.Agent()
	cat := c.Catalog()
	sess := c.Session()
	dbg := c.Debug()
	units := c.Units()
	jobs := c.Jobs()
	if z == nil || a == nil || cat == nil || sess == nil || dbg == nil || units == nil || jobs == nil {
		t.Fatal("handles")
	}
	if z.Host() != c {
		t.Fatal("Zeus host")
	}
	if a.Host() != c {
		t.Fatal("Agent host")
	}
	if sess.Host() != c {
		t.Fatal("Session host")
	}
	if dbg.Host() != c {
		t.Fatal("Debug host")
	}
	if units.Host() != c {
		t.Fatal("Units host")
	}
	if jobs.Host() != c {
		t.Fatal("Jobs host")
	}
	var n *Client
	if n.Zeus() != nil || n.Agent() != nil || n.Catalog() != nil || n.Session() != nil || n.Debug() != nil || n.Units() != nil || n.Jobs() != nil {
		t.Fatal("nil Client handles")
	}
	if n.Config().Profile != "" {
		t.Fatal("nil Config")
	}
	if n.Journal() != nil {
		t.Fatal("nil Journal")
	}
}

func TestNewLoadMissingPath(t *testing.T) {
	_, err := New(Options{ConfigPath: "/no/such/zeus-client-config.json", Env: map[string]string{}})
	if err == nil {
		t.Fatal("expected error")
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeConfigPathNotFound {
		t.Fatalf("%v", err)
	}
}

func TestConfigCloseRace(t *testing.T) {
	c, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Config()
			_ = c.Services()
			_ = c.Zeus()
			_ = c.Agent()
			_ = c.Catalog()
			_ = c.Session()
			_ = c.Journal()
			if err := c.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestNewCloseThirtyTwo(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := New(isolatedOpts())
			if err != nil {
				t.Errorf("New: %v", err)
				return
			}
			_ = c.Config()
			if err := c.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()
}

type recordingJobsZeus struct {
	mu    sync.Mutex
	calls []ports.VerbRequest
}

func (r *recordingJobsZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recordingJobsZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return ports.VerbHopResult{OK: true, StatusCode: 200, ReqID: "req-" + req.Target.Bucket, Body: map[string]any{}}, nil
}

func clientDirectUnit(id, bucket, verb string) domain.UnitConfig {
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

func TestClientJobsUnavailableWithoutHost(t *testing.T) {
	c, err := New(isolatedOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Jobs().Run(context.Background(), "fan-out", api.JobsRunParams{
		Units: []domain.UnitConfig{clientDirectUnit("u1", "east", "find")},
	})
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeJobsUnavailable {
		t.Fatalf("got %v want 130001", err)
	}
}

func TestClientJobsInjectedFake(t *testing.T) {
	zeus := &recordingJobsZeus{}
	cfg := config.RuntimeConfig{
		Target:   config.DataTarget{Bucket: "west", Scope: "s", Collection: "c"},
		Zeus:     config.ZeusEndpointConfig{URL: "http://127.0.0.1:8080"},
		Settings: config.ClientSettings{Mode: "analytics"},
	}
	j := journal.NewInMemoryJournal(nil)
	units := api.NewUnitsAPIWith("host", api.UnitsOptions{
		Zeus:    zeus,
		Journal: j,
		Config:  cfg,
		Version: Version,
		Clock:   ports.SystemClock{},
	})
	fake := jobsfake.New(api.UnitHost{Units: units})
	c, err := New(Options{Config: &cfg, Zeus: zeus, Journal: j, Jobs: fake, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Services().Jobs != fake {
		t.Fatal("injected Jobs is not the one on services")
	}
	handle, err := c.Jobs().Run(context.Background(), "two scopes", api.JobsRunParams{
		Units: []domain.UnitConfig{
			clientDirectUnit("u1", "east", "find"),
			clientDirectUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := c.Jobs().Get(context.Background(), handle.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "ok" || snap.Partial {
		t.Fatalf("snap %+v", snap)
	}
	zeus.mu.Lock()
	defer zeus.mu.Unlock()
	if len(zeus.calls) != 2 || zeus.calls[0].Target.Bucket != "east" || zeus.calls[1].Target.Bucket != "north" {
		t.Fatalf("hops %+v", zeus.calls)
	}
}

func TestClientJobsPatternA(t *testing.T) {
	zeus := &recordingJobsZeus{}
	cfg := config.RuntimeConfig{
		Target:   config.DataTarget{Bucket: "west", Scope: "s", Collection: "c"},
		Zeus:     config.ZeusEndpointConfig{URL: "http://127.0.0.1:8080"},
		Settings: config.ClientSettings{Mode: "analytics"},
	}
	j := journal.NewInMemoryJournal(nil)
	units := api.NewUnitsAPIWith("host", api.UnitsOptions{
		Zeus:    zeus,
		Journal: j,
		Config:  cfg,
		Version: Version,
		Clock:   ports.SystemClock{},
	})
	rt := jobsma.New(api.UnitHost{Units: units})
	c, err := New(Options{Config: &cfg, Zeus: zeus, Journal: j, Jobs: rt, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	handle, err := c.Jobs().Run(context.Background(), "two scopes", api.JobsRunParams{
		Units: []domain.UnitConfig{
			clientDirectUnit("u1", "east", "find"),
			clientDirectUnit("u2", "north", "find"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	var snap domain.JobSnapshot
	for time.Now().Before(deadline) {
		snap, err = c.Jobs().Get(context.Background(), handle.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if snap.Status != "" && snap.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
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
