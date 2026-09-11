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
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/application/projectors"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

func TestG4MockCreateServerShapedID(t *testing.T) {
	raw, err := os.ReadFile(findRepoFile(t, filepath.Join("testdata", "wire", "v2_session_create.response.json")))
	if err != nil {
		t.Fatal(err)
	}
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session" {
			t.Errorf("path %s", r.URL.Path)
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
	cfg.Settings.DurableSessions = true
	cli := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: cfg.Zeus,
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	defer cli.Close(context.Background())
	api := NewSessionAPIWith("host", SessionOptions{Client: cli, Config: cfg, Version: "0.1.0-dev"})
	handle, err := api.Create(context.Background(), application.SetupOptions{
		ChatRequest: map[string]any{"_note": "catalog"},
		ContractID:  "mock_analytics_b5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.SessionID != "zsess_mock_turn_001" {
		t.Fatalf("server-shaped id %q", handle.SessionID)
	}
	if handle.Round != 1 || !handle.Created {
		t.Fatalf("%+v", handle)
	}
	if handle.CreateReqID != "d4000000-0000-4000-8000-000000000004" {
		t.Fatalf("req_id %q", handle.CreateReqID)
	}
	if handle.ContractStatus != "match" {
		t.Fatalf("status %s", handle.ContractStatus)
	}
}

func TestG4ContinuePriorIDAndRound(t *testing.T) {
	raw, err := os.ReadFile(findRepoFile(t, filepath.Join("testdata", "wire", "v2_session_turn.response.json")))
	if err != nil {
		t.Fatal(err)
	}
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sid := "zsess_mock_turn_001"
	var sawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session/"+sid+"/turn" {
			t.Errorf("path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sawBody)
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
	cli := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: cfg.Zeus,
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	defer cli.Close(context.Background())
	api := NewSessionAPIWith("host", SessionOptions{Client: cli, Config: cfg})
	handle := domain.SessionHandle{SessionID: sid, Round: 2, Enabled: true, Created: false}
	out, err := api.Continue(context.Background(), handle, application.CommitOptions{
		ChatRequest:   map[string]any{},
		ProducedDelta: []any{map[string]any{"role": "user", "content": "Find fruit beers"}},
	})
	if err != nil || !out.OK {
		t.Fatalf("%+v %v", out, err)
	}
	if out.TurnReqID != "e5000000-0000-4000-8000-000000000005" {
		t.Fatalf("req_id %q", out.TurnReqID)
	}
	if int(sawBody["round"].(float64)) != 2 {
		t.Fatalf("round %v", sawBody["round"])
	}
	if out.Handle.SessionID != sid {
		t.Fatal("must continue prior id")
	}
}

func TestSessionCreate409FailClosed(t *testing.T) {
	raw, err := os.ReadFile(findRepoFile(t, filepath.Join("testdata", "wire", "errors", "contract_409.response.json")))
	if err != nil {
		t.Fatal(err)
	}
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
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
	cli := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: cfg.Zeus,
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	defer cli.Close(context.Background())
	api := NewSessionAPIWith("host", SessionOptions{Client: cli, Config: cfg})
	handle, err := api.Create(context.Background(), application.SetupOptions{
		ChatRequest: map[string]any{"messages": []any{}},
		ContractID:  "c1",
	})
	requireCode(t, err, domain.CodeZeusContractRequired)
	if handle.SessionID != "" {
		t.Fatal("must not invent session.id")
	}
	de, _ := domain.AsError(err)
	if de.Details["req_id"] != "40900000-0000-4000-8000-000000000409" {
		t.Fatalf("details %v", de.Details)
	}
	if _, ok := de.Details["contract_hash"]; ok {
		t.Fatal("must not forge hash")
	}
}

func TestNewSessionAPINilHost(t *testing.T) {
	if NewSessionAPI(nil) != nil {
		t.Fatal("nil")
	}
	s := NewSessionAPIWith("h", SessionOptions{})
	if s.Host() != "h" {
		t.Fatal("host")
	}
}

func TestG42SessionAPITraceJoinsDispatchReqID(t *testing.T) {
	raw, err := os.ReadFile(findRepoFile(t, filepath.Join("testdata", "wire", "v2_session_trace.response.json")))
	if err != nil {
		t.Fatal(err)
	}
	var doc wireDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	const dispatch = "b2000000-0000-4000-8000-000000000002"
	var sawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session/trace" {
			t.Errorf("path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &sawBody)
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
	cli := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: cfg.Zeus,
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	defer cli.Close(context.Background())
	api := NewSessionAPIWith("host", SessionOptions{Client: cli, Config: cfg, Version: "0.1.0-dev"})
	out := api.Trace(context.Background(), projectors.ProjectOptions{
		Handle: domain.SessionHandle{
			SessionID:    "zsess_mock_turn_001",
			Round:        2,
			Enabled:      true,
			ContractID:   "mock_analytics_b5",
			ContractHash: "mock:00000000000000000000000000000000",
		},
		Hops: []any{map[string]any{
			"req_id": dispatch, "name": "search", "status": 200, "snippet": "ok",
		}},
	})
	if !out.OK {
		t.Fatalf("%+v", out)
	}
	if sawBody["req_id"] != dispatch {
		t.Fatalf("join key %v", sawBody["req_id"])
	}
	if sawBody["req_id"] == "f6000000-0000-4000-8000-000000000006" {
		t.Fatal("must not send trace echo as body.req_id")
	}
	if out.PrimaryReqID != dispatch {
		t.Fatalf("primary %q", out.PrimaryReqID)
	}
	if cli.LastReqID() != "f6000000-0000-4000-8000-000000000006" {
		t.Fatalf("echo %q", cli.LastReqID())
	}
}
