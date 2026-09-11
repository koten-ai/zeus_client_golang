// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

func lifeOn(t *testing.T, srv *httptest.Server) *SessionLifecycle {
	t.Helper()
	cli := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: config.ZeusEndpointConfig{URL: srv.URL, AuthMode: config.AuthNone, TimeoutS: 5, TLSVerify: true},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
		Version:  "0.1.0-dev",
	})
	t.Cleanup(func() { _ = cli.Close(context.Background()) })
	return &SessionLifecycle{Client: cli}
}

func TestLifecycleCreateNewSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set(domain.ReqIDHeader, "req-create")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"session_id":"sid-new","round":1,"contract_status":"match"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest: map[string]any{
			"messages": []any{map[string]any{"role": "system", "content": "rules"}},
			"contract": map[string]any{"hash": "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		UserMessage:       "hello",
		ContractID:        "c1",
		BoundContractHash: "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Mode:              "analytics",
		ChatID:            "chat-1",
		EnableSessions:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.SessionID != "sid-new" || handle.Round != 1 || !handle.Created {
		t.Fatalf("%+v", handle)
	}
	if handle.ContractStatus != "match" || handle.CreateReqID != "req-create" || handle.ChatID != "chat-1" {
		t.Fatalf("%+v", handle)
	}
}

func TestLifecycleRehydrateSuccess(t *testing.T) {
	chat := map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}}
	h := domain.ComputeContractHash(chat)
	sid := "sid-live"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/session/"+sid {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set(domain.ReqIDHeader, "req-reh")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": sid, "round": 2, "hash": h, "contract_id": "c1",
			"conversation":  []any{map[string]any{"role": "user", "content": "hi"}},
			"rounds_loaded": 2,
		})
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	prior := domain.SessionHandle{SessionID: sid, Round: 2, ChatID: "chat-1", ContractID: "c1", ContractHash: h}
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:       chat,
		UserMessage:       "next",
		Prior:             &prior,
		ContractID:        "c1",
		BoundContractHash: h,
		Mode:              "analytics",
		ChatID:            "chat-1",
		EnableSessions:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.SessionID != sid || handle.Round != 3 || !handle.Rehydrated || handle.Created {
		t.Fatalf("%+v", handle)
	}
	if handle.ContractStatus != "match" {
		t.Fatalf("status %s", handle.ContractStatus)
	}
}

func TestLifecycleDeadSIDRecreatesSameTurn(t *testing.T) {
	dead := "stale-sid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(404)
			return
		}
		w.Header().Set(domain.ReqIDHeader, "req-rec")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"session_id":"sid-recover","round":1,"contract_status":"match"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	prior := domain.SessionHandle{SessionID: dead, Round: 6, ChatID: "c"}
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:       map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}},
		UserMessage:       "recover me",
		Prior:             &prior,
		ContractID:        "c1",
		BoundContractHash: "md5:cccccccccccccccccccccccccccccccc",
		Mode:              "analytics",
		ChatID:            "c",
		EnableSessions:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.SessionID != "sid-recover" || !handle.Created || handle.RecoveredFrom != dead || handle.Round != 1 {
		t.Fatalf("%+v", handle)
	}
}

func TestLifecycleDeadSIDAndCreateFailClearsSID(t *testing.T) {
	dead := "stale-2"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte("fail"))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:    map[string]any{},
		UserMessage:    "x",
		Prior:          &domain.SessionHandle{SessionID: dead, Round: 1, ChatID: "c"},
		Mode:           "analytics",
		ChatID:         "c",
		EnableSessions: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if handle.SessionID != "" || handle.Created || handle.RecoveredFrom != dead || handle.Error == "" {
		t.Fatalf("%+v", handle)
	}
}

func TestLifecycleSessionsDisabled(t *testing.T) {
	life := &SessionLifecycle{Client: zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint: config.ZeusEndpointConfig{URL: "http://unused", AuthMode: config.AuthNone},
		Secrets:  secretsenv.NewWithEnviron(map[string]string{}),
	})}
	defer life.Client.Close(context.Background())
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:       map[string]any{},
		UserMessage:       "x",
		ContractID:        "c1",
		BoundContractHash: "md5:x",
		Mode:              "analytics",
		ChatID:            "c",
		EnableSessions:    false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.Enabled || handle.SessionID != "" || handle.ContractID != "c1" {
		t.Fatalf("%+v", handle)
	}
}

