// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func sessionClient(t *testing.T, httpPort ports.HttpPort, base string) *SessionClient {
	t.Helper()
	c := NewSessionClient(SessionClientOptions{
		Endpoint: config.ZeusEndpointConfig{URL: base, AuthMode: config.AuthNone, TimeoutS: 5, TLSVerify: true},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		HTTP:     httpPort,
		Version:  "0.1.0-dev",
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestHTTPCreateSessionWireShape(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "v2_session_create.response.json"))
	var sawBody map[string]any
	var sawURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path != "/v2/session" {
			t.Errorf("path %s", r.URL.Path)
		}
		sawURL = r.URL.String()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()
	c := sessionClient(t, nil, srv.URL)
	result, err := c.Create(context.Background(), SessionCreateRequest{
		ContractID:   "mock_analytics_b5",
		ContractHash: "mock:00000000000000000000000000000000",
		ChatRequest:  map[string]any{"_note": "catalog"},
		Mode:         "analytics",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.StatusCode != 201 {
		t.Fatalf("%+v", result)
	}
	if result.ReqID != "d4000000-0000-4000-8000-000000000004" {
		t.Fatalf("req_id %q", result.ReqID)
	}
	if result.Body["session_id"] != "zsess_mock_turn_001" {
		t.Fatalf("sid %v", result.Body["session_id"])
	}
	if _, ok := sawBody["session_id"]; ok {
		t.Fatal("client must not send session_id on create")
	}
	if sawBody["contract_id"] != "mock_analytics_b5" {
		t.Fatalf("body %v", sawBody)
	}
	if sawBody["user"] != ProductUser {
		t.Fatal("product stamp")
	}
	if strings.Contains(sawURL, "rewind=") {
		t.Fatal("rewind default off")
	}
}

func TestHTTPContinueTurnWireShape(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "v2_session_turn.response.json"))
	sid := "zsess_mock_turn_001"
	var sawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session/"+sid+"/turn" {
			t.Errorf("path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()
	c := sessionClient(t, nil, srv.URL)
	result, err := c.ContinueTurn(context.Background(), SessionContinueRequest{
		SessionID:   sid,
		ClientRound: 2,
		ChatRequest: map[string]any{},
		NewTurns:    []any{map[string]any{"role": "user", "content": "Find fruit beers"}},
		Mode:        "analytics",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.ReqID != "e5000000-0000-4000-8000-000000000005" {
		t.Fatalf("%+v", result)
	}
	if result.Body["session_id"] != sid {
		t.Fatalf("%v", result.Body)
	}
	if jsonIntTest(sawBody["round"]) != 2 {
		t.Fatalf("round %v", sawBody["round"])
	}
}

func jsonIntTest(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	default:
		return 0
	}
}

func TestHTTPCreate409CapturesReqIDDoesNotForgeHash(t *testing.T) {
	doc := loadWire(t, filepath.Join("testdata", "wire", "errors", "contract_409.response.json"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range doc.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(doc.Status)
		_, _ = w.Write(doc.Body)
	}))
	defer srv.Close()
	c := sessionClient(t, nil, srv.URL)
	result, err := c.Create(context.Background(), SessionCreateRequest{
		ContractID:   "c1",
		ContractHash: "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ChatRequest:  map[string]any{"messages": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.StatusCode != 409 {
		t.Fatalf("%+v", result)
	}
	if result.ReqID != "40900000-0000-4000-8000-000000000409" {
		t.Fatalf("req_id %q", result.ReqID)
	}
	if _, ok := result.Body["contract_hash"]; ok {
		t.Fatal("must not forge contract_hash")
	}
	if HopCode(result.StatusCode) != domain.CodeZeusContractRequired {
		t.Fatalf("code %s", HopCode(result.StatusCode))
	}
}

func TestContinueMissingSessionID(t *testing.T) {
	c := sessionClient(t, &recHTTP{}, "http://z")
	_, err := c.ContinueTurn(context.Background(), SessionContinueRequest{ClientRound: 2})
	mustCode(t, err, domain.CodeSessionIDMissing)
}

func TestRehydrateDeadReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	c := sessionClient(t, nil, srv.URL)
	body, err := c.Rehydrate(context.Background(), "stale-sid", 6, "analytics", nil, config.DataTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		t.Fatalf("%v", body)
	}
}

func TestCreateRewindQueryAndBody(t *testing.T) {
	h := &recHTTP{resp: ports.HTTPResponse{
		Status:  201,
		Headers: map[string]string{domain.ReqIDHeader: "r-rw"},
		Body:    []byte(`{"session_id":"sid-rw","round":1,"contract_status":"match"}`),
	}}
	c := sessionClient(t, h, "http://zeus.test:8080")
	_, err := c.Create(context.Background(), SessionCreateRequest{
		ContractID: "c1", ChatRequest: map[string]any{}, Rewind: true,
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
	if body["rewind"] != true {
		t.Fatalf("body rewind %v", body["rewind"])
	}
}
