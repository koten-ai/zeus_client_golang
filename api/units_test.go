// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type scriptedUnitLLM struct {
	mu    sync.Mutex
	calls []ports.LlmRequest
	n     int
	usage map[string]any
	tools bool
}

func (s *scriptedUnitLLM) Complete(_ context.Context, req ports.LlmRequest) (ports.LlmResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	s.n++
	if s.tools && s.n == 1 {
		return ports.LlmResponse{
			ToolCalls: []map[string]any{
				{
					"id":   "c1",
					"type": "function",
					"function": map[string]any{
						"name":      "find",
						"arguments": `{"entity_type": "Beer"}`,
					},
				},
			},
		}, nil
	}
	return ports.LlmResponse{Content: "unit answer", Usage: s.usage}, nil
}

type recordingUnitZeus struct {
	mu    sync.Mutex
	calls []ports.VerbRequest
}

func (r *recordingUnitZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recordingUnitZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return ports.VerbHopResult{
		OK: true, StatusCode: 200, ReqID: "req-" + req.Target.Bucket, Body: map[string]any{},
	}, nil
}

func (r *recordingUnitZeus) snapshot() []ports.VerbRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ports.VerbRequest, len(r.calls))
	copy(out, r.calls)
	return out
}

type stubUnitCatalog struct {
	body map[string]any
	err  error
	keys []ports.CatalogKey
}

func (s *stubUnitCatalog) Load(_ context.Context, key ports.CatalogKey) (ports.CatalogDocument, error) {
	s.keys = append(s.keys, key)
	if s.err != nil {
		return ports.CatalogDocument{}, s.err
	}
	return ports.CatalogDocument{Body: s.body}, nil
}

func (s *stubUnitCatalog) Save(context.Context, ports.CatalogKey, ports.CatalogDocument) error {
	return nil
}

func agentUnit(id string, brief bool) domain.UnitConfig {
	content := "no inject here"
	if brief {
		content = "## SCOPE BRIEF\nscope: b/s\n"
	}
	return domain.UnitConfig{
		UnitID:      id,
		Kind:        domain.UnitKindAgentTurn,
		Goal:        "shortlist fruit beers",
		ZeusURL:     "http://127.0.0.1:8080",
		Bucket:      "beer-sample",
		Scope:       "sales",
		Collection:  "_default",
		CatalogMode: "analytics",
		ChatRequest: map[string]any{
			"messages": []any{map[string]any{"role": "system", "content": content}},
		},
	}
}

func directUnit(id, bucket, verb string) domain.UnitConfig {
	return domain.UnitConfig{
		UnitID:     id,
		Kind:       domain.UnitKindZeusDirect,
		Goal:       "list beers",
		ZeusURL:    "http://127.0.0.1:8080",
		Bucket:     bucket,
		Scope:      "sales",
		Collection: "_default",
		Call:       map[string]any{"verb": verb, "body": map[string]any{"entity_type": "Beer"}},
	}
}

func unitsAPI(llm ports.LlmPort, zeus ports.ZeusPort) (*UnitsAPI, *journal.InMemoryJournal) {
	j := journal.NewInMemoryJournal(nil)
	cfg := config.RuntimeConfig{
		Target: config.DataTarget{Bucket: "west", Scope: "s", Collection: "c"},
		Zeus:   config.ZeusEndpointConfig{URL: "http://127.0.0.1:8080"},
		LLM: config.LlmProviderConfig{
			Model:     "default-model",
			APIKeyEnv: "LLM_DEFAULT_KEY",
			Roles: map[string]config.LlmRoleConfig{
				"worker": {Model: "fast-worker", APIKeyEnv: "LLM_DEFAULT_KEY"},
			},
		},
		Settings: config.ClientSettings{DurableSessions: false, Mode: "analytics", MaxRounds: 4},
	}
	return NewUnitsAPIWith("host", UnitsOptions{
		LLM:     llm,
		Zeus:    zeus,
		Journal: j,
		Config:  cfg,
		Version: "0.1.0",
		Clock:   ports.SystemClock{},
	}), j
}

