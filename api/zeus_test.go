// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/observability"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type recZeus struct {
	n    atomic.Int32
	reqs []ports.VerbRequest
	hop  ports.VerbHopResult
	err  error
}

func (r *recZeus) ResolveAuth(_ context.Context, _ config.DataTarget, _ bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.n.Add(1)
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return ports.VerbHopResult{}, r.err
	}
	if r.hop.StatusCode == 0 && r.hop.Body == nil && !r.hop.OK {
		return ports.VerbHopResult{OK: true, StatusCode: 200, ReqID: "r1", Body: map[string]any{"ok": true}}, nil
	}
	return r.hop, nil
}

var _ ports.ZeusPort = (*recZeus)(nil)

type agentProbe struct {
	called atomic.Bool
}

func (a *agentProbe) Host() any {
	a.called.Store(true)
	return nil
}

type wireDoc struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

func loadWire(t *testing.T, rel string) wireDoc {
	t.Helper()
	raw, err := os.ReadFile(findRepoFile(t, rel))
	if err != nil {
		t.Fatal(err)
	}
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSearchSuggestShortQueryDoesNotConsumeLimiter(t *testing.T) {
	z := &recZeus{}
	lim := observability.NewTokenBucketLimiter()
	lim.Configure("typeahead", 1, 1)
	cfg := config.Default()
	cfg.RateLimit = config.RateLimitPolicy{TypeaheadEnabled: true, TypeaheadRPS: 1, TypeaheadBurst: 1}
	api := NewZeusAPIWith("host", ZeusOptions{
		Zeus: z, Config: cfg, RateLimiter: lim,
	})
	got, err := api.SearchSuggest(context.Background(), "a", SuggestCallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 0 || z.n.Load() != 0 {
		t.Fatalf("short query must skip FTS: %+v hops=%d", got, z.n.Load())
	}
	if _, err := api.SearchSuggest(context.Background(), "ab", SuggestCallOptions{}); err != nil {
		t.Fatal(err)
	}
	if z.n.Load() != 1 {
		t.Fatalf("second call should hit FTS hops=%d", z.n.Load())
	}
}

func TestSearchSuggestRateLimited(t *testing.T) {
	z := &recZeus{
		hop: ports.VerbHopResult{
			OK: true, StatusCode: 200, ReqID: "fts-1",
			Body: map[string]any{"result": map[string]any{"items": []any{}}},
		},
	}
	lim := observability.NewTokenBucketLimiter()
	lim.Configure("typeahead", 1, 1)
	metrics := observability.NewInMemoryMetrics()
	cfg := config.Default()
	cfg.RateLimit = config.RateLimitPolicy{TypeaheadEnabled: true, TypeaheadRPS: 1, TypeaheadBurst: 1}
	api := NewZeusAPIWith("host", ZeusOptions{
		Zeus: z, Config: cfg, RateLimiter: lim, Metrics: metrics,
	})
	if _, err := api.SearchSuggest(context.Background(), "ab", SuggestCallOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := api.SearchSuggest(context.Background(), "cd", SuggestCallOptions{})
	requireCode(t, err, domain.CodeClientRateLimited)
	snap := metrics.Snapshot()
	counters, _ := snap["counters"].(map[string]any)
	if _, ok := counters["zeus_client_rate_limited_total"]; !ok {
		t.Fatalf("metrics %v", snap)
	}
}

func TestCallRecordsHopMetrics(t *testing.T) {
	z := &recZeus{}
	metrics := observability.NewInMemoryMetrics()
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: z, Config: config.Default(), Metrics: metrics})
	if _, err := api.Find(context.Background(), map[string]any{"entity_type": "Beer"}, CallOptions{}); err != nil {
		t.Fatal(err)
	}
	snap := metrics.Snapshot()
	counters, _ := snap["counters"].(map[string]any)
	if _, ok := counters["zeus_client_zeus_hops_total"]; !ok {
		t.Fatalf("metrics %v", snap)
	}
}

func TestCallAndSearchPipelineRejectedNoHTTP(t *testing.T) {
	z := &recZeus{}
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: z, Config: config.Default()})
	_, err := api.Call(context.Background(), "pipeline", map[string]any{}, CallOptions{})
	requireCode(t, err, domain.CodeZeusPipelineNotOnDirect)
	if z.n.Load() != 0 {
		t.Fatal("Call pipeline sent HTTP")
	}
	// Search is the search verb, not pipeline — still must not flip AllowPipeline.
	_, err = api.Search(context.Background(), map[string]any{"q": "x"}, CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(z.reqs) != 1 || z.reqs[0].Verb != "search" || z.reqs[0].AllowPipeline {
		t.Fatalf("%+v", z.reqs)
	}
}

func TestCallUnknownVerb(t *testing.T) {
	z := &recZeus{}
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: z})
	_, err := api.Call(context.Background(), "nope", nil, CallOptions{})
	requireCode(t, err, domain.CodeZeusVerbNotAllowed)
	if z.n.Load() != 0 {
		t.Fatal("HTTP")
	}
}

