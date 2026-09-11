// SPDX-License-Identifier: BUSL-1.1

package httpx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const (
	searchRID = "b2000000-0000-4000-8000-000000000002"
	errRID    = "40900000-0000-4000-8000-000000000409"
)

func searchJSON() []byte {
	return []byte(`{"hits":[{"id":"beer:fruit-01","score":1.2,"fields":{"name":"Fruit Stand Ale","type":"Beer"}}],"total":1}`)
}

func contract409JSON() []byte {
	return []byte(`{"error":"contract_required","message":"contract hash mismatch or missing","contract_status":"mismatch"}`)
}

func mockZeus(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/search"):
			if r.Header.Get(domain.ReqIDHeader) != "" {
				http.Error(w, "client must omit X-Zeus-Req-Id by default", http.StatusBadRequest)
				return
			}
			w.Header().Set(domain.ReqIDHeader, searchRID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(searchJSON())
		case strings.HasSuffix(r.URL.Path, "/find"):
			w.Header().Set(domain.ReqIDHeader, errRID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write(contract409JSON())
		case strings.HasSuffix(r.URL.Path, "/echo-req"):
			id := r.Header.Get(domain.ReqIDHeader)
			w.Header().Set(domain.ReqIDHeader, id)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSearchCapturesReqIDOn200(t *testing.T) {
	srv := mockZeus(t)
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	resp, err := c.Request(context.Background(), ports.HTTPRequest{
		Method: http.MethodPost,
		URL:    srv.URL + "/v2/yelp-data/_default/_default/search",
		Body:   []byte(`{"text":"fruit"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	env := Wrap(resp, err)
	if !env.OK {
		t.Fatalf("ok %v", env.Map())
	}
	if env.ReqID != searchRID {
		t.Fatalf("req_id %q", env.ReqID)
	}
	if env.Value["total"] != float64(1) {
		t.Fatalf("value %v", env.Value)
	}
	m := env.Map()
	if m["result"] != true || m["req_id"] != searchRID {
		t.Fatalf("%v", m)
	}
}

func TestFindCapturesReqIDOn409(t *testing.T) {
	srv := mockZeus(t)
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	resp, err := c.Request(context.Background(), ports.HTTPRequest{
		Method: http.MethodPost,
		URL:    srv.URL + "/v2/yelp-data/_default/_default/find",
		Body:   []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 409 {
		t.Fatalf("status %d", resp.Status)
	}
	env := Wrap(resp, err)
	if env.OK {
		t.Fatal("expected failure")
	}
	if env.ReqID != errRID {
		t.Fatalf("req_id %q", env.ReqID)
	}
	m := env.Map()
	if m["result"] != false {
		t.Fatal("result")
	}
	obj := m["error"].(map[string]any)
	if obj["code"] != string(domain.CodeZeusContractRequired) {
		t.Fatalf("code %v", obj["code"])
	}
	if obj["type"] != "zeus_http" {
		t.Fatalf("type %v", obj["type"])
	}
	if obj["retryable"] != false {
		t.Fatal("retryable")
	}
	if m["req_id"] != errRID {
		t.Fatalf("envelope req_id %v", m["req_id"])
	}
	if ShouldRetry(http.MethodPost, 409) {
		t.Fatal("must not retry 4xx")
	}
}

func TestPreMintSendsUUIDv4(t *testing.T) {
	srv := mockZeus(t)
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	resp, err := c.Request(context.Background(), ports.HTTPRequest{
		Method:       http.MethodPost,
		URL:          srv.URL + "/echo-req",
		PreMintReqID: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rid := domain.ReqIDFromHeaders(resp.Headers)
	if !domain.IsUUIDv4(rid) {
		t.Fatalf("pre-mint %q", rid)
	}
	env := Wrap(resp, err)
	if env.ReqID != rid {
		t.Fatalf("%q vs %q", env.ReqID, rid)
	}
}

func TestDefaultOmitsReqIDHeader(t *testing.T) {
	var saw atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(domain.ReqIDHeader) != "" {
			t.Errorf("sent %s", r.Header.Get(domain.ReqIDHeader))
		}
		saw.Store(true)
		w.Header().Set(domain.ReqIDHeader, searchRID)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	_, err := c.Request(context.Background(), ports.HTTPRequest{Method: http.MethodGet, URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !saw.Load() {
		t.Fatal("no request")
	}
}

func TestContextCancelAborts(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := c.Request(ctx, ports.HTTPRequest{Method: http.MethodGet, URL: srv.URL})
	if err == nil {
		t.Fatal("expected cancel")
	}
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("%v", err)
	}
	if de.Code != domain.CodeCancelled && de.Code != domain.CodeContextDeadline {
		t.Fatalf("code %s", de.Code)
	}
}

func TestCloseIdempotent(t *testing.T) {
	c := New(Options{})
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n *Client
	if err := n.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := c.Request(context.Background(), ports.HTTPRequest{Method: http.MethodGet, URL: "http://127.0.0.1:1/"})
	if err == nil {
		t.Fatal("closed client must not send")
	}
}

func TestTwoClientsAreNotGlobal(t *testing.T) {
	a := New(Options{})
	b := New(Options{})
	defer a.Close(context.Background())
	defer b.Close(context.Background())
	if a == b || a.hc == b.hc {
		t.Fatal("process-global HTTP")
	}
}

func TestConcurrentRequests(t *testing.T) {
	srv := mockZeus(t)
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Request(context.Background(), ports.HTTPRequest{
				Method: http.MethodPost,
				URL:    srv.URL + "/v2/yelp-data/_default/_default/search",
				Body:   []byte(`{}`),
			})
			if err != nil {
				t.Errorf("%v", err)
				return
			}
			env := Wrap(resp, err)
			if env.ReqID != searchRID {
				t.Errorf("req_id %q", env.ReqID)
			}
		}()
	}
	wg.Wait()
}

func TestPerRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := New(Options{Timeout: time.Second})
	defer c.Close(context.Background())
	_, err := c.Request(context.Background(), ports.HTTPRequest{
		Method:   http.MethodGet,
		URL:      srv.URL,
		TimeoutS: 0.05,
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestWrapTransportError(t *testing.T) {
	env := Wrap(ports.HTTPResponse{}, domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx"))
	if env.OK {
		t.Fatal("ok")
	}
	m := env.Map()
	obj := m["error"].(map[string]any)
	if obj["code"] != string(domain.CodeZeusTransport) {
		t.Fatalf("%v", obj)
	}
}

func TestJSONBodySent(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type %q", ct)
		}
		w.Header().Set(domain.ReqIDHeader, searchRID)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(Options{})
	defer c.Close(context.Background())
	body, _ := json.Marshal(map[string]any{"q": "fruit"})
	_, err := c.Request(context.Background(), ports.HTTPRequest{
		Method: http.MethodPost,
		URL:    srv.URL,
		Body:   body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "fruit") {
		t.Fatalf("%s", got)
	}
}
