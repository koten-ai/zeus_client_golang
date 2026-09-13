// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type recHTTP struct {
	mu   sync.Mutex
	n    int
	last ports.HTTPRequest
	resp ports.HTTPResponse
	err  error
}

func (r *recHTTP) Request(_ context.Context, req ports.HTTPRequest) (ports.HTTPResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	r.last = req
	return r.resp, r.err
}

func (r *recHTTP) Close(context.Context) error { return nil }

func (r *recHTTP) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

type seqHTTP struct {
	mu    sync.Mutex
	n     int
	steps []struct {
		resp ports.HTTPResponse
		err  error
	}
}

func (s *seqHTTP) Request(_ context.Context, _ ports.HTTPRequest) (ports.HTTPResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.n
	s.n++
	if len(s.steps) == 0 {
		return ports.HTTPResponse{}, errors.New("seqHTTP empty")
	}
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	return s.steps[i].resp, s.steps[i].err
}

func (s *seqHTTP) Close(context.Context) error { return nil }

func (s *seqHTTP) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func retryPort(t *testing.T, httpPort ports.HttpPort, base string) *Port {
	t.Helper()
	p := NewPort(PortOptions{
		Endpoint: config.ZeusEndpointConfig{URL: base, AuthMode: config.AuthNone, TimeoutS: 5, TLSVerify: true},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		HTTP:     httpPort,
		Journal:  journal.NewInMemoryJournal(nil),
		TurnID:   "turn_t",
		Version:  "0.1.0-dev",
		Retry:    config.RetryPolicy{MaxAttempts: 3, BaseDelayMS: 0, MaxDelayMS: 1, Jitter: false},
		Sleep:    func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p
}

func yelpTarget() config.DataTarget {
	return config.DataTarget{Bucket: "yelp-data", Scope: "_default", Collection: "_default"}
}

func beerTargetFull() config.DataTarget {
	return config.DataTarget{Bucket: "beer-sample", Scope: "_default", Collection: "_default"}
}

func nonePort(t *testing.T, httpPort ports.HttpPort, base string) *Port {
	t.Helper()
	p := NewPort(PortOptions{
		Endpoint: config.ZeusEndpointConfig{URL: base, AuthMode: config.AuthNone, TimeoutS: 5, TLSVerify: true},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		HTTP:     httpPort,
		Journal:  journal.NewInMemoryJournal(nil),
		TurnID:   "turn_t",
		Version:  "0.1.0-dev",
	})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p
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

func TestExposedVerbsExcludePipeline(t *testing.T) {
	if IsExposedV2Verb("pipeline") {
		t.Fatal("pipeline")
	}
	if !IsExposedV2Verb("find") || !IsExposedV2Verb("search") {
		t.Fatal("find/search")
	}
	for _, v := range ExposedV2Verbs {
		if v == "pipeline" {
			t.Fatal("listed")
		}
	}
}

func TestVerbURLShapes(t *testing.T) {
	tgt := yelpTarget()
	if got := VerbURL("http://z:8080", tgt, "find"); !strings.HasSuffix(got, "/v2/yelp-data/_default/_default/find") {
		t.Fatalf("find %s", got)
	}
	if got := VerbURL("http://z:8080", tgt, "explain"); !strings.HasSuffix(got, "/v2/explain") {
		t.Fatalf("explain %s", got)
	}
	if got := VerbURL("http://z:8080", tgt, "describe"); !strings.HasSuffix(got, "/v2/yelp-data/_default/describe") {
		t.Fatalf("describe %s", got)
	}
}

func TestCallVerbPipelineRejectedNoHTTP(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{Status: 200}}
	p := nonePort(t, h, "http://z")
	_, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "pipeline", Target: yelpTarget()})
	mustCode(t, err, domain.CodeZeusPipelineNotOnDirect)
	if h.count() != 0 {
		t.Fatal("HTTP sent")
	}
}

