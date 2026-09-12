// SPDX-License-Identifier: BUSL-1.1

package llmopenai

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
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const (
	testBase = "https://api.x.ai/v1"
	testKey  = "sk-test-secret-key"
)

type recHTTP struct {
	mu    sync.Mutex
	n     int
	last  ports.HTTPRequest
	resps []ports.HTTPResponse
	errs  []error
}

func (r *recHTTP) Request(_ context.Context, req ports.HTTPRequest) (ports.HTTPResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last = req
	i := r.n
	r.n++
	var err error
	if i < len(r.errs) {
		err = r.errs[i]
	}
	resp := ports.HTTPResponse{Status: 200, Body: []byte(`{}`)}
	if i < len(r.resps) {
		resp = r.resps[i]
	} else if len(r.resps) > 0 {
		resp = r.resps[len(r.resps)-1]
	}
	return resp, err
}

func (r *recHTTP) Close(context.Context) error { return nil }

func (r *recHTTP) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func testClient(t *testing.T, httpPort ports.HttpPort, extra func(*Options)) *Client {
	t.Helper()
	opts := Options{
		Config: config.LlmProviderConfig{
			Provider:  "xai",
			BaseURL:   testBase,
			Model:     "grok-test",
			APIKeyEnv: "XAI_API_KEY",
			TimeoutS:  5,
		},
		Secrets: secretsenv.NewWithEnviron(map[string]string{"XAI_API_KEY": testKey}),
		HTTP:    httpPort,
		Retry:   config.RetryPolicy{MaxAttempts: 0, BaseDelayMS: 1, Jitter: false},
		Budget:  ptrBudget(domain.RetryBudget{MaxAttempts: 0, MaxExtraMS: 30_000}),
		TurnID:  "turn_t",
	}
	if extra != nil {
		extra(&opts)
	}
	c := New(opts)
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func ptrBudget(b domain.RetryBudget) *domain.RetryBudget { return &b }

func TestCacheHintsBranches(t *testing.T) {
	h, b := CacheHints("grok", testBase, "")
	if len(h) != 0 || len(b) != 0 {
		t.Fatalf("%v %v", h, b)
	}
	h, b = CacheHints("xai", testBase, "conv-1")
	if h["x-grok-conv-id"] != "conv-1" || len(b) != 0 {
		t.Fatalf("%v %v", h, b)
	}
	h2, b2 := CacheHints("openai", "https://api.openai.com/v1", "conv-2")
	if len(h2) != 0 || b2["prompt_cache_key"] != "conv-2" {
		t.Fatalf("%v %v", h2, b2)
	}
}

func TestBuildPayloadToolsAndNoTools(t *testing.T) {
	temp := 0.0
	base := BuildChatPayload("m", []map[string]any{{"role": "user", "content": "hi"}}, nil, &temp, nil, nil)
	if _, ok := base["tools"]; ok {
		t.Fatal("tools")
	}
	if _, ok := base["tool_choice"]; ok {
		t.Fatal("tool_choice")
	}
	withTools := BuildChatPayload("m", nil, []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}}, nil, nil, nil)
	if withTools["tool_choice"] != "auto" {
		t.Fatal("tool_choice")
	}
	tools, _ := withTools["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("tools %v", withTools["tools"])
	}
}

func TestCompleteHappyPathAndNoKeyInJournal(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{
			"role":    "assistant",
			"content": "hello",
			"tool_calls": []any{map[string]any{
				"id": "c1", "type": "function",
				"function": map[string]any{"name": "find", "arguments": "{}"},
			}},
		}}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2},
	})
	h := &recHTTP{resps: []ports.HTTPResponse{{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}}}
	j := journal.NewInMemoryJournal(nil)
	c := testClient(t, h, func(o *Options) { o.Journal = j })
	resp, err := c.Complete(context.Background(), ports.LlmRequest{
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
		Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}},
		ConvID:   "chat-abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello" || len(resp.ToolCalls) != 1 {
		t.Fatalf("%+v", resp)
	}
	fn, _ := resp.ToolCalls[0]["function"].(map[string]any)
	if fn["name"] != "find" {
		t.Fatalf("tool %v", resp.ToolCalls)
	}
	if asIntMust(resp.Usage["prompt_tokens"]) != 10 {
		t.Fatalf("usage %v", resp.Usage)
	}
	auth := h.last.Headers["Authorization"]
	if auth != "Bearer "+testKey {
		t.Fatalf("auth %q", auth)
	}
	if h.last.Headers["x-grok-conv-id"] != "chat-abc" {
		t.Fatalf("conv %v", h.last.Headers)
	}
	blob, _ := json.Marshal(j.Events())
	s := string(blob) + "\n" + c.Logs()
	if strings.Contains(s, testKey) {
		t.Fatal("api key leaked")
	}
	if strings.Contains(s, "Bearer "+testKey) {
		t.Fatal("authorization leaked")
	}
	evs := j.Events()
	found := false
	for _, e := range evs {
		if e.Type == journal.EventLLMRound {
			found = true
			if e.Data["ok"] != true {
				t.Fatalf("ok %v", e.Data["ok"])
			}
		}
	}
	if !found {
		t.Fatal("llm.round missing")
	}
	if !strings.Contains(c.Logs(), "zeus_client.llm.request_finished") {
		t.Fatalf("logs %s", c.Logs())
	}
	if !strings.Contains(c.Logs(), "tokens.input=10") || !strings.Contains(c.Logs(), "tokens.output=2") {
		t.Fatalf("tokens %s", c.Logs())
	}
}

