// SPDX-License-Identifier: BUSL-1.1

package projectors

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
)

const (
	g3SearchDispatch = "b2000000-0000-4000-8000-000000000002"
	g42TraceEcho     = "f6000000-0000-4000-8000-000000000006"
)

func hopsOf(ms ...map[string]any) []any {
	out := make([]any, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

func projectorClient(t *testing.T, base string) *zeushttp.SessionClient {
	t.Helper()
	c := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: config.ZeusEndpointConfig{URL: base, AuthMode: config.AuthNone, TimeoutS: 5, TLSVerify: true},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := wd; ; d = filepath.Dir(d) {
		p := filepath.Join(d, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	t.Fatalf("missing %s from %s", rel, wd)
	return ""
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

func TestNormalizeLegacyTuple(t *testing.T) {
	hop := NormalizeHop([]any{"rid-1", "find", 200, "body", "http://z/find", 12})
	if hop["req_id"] != "rid-1" || hop["name"] != "find" || hop["status"] != 200 {
		t.Fatalf("%v", hop)
	}
	if hop["snippet"] != "body" || hop["url"] != "http://z/find" || hop["ms"] != 12 {
		t.Fatalf("%v", hop)
	}
}

func TestNormalizeDictAndSnipCap(t *testing.T) {
	long := strings.Repeat("x", TraceSnippetMax+50)
	hop := NormalizeHop(map[string]any{"req_id": "r", "name": "search", "status": 500, "snippet": long})
	if len(asString(hop["snippet"])) != TraceSnippetMax {
		t.Fatalf("snip len %d", len(asString(hop["snippet"])))
	}
}

func TestNormalizeHopsDedupesReqID(t *testing.T) {
	got := NormalizeHops([]any{
		[]any{"a", "find", 200, "", "u1"},
		[]any{"a", "find", 200, "", "u1"},
		[]any{"b", "search", 500, "err", "u2"},
		[]any{"", "x", 200, "", ""},
	})
	if len(got) != 2 || got[0]["req_id"] != "a" || got[1]["req_id"] != "b" {
		t.Fatalf("%v", got)
	}
}

func TestSelectPrimaryPrefersErrorOverOKPipeline(t *testing.T) {
	hops := hopsOf(
		map[string]any{"req_id": "p1", "name": "pipeline", "status": 200, "snippet": "ok", "result_size": 0},
		map[string]any{"req_id": "s1", "name": "search", "status": 500, "snippet": "timeout"},
	)
	if got := SelectPrimaryReqID(hops); got != "s1" {
		t.Fatalf("primary %q", got)
	}
}

func TestSelectPrimaryPrefersFindWithRowsOverEmptyPipeline(t *testing.T) {
	hops := hopsOf(
		map[string]any{"req_id": "p1", "name": "pipeline", "status": 200, "result_size": 0},
		map[string]any{"req_id": "f1", "name": "find", "status": 200, "result_size": 5},
	)
	if got := SelectPrimaryReqID(hops); got != "f1" {
		t.Fatalf("primary %q", got)
	}
}

func TestSelectPrimaryPrefersNonPipelineWhenTied(t *testing.T) {
	hops := hopsOf(
		map[string]any{"req_id": "p1", "name": "pipeline", "status": 200, "snippet": "x"},
		map[string]any{"req_id": "f1", "name": "find", "status": 200, "snippet": "y"},
	)
	if got := SelectPrimaryReqID(hops); got != "f1" {
		t.Fatalf("primary %q", got)
	}
}

func TestOrderedReqIDsPrimaryLast(t *testing.T) {
	hops := hopsOf(
		map[string]any{"req_id": "p1", "name": "pipeline", "status": 200},
		map[string]any{"req_id": "s1", "name": "search", "status": 500},
	)
	order := OrderedReqIDsForTracePosts(hops)
	if len(order) == 0 || order[len(order)-1] != "s1" {
		t.Fatalf("order %v", order)
	}
	seen := map[string]bool{}
	for _, id := range order {
		seen[id] = true
	}
	if !seen["p1"] || !seen["s1"] {
		t.Fatalf("order %v", order)
	}
}

func TestSelectPrimaryHopEmpty(t *testing.T) {
	if SelectPrimaryHop(nil) != nil || SelectPrimaryHop([]map[string]any{}) != nil {
		t.Fatal("empty hops must yield none")
	}
	if SelectPrimaryReqID(nil) != "" || SelectPrimaryReqID([]any{}) != "" {
		t.Fatal("empty req id")
	}
	if len(OrderedReqIDsForTracePosts(nil)) != 0 || len(OrderedReqIDsForTracePosts([]any{})) != 0 {
		t.Fatal("empty order")
	}
}

func TestBuildAggregateTracePayloadMultiHop(t *testing.T) {
	hops := hopsOf(
		map[string]any{
			"req_id":  "p1",
			"name":    "pipeline",
			"status":  200,
			"url":     "http://z/pipeline",
			"snippet": `{"meta":{}}`,
			"ms":      23,
			"step_costs": []any{
				map[string]any{"as": "tampa", "status": "ok", "result_size": 0, "ms": 22},
				map[string]any{"as": "salon_hits", "status": "ok", "result_size": 0, "ms": 2},
			},
		},
		map[string]any{
			"req_id":  "s1",
			"name":    "search",
			"status":  500,
			"url":     "http://z/search",
			"snippet": "search_timeout",
			"ms":      2001,
		},
	)
	agg := BuildAggregateTracePayload(hops, nil, nil, nil, nil, nil, nil)
	if agg.PrimaryReqID != "s1" || agg.Outcome != "error" {
		t.Fatalf("%+v", agg)
	}
	if len(agg.Turns) != 2 {
		t.Fatalf("turns %d", len(agg.Turns))
	}
	for _, turn := range agg.Turns {
		if turn["role"] != "tool" {
			t.Fatalf("role %v", turn)
		}
	}
	if !strings.Contains(agg.Turns[0]["content"], "pipeline → 200") {
		t.Fatalf("turn0 %q", agg.Turns[0]["content"])
	}
	if !strings.Contains(agg.Turns[0]["content"], "result_size=0") {
		t.Fatalf("turn0 %q", agg.Turns[0]["content"])
	}
	zresp := agg.ZeusResponse
	if zresp["aggregate"] != true || zresp["primary_req_id"] != "s1" || zresp["hop_count"] != 2 {
		t.Fatalf("%v", zresp)
	}
	ids, _ := zresp["req_ids"].([]string)
	if len(ids) != 2 || ids[0] != "p1" || ids[1] != "s1" {
		t.Fatalf("req_ids %v", zresp["req_ids"])
	}
	tools, _ := zresp["tool_hops"].([]map[string]any)
	if len(tools) != 2 {
		t.Fatalf("tool_hops %v", zresp["tool_hops"])
	}
	if zresp["status"] != 500 || zresp["url"] != "http://z/search" {
		t.Fatalf("%v", zresp)
	}
	if !strings.Contains(asString(zresp["snippet"]), "search_timeout") {
		t.Fatalf("snippet %v", zresp["snippet"])
	}
}

func TestBuildAggregateLegacyTuples(t *testing.T) {
	agg := BuildAggregateTracePayload([]any{[]any{"r1", "find", 200, "rows", "http://z/find"}}, nil, nil, nil, nil, nil, nil)
	if agg.PrimaryReqID != "r1" || agg.Outcome != "ok" {
		t.Fatalf("%+v", agg)
	}
	ids, _ := agg.ZeusResponse["req_ids"].([]string)
	if len(ids) != 1 || ids[0] != "r1" {
		t.Fatalf("req_ids %v", agg.ZeusResponse["req_ids"])
	}
	if !strings.HasPrefix(agg.Turns[0]["content"], "find → 200") {
		t.Fatalf("turn %q", agg.Turns[0]["content"])
	}
}

func TestExtractPipelineMetaNestedData(t *testing.T) {
	body := map[string]any{
		"status": "ok",
		"data": map[string]any{
			"meta": map[string]any{
				"elapsed_ms":     22,
				"steps_executed": 5,
				"step_costs": []any{
					map[string]any{"as": "a", "status": "ok", "result_size": 0},
					map[string]any{"as": "b", "status": "ok", "result_size": 3},
				},
			},
		},
	}
	meta := ExtractPipelineMeta(body)
	if meta["steps_executed"] != 5 {
		t.Fatalf("%v", meta)
	}
	if meta["result_size"] != 3 {
		t.Fatalf("result_size %v", meta["result_size"])
	}
	costs, _ := asSlice(meta["step_costs"])
	if len(costs) != 2 {
		t.Fatalf("costs %v", meta["step_costs"])
	}
	if meta["pipeline_status"] != "ok" {
		t.Fatalf("pipeline_status %v", meta["pipeline_status"])
	}
}

func TestExtractFindResultSize(t *testing.T) {
	meta := ExtractPipelineMeta(map[string]any{"result": map[string]any{"returned_count": 7, "items": []any{1, 2}}})
	if meta["result_size"] != 7 {
		t.Fatalf("%v", meta)
	}
}

func TestBuildAggregateEmpty(t *testing.T) {
	agg := BuildAggregateTracePayload(nil, nil, nil, nil, nil, nil, nil)
	if agg.PrimaryReqID != "" || agg.Outcome != "ok" || len(agg.Turns) != 0 || len(agg.ZeusResponse) != 0 {
		t.Fatalf("%+v", agg)
	}
	agg2 := BuildAggregateTracePayload(nil, map[string]any{"intent": "List", "confidence": "high"}, nil, nil, nil, nil, nil)
	if agg2.PrimaryReqID != "" || agg2.Outcome != "ok" || len(agg2.Turns) != 0 {
		t.Fatalf("%+v", agg2)
	}
	la, _ := agg2.ZeusResponse["layer_a"].(map[string]any)
	if la["intent"] != "List" {
		t.Fatalf("%v", agg2.ZeusResponse)
	}
}

func TestBuildAggregateIncludesCatalogTerminateTokensLayerA(t *testing.T) {
	hops := hopsOf(map[string]any{"req_id": "r1", "name": "find", "status": 200, "snippet": "ok"})
	cat := map[string]any{"tool_count": 2, "tool_names": []any{"find", "return"}, "has_return_verb": true, "has_pipeline_verb": false}
	term := map[string]any{"has_terminate": true, "terminate_via": "return", "ai_process_result": false, "cheap_final": false}
	agg := BuildAggregateTracePayload(hops, nil, nil, nil, cat, term, nil)
	c, _ := agg.ZeusResponse["catalog"].(map[string]any)
	if c["tool_count"] != 2 {
		t.Fatalf("catalog %v", agg.ZeusResponse["catalog"])
	}
	tm, _ := agg.ZeusResponse["terminate"].(map[string]any)
	if tm["terminate_via"] != "return" {
		t.Fatalf("terminate %v", agg.ZeusResponse["terminate"])
	}
	bare := BuildAggregateTracePayload(hops, nil, nil, nil, nil, nil, nil)
	if _, ok := bare.ZeusResponse["catalog"]; ok {
		t.Fatal("catalog present")
	}
	bag := map[string]any{"prompt": 30, "completion": 3, "total": 33, "rounds": 2, "ok": true}
	withTok := BuildAggregateTracePayload(hops, nil, nil, bag, nil, nil, nil)
	tok, _ := withTok.ZeusResponse["tokens"].(map[string]any)
	if tok["prompt"] != 30 || tok["rounds"] != 2 {
		t.Fatalf("tokens %v", tok)
	}
	empty := BuildAggregateTracePayload(nil, nil, nil, bag, nil, nil, nil)
	et, _ := empty.ZeusResponse["tokens"].(map[string]any)
	if et["prompt"] != 30 {
		t.Fatalf("%v", empty.ZeusResponse)
	}
	la := map[string]any{
		"intent":              "List",
		"query_decomposition": map[string]any{"intent": "List", "entity": "Business"},
		"decomposition":       map[string]any{"targets": []any{"Business"}},
		"confidence":          "high",
		"summary":             "found 3",
	}
	pipe := hopsOf(map[string]any{"req_id": "r1", "name": "pipeline", "status": 200, "snippet": "ok"})
	withLA := BuildAggregateTracePayload(pipe, la, nil, nil, nil, nil, nil)
	gotLA, _ := withLA.ZeusResponse["layer_a"].(map[string]any)
	qd, _ := gotLA["query_decomposition"].(map[string]any)
	if gotLA["intent"] != "List" || qd["entity"] != "Business" {
		t.Fatalf("%v", gotLA)
	}
}

func TestBuildAggregateIncludesProductStamp(t *testing.T) {
	agg := BuildAggregateTracePayload(
		hopsOf(map[string]any{"req_id": "r1", "name": "find", "status": 200}),
		nil, nil, nil, nil, nil,
		map[string]any{"user": "zeus_client", "version": "2.1.0"},
	)
	if agg.ZeusResponse["user"] != "zeus_client" || agg.ZeusResponse["version"] != "2.1.0" {
		t.Fatalf("%v", agg.ZeusResponse)
	}
}

func TestProjectSessionTracePostsIdenticalBodyPrimaryLast(t *testing.T) {
	var posts []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session/trace" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("json: %v", err)
		}
		posts = append(posts, body)
		w.Header().Set(domain.ReqIDHeader, "trace-post")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"contract_status":"match"}`))
	}))
	defer srv.Close()
	cli := projectorClient(t, srv.URL)
	handle := domain.SessionHandle{
		SessionID: "sid-1", Round: 2, ChatID: "c",
		ContractID: "cid", ContractHash: "md5:h", ContractStatus: "match", Enabled: true,
	}
	hops := hopsOf(
		map[string]any{"req_id": "p1", "name": "pipeline", "status": 200, "snippet": "ok"},
		map[string]any{"req_id": "s1", "name": "search", "status": 500, "snippet": "boom"},
	)
	result := ProjectSessionTrace(context.Background(), cli, ProjectOptions{
		Handle:      handle,
		Hops:        hops,
		ChatRequest: map[string]any{"messages": []any{}},
		Inject:      map[string]any{"has_scope_brief": true, "brief_sha12": "slicehash12"},
		Mode:        "analytics",
		TurnID:      "turn-agg-1",
	})
	if !result.OK {
		t.Fatalf("%+v", result)
	}
	if result.PrimaryReqID != "s1" || len(result.PostOrder) != 2 || result.PostOrder[0] != "p1" || result.PostOrder[1] != "s1" {
		t.Fatalf("%+v", result)
	}
	if len(posts) != 2 {
		t.Fatalf("posts %d", len(posts))
	}
	if posts[0]["req_id"] != "p1" || posts[1]["req_id"] != "s1" {
		t.Fatalf("join keys %v %v", posts[0]["req_id"], posts[1]["req_id"])
	}
	for _, p := range posts {
		if p["session_id"] != "sid-1" || p["turn_id"] != "turn-agg-1" {
			t.Fatalf("%v", p)
		}
		if int(p["round"].(float64)) != 2 {
			t.Fatalf("round %v", p["round"])
		}
		z, _ := p["zeus_response"].(map[string]any)
		if z["aggregate"] != true || z["primary_req_id"] != "s1" {
			t.Fatalf("zresp %v", z)
		}
		ids, _ := z["req_ids"].([]any)
		if len(ids) != 2 || ids[0] != "p1" || ids[1] != "s1" {
			t.Fatalf("req_ids %v", z["req_ids"])
		}
		inj, _ := z["inject"].(map[string]any)
		if inj["brief_sha12"] != "slicehash12" {
			t.Fatalf("inject %v", inj)
		}
		if p["outcome"] != "error" {
			t.Fatalf("outcome %v", p["outcome"])
		}
		turns, _ := p["turns"].([]any)
		if len(turns) != 2 {
			t.Fatalf("turns %v", p["turns"])
		}
	}
	b0, _ := json.Marshal(stripVolatileStamp(posts[0]))
	b1, _ := json.Marshal(stripVolatileStamp(posts[1]))
	if string(b0) != string(b1) {
		t.Fatalf("bodies differ:\n%s\n%s", b0, b1)
	}
}

func TestProjectSessionTraceSoftFailOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("fail"))
	}))
	defer srv.Close()
	cli := projectorClient(t, srv.URL)
	j := journal.NewInMemoryJournal(nil)
	result := ProjectSessionTrace(context.Background(), cli, ProjectOptions{
		Handle:  domain.SessionHandle{SessionID: "sid-1", Round: 1, Enabled: true, ContractID: "c", ContractHash: "h"},
		Hops:    hopsOf(map[string]any{"req_id": "r1", "name": "find", "status": 200}),
		Journal: j,
	})
	if result.OK {
		t.Fatal("expected soft-fail")
	}
	if len(result.Errors) == 0 {
		t.Fatal("errors")
	}
	if result.PrimaryReqID != "r1" {
		t.Fatalf("primary %q", result.PrimaryReqID)
	}
	evs := j.Events()
	if len(evs) != 1 || evs[0].Type != journal.EventNote {
		t.Fatalf("journal %+v", evs)
	}
	if evs[0].Data["kind"] != "projector.session_trace" || evs[0].Data["ok"] != false {
		t.Fatalf("note %v", evs[0].Data)
	}
	if asString(evs[0].Data["session.id"]) != "sid-1" {
		t.Fatalf("session %v", evs[0].Data)
	}
}

func TestProjectSessionTraceNoopWhenDisabledOrNoHops(t *testing.T) {
	cli := projectorClient(t, "http://zeus.test:8080")
	r1 := ProjectSessionTrace(context.Background(), cli, ProjectOptions{
		Handle: domain.SessionHandle{SessionID: "s", Round: 1, Enabled: false},
		Hops:   hopsOf(map[string]any{"req_id": "a"}),
	})
	if !r1.OK || r1.Posts != 0 {
		t.Fatalf("%+v", r1)
	}
	r2 := ProjectSessionTrace(context.Background(), cli, ProjectOptions{
		Handle: domain.SessionHandle{SessionID: "s", Round: 1, Enabled: true},
		Hops:   []any{},
	})
	if !r2.OK || r2.Posts != 0 {
		t.Fatalf("%+v", r2)
	}
}

func TestG42TraceJoinsDispatchReqIDFromWire(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "v2_session_trace.response.json"))
	var sawBody map[string]any
	var sawHeader http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session/trace" {
			t.Errorf("path %s", r.URL.Path)
		}
		sawHeader = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()
	cli := projectorClient(t, srv.URL)
	result := ProjectSessionTrace(context.Background(), cli, ProjectOptions{
		Handle: domain.SessionHandle{
			SessionID:    "zsess_mock_turn_001",
			Round:        2,
			Enabled:      true,
			ContractID:   "mock_analytics_b5",
			ContractHash: "mock:00000000000000000000000000000000",
		},
		Hops: hopsOf(map[string]any{
			"req_id": g3SearchDispatch, "name": "search", "status": 200, "snippet": "ok",
		}),
	})
	if !result.OK {
		t.Fatalf("%+v", result)
	}
	if sawBody["req_id"] != g3SearchDispatch {
		t.Fatalf("join key %v (must be G3 dispatch, not a minted id)", sawBody["req_id"])
	}
	if sawBody["req_id"] == g42TraceEcho {
		t.Fatal("must not send the trace hop echo as body.req_id")
	}
	if sawHeader.Get(domain.ReqIDHeader) != "" {
		t.Fatalf("must not mint %s on trace request, got %q", domain.ReqIDHeader, sawHeader.Get(domain.ReqIDHeader))
	}
	if cli.LastReqID() != g42TraceEcho {
		t.Fatalf("capture echo %q", cli.LastReqID())
	}
	if result.PrimaryReqID != g3SearchDispatch {
		t.Fatalf("primary %q", result.PrimaryReqID)
	}
}

func stripVolatileStamp(m map[string]any) map[string]any {
	out := copyMap(m)
	delete(out, "req_id")
	delete(out, "ts")
	if z, ok := out["zeus_response"].(map[string]any); ok {
		zc := copyMap(z)
		delete(zc, "ts")
		out["zeus_response"] = zc
	}
	return out
}