func TestCallVerbUnknownNotAllowed(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{Status: 200}}
	p := nonePort(t, h, "http://z")
	_, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "nope", Target: yelpTarget()})
	mustCode(t, err, domain.CodeZeusVerbNotAllowed)
	if h.count() != 0 {
		t.Fatal("HTTP sent")
	}
}

func TestAllowPipelineDefaultFalseOnZeroRequest(t *testing.T) {
	var req ports.VerbRequest
	if req.AllowPipeline {
		t.Fatal("default")
	}
}

func TestCallVerbAllowPipelineTruePosts(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{
		Status:  200,
		Headers: map[string]string{domain.ReqIDHeader: "pipe-1"},
		Body:    []byte(`{"ok":true}`),
	}}
	p := nonePort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:          "pipeline",
		AllowPipeline: true,
		Target:        yelpTarget(),
		Body:          map[string]any{"steps": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatal("expected HTTP")
	}
	if !strings.HasSuffix(h.last.URL, "/v2/yelp-data/_default/_default/pipeline") {
		t.Fatalf("url %s", h.last.URL)
	}
	if !hop.OK || hop.ReqID != "pipe-1" {
		t.Fatalf("%+v", hop)
	}
}

func TestCallVerbRewindIsQueryNotBody(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{
		Status:  200,
		Headers: map[string]string{domain.ReqIDHeader: "r-rw"},
		Body:    []byte(`{"ok":true}`),
	}}
	p := nonePort(t, h, "http://zeus.test:8080")
	_, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:   "find",
		Target: yelpTarget(),
		Body:   map[string]any{"entity_type": "Business", "rewind": true},
		Rewind: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.last.URL, "rewind=true") {
		t.Fatalf("url %s", h.last.URL)
	}
	var body map[string]any
	if err := json.Unmarshal(h.last.Body, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["rewind"]; ok {
		t.Fatal("rewind in JSON")
	}
	if body["entity_type"] != "Business" {
		t.Fatalf("%v", body)
	}
}

func TestCallVerbDefaultOmitsRewindQuery(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{Status: 200, Body: []byte(`{"ok":true}`)}}
	p := nonePort(t, h, "http://zeus.test:8080")
	_, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:   "find",
		Target: yelpTarget(),
		Body:   map[string]any{"entity_type": "Business"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.last.URL, "rewind=") {
		t.Fatalf("url %s", h.last.URL)
	}
	var body map[string]any
	if err := json.Unmarshal(h.last.Body, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["rewind"]; ok {
		t.Fatal("rewind in JSON")
	}
}

func TestMockSearch200CapturesReqID(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "v2_search.response.json"))
	var sawURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawURL = r.URL.String()
		_, _ = io.ReadAll(r.Body)
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()
	p := nonePort(t, nil, srv.URL)
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:       "search",
		Target:     beerTargetFull(),
		ModeHeader: "analytics",
		Body:       map[string]any{"strategy": "fts", "q": "fruit beer", "limit": 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hop.OK || hop.StatusCode != 200 {
		t.Fatalf("%+v", hop)
	}
	if hop.ReqID != "b2000000-0000-4000-8000-000000000002" {
		t.Fatalf("req_id %q", hop.ReqID)
	}
	if !strings.HasSuffix(strings.Split(sawURL, "?")[0], "/v2/beer-sample/_default/_default/search") {
		t.Fatalf("path %s", sawURL)
	}
	blob := p.Logs()
	if !strings.Contains(blob, "verb=search") || !strings.Contains(blob, "b2000000-0000-4000-8000-000000000002") {
		t.Fatalf("logs %s", blob)
	}
	j := p.journal.Events()
	if len(j) != 1 || j[0].Type != journal.EventZeusHop {
		t.Fatalf("journal %+v", j)
	}
	if j[0].Data["req_id"] != hop.ReqID {
		t.Fatalf("journal req_id %v", j[0].Data["req_id"])
	}
}