func TestCompleteOmitsTokensWhenUsageMissing(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{
			"role": "assistant", "content": "ok",
		}}},
	})
	h := &recHTTP{resps: []ports.HTTPResponse{{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}}}
	c := testClient(t, h, nil)
	_, err := c.Complete(context.Background(), ports.LlmRequest{
		Messages: []map[string]any{{"role": "user", "content": "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := c.Logs()
	if !strings.Contains(logs, "zeus_client.llm.request_finished") {
		t.Fatalf("logs %s", logs)
	}
	if strings.Contains(logs, "tokens.input") || strings.Contains(logs, "tokens.output") {
		t.Fatalf("must omit missing tokens: %s", logs)
	}
}

func TestComplete429RateRetriesThenOK(t *testing.T) {
	okBody := []byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	h := &recHTTP{resps: []ports.HTTPResponse{
		{Status: 429, Headers: map[string]string{"Content-Type": "application/json", "Retry-After": "0"}, Body: []byte(`{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`)},
		{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: okBody},
	}}
	var sleeps []time.Duration
	c := testClient(t, h, func(o *Options) {
		o.Retry = config.RetryPolicy{MaxAttempts: 2, BaseDelayMS: 10, Jitter: false}
		o.Budget = ptrBudget(domain.RetryBudget{MaxAttempts: 2, MaxExtraMS: 60_000})
		o.Sleep = func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		}
	})
	resp, err := c.Complete(context.Background(), ports.LlmRequest{
		Messages: []map[string]any{{"role": "user", "content": "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("%+v", resp)
	}
	if h.count() != 2 {
		t.Fatalf("calls %d", h.count())
	}
	if len(sleeps) == 0 {
		t.Fatal("expected backoff")
	}
}

func TestComplete429QuotaNeverRetries(t *testing.T) {
	h := &recHTTP{resps: []ports.HTTPResponse{{
		Status:  429,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"error":{"code":"insufficient_quota","type":"insufficient_quota","message":"You exceeded your current quota"}}`),
	}}}
	var sleeps []time.Duration
	c := testClient(t, h, func(o *Options) {
		o.Retry = config.RetryPolicy{MaxAttempts: 5, BaseDelayMS: 1, Jitter: false}
		o.Budget = ptrBudget(domain.RetryBudget{MaxAttempts: 5, MaxExtraMS: 30_000})
		o.Sleep = func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		}
	})
	_, err := c.Complete(context.Background(), ports.LlmRequest{
		Messages: []map[string]any{{"role": "user", "content": "x"}},
	})
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeLLMQuotaExhausted {
		t.Fatalf("%v", err)
	}
	if de.Retryable {
		t.Fatal("quota must not retry")
	}
	if h.count() != 1 {
		t.Fatalf("calls %d", h.count())
	}
	if len(sleeps) != 0 {
		t.Fatalf("sleeps %v", sleeps)
	}
}

func TestCompleteContextLengthNoRetry(t *testing.T) {
	h := &recHTTP{resps: []ports.HTTPResponse{{
		Status:  400,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"error":{"code":"context_length_exceeded","message":"maximum context length exceeded"}}`),
	}}}
	c := testClient(t, h, func(o *Options) {
		o.Retry = config.RetryPolicy{MaxAttempts: 3, BaseDelayMS: 1, Jitter: false}
		o.Budget = ptrBudget(domain.RetryBudget{MaxAttempts: 3, MaxExtraMS: 30_000})
	})
	_, err := c.Complete(context.Background(), ports.LlmRequest{
		Messages: []map[string]any{{"role": "user", "content": "x"}},
	})
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeLLMContextLength || de.Retryable {
		t.Fatalf("%v", err)
	}
	if h.count() != 1 {
		t.Fatalf("calls %d", h.count())
	}
}

func TestMissingAPIKey(t *testing.T) {
	c := New(Options{
		Config:  config.LlmProviderConfig{BaseURL: testBase, Model: "m", APIKeyEnv: "MISSING_KEY"},
		Secrets: secretsenv.NewWithEnviron(map[string]string{}),
		HTTP:    &recHTTP{},
		Budget:  ptrBudget(domain.RetryBudget{MaxAttempts: 0}),
	})
	defer c.Close(context.Background())
	_, err := c.Complete(context.Background(), ports.LlmRequest{
		Messages: []map[string]any{{"role": "user", "content": "x"}},
	})
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeLLMAPIKeyMissing {
		t.Fatalf("%v", err)
	}
}

func TestCustomMissingBaseURL(t *testing.T) {
	c := testClient(t, &recHTTP{}, func(o *Options) {
		o.Config.Provider = "custom"
		o.Config.BaseURL = ""
		o.Config.Model = "local-model"
		o.Config.APIStyle = "openai_compatible"
	})
	_, err := c.Complete(context.Background(), ports.LlmRequest{Messages: []map[string]any{{"role": "user", "content": "x"}}})
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeLLMBaseURLMissing {
		t.Fatalf("%v", err)
	}
}

func TestCustomProviderLogged(t *testing.T) {
	okBody := []byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	h := &recHTTP{resps: []ports.HTTPResponse{{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Body: okBody}}}
	c := testClient(t, h, func(o *Options) {
		o.Config.Provider = "custom"
		o.Config.BaseURL = "http://127.0.0.1:11434/v1"
		o.Config.Model = "llama"
		o.Config.APIStyle = "openai_compatible"
	})
	if _, err := c.Complete(context.Background(), ports.LlmRequest{Messages: []map[string]any{{"role": "user", "content": "x"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Logs(), "llm.provider=custom") {
		t.Fatalf("logs %s", c.Logs())
	}
}

func TestScriptedLLMToolRoundWithoutNetwork(t *testing.T) {
	raw, err := os.ReadFile(findRepoFile(t, filepath.Join("testdata", "wire", "llm_tool_round.script.json")))
	if err != nil {
		t.Fatal(err)
	}
	var script struct {
		Rounds []struct {
			Round     int            `json:"round"`
			Assistant map[string]any `json:"assistant"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal(raw, &script); err != nil {
		t.Fatal(err)
	}
	if len(script.Rounds) < 2 {
		t.Fatal("script rounds")
	}
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.ReadAll(r.Body)
		i := n
		if i >= len(script.Rounds) {
			i = len(script.Rounds) - 1
		}
		n++
		msg := script.Rounds[i].Assistant
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": msg}},
		})
	}))
	defer srv.Close()
	c := New(Options{
		Config: config.LlmProviderConfig{
			Provider: "xai", BaseURL: srv.URL, Model: "grok-test", APIKeyEnv: "XAI_API_KEY", TimeoutS: 5,
		},
		Secrets: secretsenv.NewWithEnviron(map[string]string{"XAI_API_KEY": testKey}),
		Retry:   config.RetryPolicy{MaxAttempts: 0, BaseDelayMS: 1, Jitter: false},
		Budget:  ptrBudget(domain.RetryBudget{MaxAttempts: 0}),
	})
	defer c.Close(context.Background())

	r1, err := c.Complete(context.Background(), ports.LlmRequest{Messages: []map[string]any{{"role": "user", "content": "fruit beer"}}})
	if err != nil {
		t.Fatal(err)
	}
	if toolName(r1) != "search" {
		t.Fatalf("round1 tool %q %+v", toolName(r1), r1.ToolCalls)
	}
	r2, err := c.Complete(context.Background(), ports.LlmRequest{Messages: []map[string]any{{"role": "tool", "content": "hits"}}})
	if err != nil {
		t.Fatal(err)
	}
	if toolName(r2) != "return" {
		t.Fatalf("round2 tool %q %+v", toolName(r2), r2.ToolCalls)
	}
}

func toolName(resp ports.LlmResponse) string {
	if len(resp.ToolCalls) == 0 {
		return ""
	}
	fn, _ := resp.ToolCalls[0]["function"].(map[string]any)
	s, _ := fn["name"].(string)
	return s
}

func asIntMust(v any) int {
	n, _ := asInt(v)
	return n
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
