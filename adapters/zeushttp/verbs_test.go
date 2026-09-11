// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