func TestMock409CapturesReqIDDoesNotForgeHash(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "errors", "contract_409.response.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()
	p := nonePort(t, nil, srv.URL)
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:   "search",
		Target: beerTargetFull(),
		Body:   map[string]any{"strategy": "fts", "q": "fruit beer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hop.OK || hop.StatusCode != 409 {
		t.Fatalf("%+v", hop)
	}
	if hop.ReqID != "40900000-0000-4000-8000-000000000409" {
		t.Fatalf("req_id %q", hop.ReqID)
	}
	raw, _ := json.Marshal(hop.Body)
	if strings.Contains(string(raw), "TO_BE_FILLED") {
		t.Fatal("forged hash marker")
	}
	if _, ok := hop.Body["contract_hash"]; ok {
		t.Fatal("must not forge contract_hash")
	}
	if HopCode(hop.StatusCode) != domain.CodeZeusContractRequired {
		t.Fatalf("code %s", HopCode(hop.StatusCode))
	}
}

func TestCallVerbStampsDirectHeadersAndProduct(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{
		Status:  200,
		Headers: map[string]string{domain.ReqIDHeader: "r-dr"},
		Body:    []byte(`{"ok":true}`),
	}}
	p := nonePort(t, h, "http://zeus.test:8080")
	_, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:       "find",
		Target:     yelpTarget(),
		ModeHeader: "analytics",
		Headers:    map[string]string{"X-Zeus-Trace-Class": TraceClassDirectRead, "X-Zeus-Chat-Id": "chat-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	hdr := h.last.Headers
	if hdr["X-Zeus-Mode"] != "analytics" {
		t.Fatalf("%v", hdr)
	}
	if hdr["X-Zeus-Client"] != ProductUser {
		t.Fatal("product stamp")
	}
	if hdr["X-Zeus-Scope"] != "yelp-data/_default" {
		t.Fatalf("scope %s", hdr["X-Zeus-Scope"])
	}
	if _, ok := hdr[domain.ReqIDHeader]; ok {
		t.Fatal("must omit req id (Zeus mints)")
	}
	if strings.Contains(h.last.URL, "rewind=") {
		t.Fatal("rewind")
	}
}

func TestCallVerbStampUserHubAdmin(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{
		Status:  200,
		Headers: map[string]string{domain.ReqIDHeader: "r-admin"},
		Body:    []byte(`{"ok":true}`),
	}}
	p := NewPort(PortOptions{
		Endpoint:  config.ZeusEndpointConfig{URL: "http://zeus.test:8080", AuthMode: config.AuthNone, TimeoutS: 5, TLSVerify: true},
		Secrets:   secretsenv.NewWithEnviron(map[string]string{}),
		HTTP:      h,
		Journal:   journal.NewInMemoryJournal(nil),
		TurnID:    "turn_t",
		Version:   "0.1.0-dev",
		StampUser: domain.HubUser,
	})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	_, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "find", Target: yelpTarget()})
	if err != nil {
		t.Fatal(err)
	}
	if h.last.Headers["X-Zeus-Client"] != domain.HubUser {
		t.Fatalf("%v", h.last.Headers)
	}
}