func TestAgentTurnUsesWorkerModelAndOwnSession(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, j := unitsAPI(llm, zeus)
	a, err := u.AgentTurn(context.Background(), agentUnit("u1", true), UnitCallParams{JobID: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := u.AgentTurn(context.Background(), agentUnit("u2", true), UnitCallParams{JobID: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != domain.UnitStatusOK || b.Status != domain.UnitStatusOK {
		t.Fatalf("%+v %+v", a, b)
	}
	if a.Answer != "unit answer" {
		t.Fatalf("answer %q", a.Answer)
	}
	if llm.calls[0].Model != "fast-worker" || llm.calls[1].Model != "fast-worker" {
		t.Fatalf("model %q %q", llm.calls[0].Model, llm.calls[1].Model)
	}
	if sid, _ := a.Artifacts["session_id"]; sid != nil && sid != "" {
		t.Fatalf("session %v", sid)
	}
	types := map[string]bool{}
	for _, e := range j.Events() {
		if strings.HasPrefix(e.Type, "unit.") {
			types[e.Type] = true
		}
	}
	if !types[journal.EventUnitStarted] || !types[journal.EventUnitFinished] {
		t.Fatalf("journal %v", types)
	}
}

func TestAgentTurnBorrowsLiveBriefWhenInjectMissing(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	u.opts.FetchChatRequest = func(_ context.Context, _, _, _ string) (map[string]any, error) {
		return map[string]any{
			"messages": []any{
				map[string]any{"content": "## SCOPE BRIEF\nscope: beer-sample/sales\n\n## MINI-SCHEMA\n### Beer\n"},
			},
		}, nil
	}
	result, err := u.AgentTurn(context.Background(), agentUnit("u1", false), UnitCallParams{JobID: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusOK {
		t.Fatalf("%+v", result)
	}
	sys := fmt.Sprint(llm.calls[0].Messages[0]["content"])
	if !strings.Contains(sys, "## SCOPE BRIEF") || !strings.Contains(sys, "## MINI-SCHEMA") {
		t.Fatalf("system %v", sys)
	}
}

func TestAgentTurnUnitLLMOverrideWinsModel(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	unit := agentUnit("u1", true)
	unit.LLM = map[string]any{"model": "fast-worker-v3"}
	result, err := u.AgentTurn(context.Background(), unit, UnitCallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusOK {
		t.Fatal(result.Status)
	}
	if llm.calls[0].Model != "fast-worker-v3" {
		t.Fatalf("model %q", llm.calls[0].Model)
	}
}

func TestAgentTurnDistinctWorkerKeyDoesNotUseProcessLLM(t *testing.T) {
	process := &scriptedUnitLLM{}
	worker := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, j := unitsAPI(process, zeus)
	u.opts.Config.LLM.Roles["worker"] = config.LlmRoleConfig{Model: "fast-worker", APIKeyEnv: "LLM_WORKER_KEY"}
	u.opts.LLMForSlice = func(slice config.ResolvedLlmSlice) ports.LlmPort {
		if slice.APIKeyEnv == "LLM_WORKER_KEY" {
			return worker
		}
		return process
	}
	result, err := u.AgentTurn(context.Background(), agentUnit("u1", true), UnitCallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusOK {
		t.Fatal(result.Status)
	}
	if len(process.calls) != 0 {
		t.Fatalf("process used %d", len(process.calls))
	}
	if len(worker.calls) == 0 || worker.calls[0].Model != "fast-worker" {
		t.Fatalf("worker %+v", worker.calls)
	}
	var started journal.JournalEvent
	for _, e := range j.Events() {
		if e.Type == journal.EventUnitStarted {
			started = e
			break
		}
	}
	if started.Data["api_key_env"] != "LLM_WORKER_KEY" {
		t.Fatalf("%v", started.Data)
	}
	if strings.Contains(fmt.Sprint(started.Data), "sk-") {
		t.Fatal("secret in journal")
	}
}

func TestMode1RunTurnIgnoresLLMRoles(t *testing.T) {
	llm := &scriptedUnitLLM{}
	a := NewAgentAPIWith("host", AgentOptions{
		LLM: llm,
		Config: config.RuntimeConfig{
			LLM:      config.LlmProviderConfig{Model: "default-model", Roles: map[string]config.LlmRoleConfig{"worker": {Model: "fast-worker"}}},
			Settings: config.ClientSettings{MaxRounds: 4, Mode: "analytics"},
		},
		Version: "0.1.0",
	})
	if _, err := a.RunTurn(context.Background(), "hello", RunTurnParams{EnableSessions: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	if llm.calls[0].Model != "default-model" {
		t.Fatalf("mode1 model %q", llm.calls[0].Model)
	}
}

func TestAgentTurnCopiesUsageOntoArtifacts(t *testing.T) {
	llm := &scriptedUnitLLM{usage: map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	result, err := u.AgentTurn(context.Background(), agentUnit("u1", true), UnitCallParams{})
	if err != nil {
		t.Fatal(err)
	}
	usage, _ := result.Artifacts["usage"].(map[string]any)
	if usage["prompt"] != 10 || usage["completion"] != 2 {
		t.Fatalf("usage %v", usage)
	}
	if strings.Contains(fmt.Sprint(result.Artifacts), "api_key") {
		t.Fatal("api_key in artifacts")
	}
}

func TestAgentTurnMissingInjectIs130012(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	_, err := u.AgentTurn(context.Background(), agentUnit("u1", false), UnitCallParams{})
	requireAPICode(t, err, domain.CodeUnitsInjectMissing)
	if len(llm.calls) != 0 {
		t.Fatal("llm")
	}
}

func TestAgentTurnHonorsCanceledCtx(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := u.AgentTurn(ctx, agentUnit("u1", true), UnitCallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusCancelled {
		t.Fatalf("%+v", result)
	}
	if len(llm.calls) != 0 {
		t.Fatal("llm")
	}
}

func TestZeusDirectRejectsPipelineAndRecordsReqID(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	piped, err := u.ZeusDirect(context.Background(), directUnit("u1", "east", "pipeline"), UnitCallParams{JobID: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := u.ZeusDirect(context.Background(), directUnit("u2", "east", "find"), UnitCallParams{JobID: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	if piped.Status != domain.UnitStatusError || piped.ErrorCode != string(domain.CodeZeusPipelineNotOnDirect) {
		t.Fatalf("pipeline %+v", piped)
	}
	if ok.Status != domain.UnitStatusOK {
		t.Fatalf("%+v", ok)
	}
	found := false
	for _, id := range ok.ReqIDs {
		if id == "req-east" {
			found = true
		}
	}
	if !found {
		t.Fatalf("req_ids %v", ok.ReqIDs)
	}
	calls := zeus.snapshot()
	last := calls[len(calls)-1]
	if last.Target.Bucket != "east" {
		t.Fatalf("bucket %q", last.Target.Bucket)
	}
	if last.Headers["X-Zeus-Chat-Id"] != "job_1" {
		t.Fatalf("chat %v", last.Headers)
	}
	if last.Headers["X-Zeus-Trace-Class"] != "direct.read" {
		t.Fatalf("trace %v", last.Headers)
	}
	if len(llm.calls) != 0 {
		t.Fatal("direct must skip LLM")
	}
}

func TestZeusDirectUnitsUsePerUnitZeusURL(t *testing.T) {
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(&scriptedUnitLLM{}, zeus)
	east := directUnit("u1", "east", "find")
	east.ZeusURL = "http://zeus-east:8080"
	west := directUnit("u2", "west", "find")
	west.ZeusURL = "http://zeus-west:8080"
	if _, err := u.ZeusDirect(context.Background(), east, UnitCallParams{}); err != nil {
		t.Fatal(err)
	}
	if _, err := u.ZeusDirect(context.Background(), west, UnitCallParams{}); err != nil {
		t.Fatal(err)
	}
	calls := zeus.snapshot()
	if calls[0].BaseURL != "http://zeus-east:8080" || calls[1].BaseURL != "http://zeus-west:8080" {
		t.Fatalf("%q %q", calls[0].BaseURL, calls[1].BaseURL)
	}
}

func TestZeusDirectAuthNoneDoesNotInheritProcessBasic(t *testing.T) {
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(&scriptedUnitLLM{}, zeus)
	u.opts.Config.Zeus.AuthMode = config.AuthBasic
	u.opts.Config.Zeus.Username = "admin"
	u.opts.Config.Zeus.PasswordEnv = "ZEUS_PASSWORD"
	unit := directUnit("u1", "east", "find")
	unit.AuthMode = "none"
	if _, err := u.ZeusDirect(context.Background(), unit, UnitCallParams{}); err != nil {
		t.Fatal(err)
	}
	got := zeus.snapshot()[0]
	if got.AuthMode != "none" {
		t.Fatalf("auth %q", got.AuthMode)
	}
	if got.PasswordEnv != "" {
		t.Fatalf("password_env %q", got.PasswordEnv)
	}
}

func TestAgentTurnToolHopUsesUnitZeusURL(t *testing.T) {
	llm := &scriptedUnitLLM{tools: true}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	unit := agentUnit("u1", true)
	unit.ZeusURL = "http://zeus-b:8080"
	result, err := u.AgentTurn(context.Background(), unit, UnitCallParams{JobID: "job_1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusOK {
		t.Fatalf("%+v", result)
	}
	calls := zeus.snapshot()
	if len(calls) == 0 {
		t.Fatal("no zeus hop")
	}
	if calls[0].BaseURL != "http://zeus-b:8080" {
		t.Fatalf("url %q", calls[0].BaseURL)
	}
	if calls[0].Verb != "find" {
		t.Fatalf("verb %q", calls[0].Verb)
	}
}

func TestAgentTurnOtherHostMissingInjectIs130012(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	unit := agentUnit("u1", false)
	unit.ZeusURL = "http://zeus-b:8080"
	_, err := u.AgentTurn(context.Background(), unit, UnitCallParams{})
	requireAPICode(t, err, domain.CodeUnitsInjectMissing)
	if len(llm.calls) != 0 || len(zeus.snapshot()) != 0 {
		t.Fatal("must not hop")
	}
}

func TestZeusDirectMissingVerbIs130002(t *testing.T) {
	u, _ := unitsAPI(&scriptedUnitLLM{}, &recordingUnitZeus{})
	unit := domain.UnitConfig{
		UnitID: "u1", Kind: domain.UnitKindZeusDirect, Goal: "x",
		ZeusURL: "http://z", Bucket: "b", Scope: "s", Collection: "c",
		Call: map[string]any{"body": map[string]any{}},
	}
	_, err := u.ZeusDirect(context.Background(), unit, UnitCallParams{})
	requireAPICode(t, err, domain.CodeJobsInvalidUnitMap)
}

func TestAgentTurnIsolatedCatalogLoad(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	cat := &stubUnitCatalog{body: map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "## SCOPE BRIEF\nscope: beer-sample/sales\n"}},
	}}
	u.opts.Catalog = cat
	unit := agentUnit("u1", true)
	unit.ChatRequest = nil
	unit.CatalogMode = "analytics"
	unit.BaseID = "base-5.3"
	result, err := u.AgentTurn(context.Background(), unit, UnitCallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusOK {
		t.Fatalf("%+v", result)
	}
	if len(cat.keys) != 1 || cat.keys[0].Bucket != "beer-sample" || cat.keys[0].Scope != "sales" {
		t.Fatalf("catalog key %+v", cat.keys)
	}
}

func TestUnitsJournalRace(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, j := unitsAPI(llm, zeus)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		id := i
		go func() {
			defer wg.Done()
			unit := directUnit("u-race", "east", "find")
			unit.UnitID = "u" + strconv.Itoa(id)
			_, _ = u.ZeusDirect(ctx, unit, UnitCallParams{JobID: "job_race"})
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if len(j.Events()) == 0 {
		t.Fatal("no events")
	}
}

func TestAgentTurnPrefersDirectForPureDataKind(t *testing.T) {
	llm := &scriptedUnitLLM{}
	zeus := &recordingUnitZeus{}
	u, _ := unitsAPI(llm, zeus)
	result, err := u.AgentTurn(context.Background(), directUnit("u1", "east", "find"), UnitCallParams{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.UnitStatusOK {
		t.Fatalf("%+v", result)
	}
	if len(llm.calls) != 0 {
		t.Fatal("must skip LLM")
	}
	if len(zeus.snapshot()) != 1 {
		t.Fatal("expected one direct hop")
	}
}

func requireAPICode(t *testing.T, err error, code domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("got %v want %s", err, code)
	}
}