func TestLifecycleCommitContinueTurnJustCreated(t *testing.T) {
	sid := "sid-new"
	var sawBody map[string]any
	var sawHdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/session/"+sid+"/turn" {
			t.Errorf("path %s", r.URL.Path)
		}
		sawHdr = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		w.Header().Set(domain.ReqIDHeader, "turn-req")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"session_id":"` + sid + `","round":2,"status":"ok"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	handle := domain.SessionHandle{
		SessionID: sid, Round: 1, ChatID: "c", ContractID: "c1",
		ContractHash: "md5:h", ContractStatus: "match", Created: true, Enabled: true,
	}
	out, err := life.Commit(context.Background(), handle, CommitOptions{
		ChatRequest: map[string]any{},
		ProducedDelta: []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{"role": "assistant", "content": "hello"},
		},
		Mode:       "analytics",
		TurnID:     "turn_join",
		BriefSha12: "briefsha12ab",
		MiniSha12:  "minisha12abc",
	})
	if err != nil || !out.OK || out.Handle.Round != 2 {
		t.Fatalf("%+v %v", out, err)
	}
	if jsonInt(sawBody["round"], 0) != 2 {
		t.Fatalf("round %v", sawBody["round"])
	}
	turns, _ := sawBody["new_turns"].([]any)
	for _, tr := range turns {
		m, _ := tr.(map[string]any)
		if asStr(m["role"]) == "user" {
			t.Fatal("user turn must be stripped on just-created commit")
		}
	}
	if sawHdr.Get("X-Zeus-Chat-Session-Id") != sid {
		t.Fatal("chat session id")
	}
	if sawHdr.Get("X-Zeus-Brief-Sha12") != "briefsha12ab" || sawHdr.Get("X-Zeus-Mini-Sha12") != "minisha12abc" {
		t.Fatal("sha12")
	}
	if sawHdr.Get("X-Zeus-Turn-Id") != "turn_join" {
		t.Fatal("turn")
	}
	if sawHdr.Get("X-Zeus-Session") == sid {
		t.Fatal("must not set X-Zeus-Session to durable id")
	}
}

func TestLifecyclePrefersPayloadHashOnDrift(t *testing.T) {
	var sawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		w.Header().Set(domain.ReqIDHeader, "r1")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"session_id":"s1","round":1,"contract_status":"match"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	chat := map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "rules only"}},
		"verbs":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "find"}}},
	}
	payloadH := domain.ComputeContractHash(chat)
	_, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:       chat,
		UserMessage:       "hi",
		ContractID:        "c1",
		BoundContractHash: "md5:boundddddddddddddddddddddddddddd",
		Mode:              "analytics",
		ChatID:            "c",
		EnableSessions:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawBody["contract_hash"] != payloadH {
		t.Fatalf("got %v want %s", sawBody["contract_hash"], payloadH)
	}
}

func TestLifecycleCreateStampsSessionHeaders(t *testing.T) {
	var sawHdr http.Header
	var sawURL string
	var sawBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawHdr = r.Header.Clone()
		sawURL = r.URL.String()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		w.Header().Set(domain.ReqIDHeader, "req-create-rw")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"session_id":"sid-rw","round":1,"contract_status":"match"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	_, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:    map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}},
		UserMessage:    "hello",
		ContractID:     "c1",
		Mode:           "analytics",
		ChatID:         "chat-rew",
		TurnID:         "turn_abc",
		ForceTrace:     true,
		EnableSessions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawHdr.Get("X-Zeus-Chat-Id") != "chat-rew" || sawHdr.Get("X-Zeus-Turn-Id") != "turn_abc" {
		t.Fatal("correlation")
	}
	if sawHdr.Get("X-Zeus-Trace-Class") != "session" || sawHdr.Get("X-Zeus-Trace") != "1" {
		t.Fatal("trace")
	}
	if sawHdr.Get("X-Zeus-Call-Id") != "" || sawHdr.Get("X-Zeus-Chat-Session-Id") != "" {
		t.Fatal("must omit call-id and chat-session on first create")
	}
	if sawHdr.Get(domain.ReqIDHeader) != "" {
		t.Fatal("must omit req id (Zeus mints)")
	}
	if _, ok := sawBody["rewind"]; ok {
		t.Fatal("rewind in body")
	}
	if strings.Contains(sawURL, "rewind=") {
		t.Fatal("rewind query")
	}
}

func TestModeSwitchCreatesNewSession(t *testing.T) {
	var gotGET bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gotGET = true
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"session_id":"sid-new-mode","round":1,"contract_status":"match"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	prior := domain.SessionHandle{SessionID: "sid-old", Round: 3, ChatID: "c", Mode: "analytics"}
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:    map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}},
		UserMessage:    "switch",
		Prior:          &prior,
		Mode:           "explore",
		ChatID:         "c",
		EnableSessions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotGET {
		t.Fatal("mode switch must not rehydrate")
	}
	if handle.SessionID != "sid-new-mode" || !handle.Created || handle.Mode != "explore" {
		t.Fatalf("%+v", handle)
	}
}

func TestLifecycleCreate409FailClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(domain.ReqIDHeader, "40900000-0000-4000-8000-000000000409")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"contract_required","message":"contract hash mismatch or missing","contract_status":"mismatch"}`))
	}))
	defer srv.Close()
	life := lifeOn(t, srv)
	handle, err := life.Setup(context.Background(), SetupOptions{
		ChatRequest:    map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}},
		UserMessage:    "hi",
		ContractID:     "c1",
		EnableSessions: true,
	})
	if err == nil {
		t.Fatal("expected 409")
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeZeusContractRequired {
		t.Fatalf("%v", err)
	}
	if handle.SessionID != "" {
		t.Fatal("must not invent session id")
	}
	if handle.CreateReqID != "40900000-0000-4000-8000-000000000409" {
		t.Fatalf("req_id %q", handle.CreateReqID)
	}
	if de.Details["req_id"] != handle.CreateReqID {
		t.Fatalf("details %v", de.Details)
	}
	if _, ok := de.Details["contract_hash"]; ok {
		t.Fatal("must not forge hash")
	}
}