func TestFindGetProjectCall(t *testing.T) {
	z := &recZeus{}
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: z, Config: config.Default()})
	ctx := context.Background()
	if _, err := api.Find(ctx, map[string]any{"entity_type": "Beer"}, CallOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Get(ctx, map[string]any{"id": "1"}, CallOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Project(ctx, map[string]any{"fields": []any{"name"}}, CallOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Call(ctx, "explain", map[string]any{}, CallOptions{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"find", "get", "project", "explain"}
	if len(z.reqs) != 4 {
		t.Fatalf("n=%d", len(z.reqs))
	}
	for i, v := range want {
		if z.reqs[i].Verb != v || z.reqs[i].AllowPipeline {
			t.Fatalf("%d %+v", i, z.reqs[i])
		}
	}
}

func TestSearchSuggestUsesSearchDoesNotCallAgent(t *testing.T) {
	agent := &agentProbe{}
	z := &recZeus{
		hop: ports.VerbHopResult{
			OK: true, StatusCode: 200, ReqID: "fts-1",
			Body: map[string]any{
				"result": map[string]any{
					"items": []any{
						map[string]any{"node": map[string]any{"id": "x", "name": "X"}},
					},
				},
			},
		},
	}
	api := NewZeusAPIWith(agent, ZeusOptions{Zeus: z, Config: config.Default()})
	r, err := api.SearchSuggest(context.Background(), "ab", SuggestCallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if agent.called.Load() {
		t.Fatal("SearchSuggest must not touch Agent")
	}
	if r.FTSReqID != "fts-1" || r.Count < 1 {
		t.Fatalf("%+v", r)
	}
	if len(z.reqs) != 1 || z.reqs[0].Verb != "search" {
		t.Fatalf("%+v", z.reqs)
	}
	if z.reqs[0].Headers["X-Zeus-Trace-Class"] != zeushttp.TraceClassDirectInteractive {
		t.Fatalf("%v", z.reqs[0].Headers)
	}
}

func TestG3MockSearchOK(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "v2_search.response.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/search") {
			t.Errorf("path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if _, ok := body["rewind"]; ok {
			t.Error("rewind in body")
		}
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Zeus.URL = srv.URL
	cfg.Zeus.AuthMode = config.AuthNone
	cfg.Target = config.DataTarget{Bucket: "beer-sample", Scope: "_default", Collection: "_default"}
	port := zeushttp.NewPort(zeushttp.PortOptions{
		Endpoint: cfg.Zeus,
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	defer port.Close(context.Background())
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: port, Config: cfg, Version: "0.1.0-dev"})
	result, err := api.Search(context.Background(), map[string]any{
		"strategy": "fts", "q": "fruit beer", "limit": 8,
	}, CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.ReqID != "b2000000-0000-4000-8000-000000000002" {
		t.Fatalf("%+v", result)
	}
	hits, _ := result.Body["hits"].([]any)
	if len(hits) != 2 {
		t.Fatalf("hits %v", result.Body)
	}
}

func TestG3Mock409ReqIDNoForgedHash(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "errors", "contract_409.response.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Zeus.URL = srv.URL
	cfg.Zeus.AuthMode = config.AuthNone
	cfg.Target = config.DataTarget{Bucket: "beer-sample", Scope: "_default", Collection: "_default"}
	port := zeushttp.NewPort(zeushttp.PortOptions{
		Endpoint: cfg.Zeus,
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	defer port.Close(context.Background())
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: port, Config: cfg, Version: "0.1.0-dev"})
	result, err := api.Search(context.Background(), map[string]any{"strategy": "fts", "q": "x"}, CallOptions{})
	requireCode(t, err, domain.CodeZeusContractRequired)
	de, _ := domain.AsError(err)
	if result.ReqID != "40900000-0000-4000-8000-000000000409" {
		t.Fatalf("result req_id %q", result.ReqID)
	}
	if de.Details["req_id"] != result.ReqID {
		t.Fatalf("details %v", de.Details)
	}
	if de.Type != "zeus_http" {
		t.Fatalf("type %s", de.Type)
	}
	if de.Retryable {
		t.Fatal("retryable")
	}
	if _, ok := de.Details["contract_hash"]; ok {
		t.Fatal("must not forge hash")
	}
	raw, _ := json.Marshal(result.Body)
	if strings.Contains(string(raw), "TO_BE_FILLED") {
		t.Fatal("forged hash marker")
	}
}

func TestCallRewindQueryFromOptions(t *testing.T) {
	z := &recZeus{}
	api := NewZeusAPIWith("host", ZeusOptions{Zeus: z, Config: config.Default()})
	on := true
	_, err := api.Find(context.Background(), map[string]any{"entity_type": "Beer", "rewind": true}, CallOptions{Rewind: &on})
	if err != nil {
		t.Fatal(err)
	}
	if !z.reqs[0].Rewind {
		t.Fatal("rewind")
	}
	if _, ok := z.reqs[0].Body["rewind"]; ok {
		t.Fatal("rewind in body")
	}
}

func TestNewZeusAPINilHost(t *testing.T) {
	if NewZeusAPI(nil) != nil {
		t.Fatal("nil")
	}
	z := NewZeusAPIWith("h", ZeusOptions{})
	if z.Host() != "h" {
		t.Fatal("host")
	}
}

func TestSearchSuggestIsApplicationType(t *testing.T) {
	var _ application.SuggestResult
}