func TestZeusReqLogsBytesInOutAndReqID(t *testing.T) {
	body := []byte(`{"ok":true,"n":1}`)
	h := &recHTTP{resp: ports.HTTPResponse{
		Status:  200,
		Headers: map[string]string{domain.ReqIDHeader: "req-bytes-1"},
		Body:    body,
	}}
	p := nonePort(t, h, "http://zeus.test:8080")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:   "find",
		Target: yelpTarget(),
		Body:   map[string]any{"entity_type": "Beer", "limit": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hop.HasBytes || hop.BytesIn != len(body) || hop.BytesOut <= 0 {
		t.Fatalf("bytes in=%d out=%d has=%v", hop.BytesIn, hop.BytesOut, hop.HasBytes)
	}
	logs := p.Logs()
	if !strings.Contains(logs, "zeus_client.zeus.req") {
		t.Fatalf("logs %s", logs)
	}
	if !strings.Contains(logs, "req_id=req-bytes-1") || !strings.Contains(logs, "duration_ms=") {
		t.Fatalf("req/duration %s", logs)
	}
	if !strings.Contains(logs, "bytes.in=") || !strings.Contains(logs, "bytes.out=") {
		t.Fatalf("bytes %s", logs)
	}
	if strings.Contains(logs, "zeus_client.hop.telemetry") {
		t.Fatal("invented hop.telemetry")
	}
	if strings.Contains(logs, "tokens.input=0") {
		t.Fatal("tokens.input=0 on Direct HTTP")
	}
}

func TestCallVerbFindRetries503ThenOK(t *testing.T) {
	h := &seqHTTP{steps: []struct {
		resp ports.HTTPResponse
		err  error
	}{
		{resp: ports.HTTPResponse{Status: 503, Body: []byte(`{"error":"busy"}`)}},
		{resp: ports.HTTPResponse{Status: 200, Headers: map[string]string{domain.ReqIDHeader: "r-ok"}, Body: []byte(`{"ok":true}`)}},
	}}
	p := retryPort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "find", Target: yelpTarget()})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 {
		t.Fatalf("calls=%d want 2", h.count())
	}
	if !hop.OK || hop.ReqID != "r-ok" {
		t.Fatalf("%+v", hop)
	}
}

func TestCallVerbFindNoRetry409(t *testing.T) {
	h := &seqHTTP{steps: []struct {
		resp ports.HTTPResponse
		err  error
	}{
		{resp: ports.HTTPResponse{Status: 409, Headers: map[string]string{domain.ReqIDHeader: "r-409"}, Body: []byte(`{"error":"contract"}`)}},
	}}
	p := retryPort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "find", Target: yelpTarget()})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatalf("calls=%d", h.count())
	}
	if hop.OK || hop.StatusCode != 409 {
		t.Fatalf("%+v", hop)
	}
}

func TestCallVerbFindNoRetry429(t *testing.T) {
	h := &seqHTTP{steps: []struct {
		resp ports.HTTPResponse
		err  error
	}{
		{resp: ports.HTTPResponse{Status: 429, Body: []byte(`{"error":"slow"}`)}},
	}}
	p := retryPort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "find", Target: yelpTarget()})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || hop.StatusCode != 429 {
		t.Fatalf("calls=%d hop=%+v", h.count(), hop)
	}
}

func TestCallVerbFindRetriesTransportThenOK(t *testing.T) {
	h := &seqHTTP{steps: []struct {
		resp ports.HTTPResponse
		err  error
	}{
		{err: errors.New("connection reset")},
		{resp: ports.HTTPResponse{Status: 200, Headers: map[string]string{domain.ReqIDHeader: "r-t"}, Body: []byte(`{"ok":true}`)}},
	}}
	p := retryPort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{Verb: "find", Target: yelpTarget()})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || !hop.OK {
		t.Fatalf("calls=%d hop=%+v err=%v", h.count(), hop, err)
	}
}

func TestCallVerbSetNoRetry503(t *testing.T) {
	h := &seqHTTP{steps: []struct {
		resp ports.HTTPResponse
		err  error
	}{
		{resp: ports.HTTPResponse{Status: 503, Body: []byte(`{"error":"busy"}`)}},
	}}
	p := retryPort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:   "set",
		Target: yelpTarget(),
		Body:   map[string]any{"doc_key": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || hop.StatusCode != 503 {
		t.Fatalf("calls=%d hop=%+v", h.count(), hop)
	}
}

func TestCallVerbPipelineAllowNoRetry503(t *testing.T) {
	h := &seqHTTP{steps: []struct {
		resp ports.HTTPResponse
		err  error
	}{
		{resp: ports.HTTPResponse{Status: 503, Body: []byte(`{"error":"busy"}`)}},
	}}
	p := retryPort(t, h, "http://z")
	hop, err := p.CallVerb(context.Background(), ports.VerbRequest{
		Verb:          "pipeline",
		AllowPipeline: true,
		Target:        yelpTarget(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || hop.StatusCode != 503 {
		t.Fatalf("calls=%d hop=%+v", h.count(), hop)
	}
}
