// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type scriptedLLM struct {
	script []any
	calls  []ports.LlmRequest
}

func (s *scriptedLLM) Complete(_ context.Context, req ports.LlmRequest) (ports.LlmResponse, error) {
	s.calls = append(s.calls, req)
	if len(s.script) == 0 {
		return ports.LlmResponse{Content: ""}, nil
	}
	item := s.script[0]
	s.script = s.script[1:]
	if err, ok := item.(error); ok {
		return ports.LlmResponse{}, err
	}
	if r, ok := item.(ports.LlmResponse); ok {
		return r, nil
	}
	return ports.LlmResponse{}, nil
}

type blockingLLM struct {
	started chan struct{}
}

func (b *blockingLLM) Complete(ctx context.Context, _ ports.LlmRequest) (ports.LlmResponse, error) {
	if b.started != nil {
		select {
		case <-b.started:
		default:
			close(b.started)
		}
	}
	<-ctx.Done()
	return ports.LlmResponse{}, ctx.Err()
}

type scriptedZeus struct {
	results map[string]ports.VerbHopResult
	calls   []ports.VerbRequest
}

func (z *scriptedZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (z *scriptedZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	z.calls = append(z.calls, req)
	if z.results != nil {
		if r, ok := z.results[req.Verb]; ok {
			return r, nil
		}
	}
	return ports.VerbHopResult{
		OK:         true,
		StatusCode: 200,
		ReqID:      "req-" + req.Verb,
		Body:       map[string]any{"result": map[string]any{"items": []any{map[string]any{"id": "1"}}}},
	}, nil
}

func tc(name string, args map[string]any, callID string) map[string]any {
	raw, _ := json.Marshal(args)
	return map[string]any{
		"id":   callID,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": string(raw),
		},
	}
}

func returnArgs(extra map[string]any) map[string]any {
	base := map[string]any{
		"summary":             "Found two beers in Tampa.",
		"query_decomposition": map[string]any{"intent": "List", "entity": "Beer"},
		"decomposition":       map[string]any{"targets": []any{map[string]any{"entity_type": "Beer"}}},
		"confidence":          "high",
		"policy_action":       "answer",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

var (
	findTool = map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "find", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}},
	}
	returnTool = map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "return", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}},
	}
	describeTool = map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "describe", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}},
	}
	pipelineTool = map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "pipeline", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}},
	}
)

func TestDirectAnswerNoTools(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "Hello from Zeus."}}}
	result := RunAgentTurn(context.Background(), TurnRequest{Message: "hi"}, RunAgentTurnOpts{LLM: llm})
	if result.Status != TurnOK {
		t.Fatalf("status %s", result.Status)
	}
	if result.Answer != "Hello from Zeus." {
		t.Fatalf("answer %q", result.Answer)
	}
	if result.Debug.Rounds != 1 {
		t.Fatalf("rounds %d", result.Debug.Rounds)
	}
	if result.Debug.AIProcessResultExit != "direct" {
		t.Fatalf("exit %q", result.Debug.AIProcessResultExit)
	}
	if strings.Contains(result.Answer, "wish_i_knew") {
		t.Fatal("g2 in answer")
	}
}

func TestTurnFinishedHangsTelemetry(t *testing.T) {
	var recs []map[string]any
	logf := func(level, msg string, attrs map[string]any) {
		cp := map[string]any{}
		for k, v := range attrs {
			cp[k] = v
		}
		recs = append(recs, map[string]any{"level": level, "msg": msg, "attrs": cp})
	}
	llm := &scriptedLLM{script: []any{ports.LlmResponse{
		Content: "Hello from Zeus.",
		Usage:   map[string]any{"prompt_tokens": 12, "completion_tokens": 4, "total_tokens": 16},
	}}}
	result := RunAgentTurn(context.Background(), TurnRequest{Message: "hi"}, RunAgentTurnOpts{LLM: llm, Log: logf})
	if result.Status != TurnOK {
		t.Fatalf("status %s", result.Status)
	}
	if result.Debug.Tokens["prompt"] != 12 || result.Debug.Tokens["completion"] != 4 || result.Debug.Tokens["ok"] != true {
		t.Fatalf("tokens %v", result.Debug.Tokens)
	}
	var fin map[string]any
	for _, r := range recs {
		if r["msg"] == "zeus_client.turn.finished" {
			fin, _ = r["attrs"].(map[string]any)
		}
		if msg, _ := r["msg"].(string); strings.Contains(msg, "hop.telemetry") {
			t.Fatalf("invented %v", r["msg"])
		}
	}
	if fin == nil {
		t.Fatalf("missing turn.finished in %v", recs)
	}
	if fin["tokens.input"] != 12 || fin["tokens.output"] != 4 {
		t.Fatalf("finish tokens %v", fin)
	}
	if _, ok := fin["duration_ms"]; !ok {
		t.Fatal("duration_ms")
	}
	if _, ok := fin["bytes.in"]; ok {
		t.Fatal("bytes on a no-Zeus turn")
	}
}

func TestTurnFinishedOmitsTokensWhenLLMNotBilled(t *testing.T) {
	var recs []map[string]any
	logf := func(level, msg string, attrs map[string]any) {
		recs = append(recs, map[string]any{"level": level, "msg": msg, "attrs": cloneAnyMap(attrs)})
	}
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "Hello from Zeus."}}}
	RunAgentTurn(context.Background(), TurnRequest{Message: "hi"}, RunAgentTurnOpts{LLM: llm, Log: logf})
	for _, r := range recs {
		if r["msg"] != "zeus_client.turn.finished" {
			continue
		}
		attrs, _ := r["attrs"].(map[string]any)
		if _, ok := attrs["tokens.input"]; ok {
			t.Fatalf("must omit missing tokens: %v", attrs)
		}
	}
}

func TestSingleToolThenReturnInsight(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("find", map[string]any{"entity_type": "Beer", "limit": 2}, "c1"),
			tc("return", returnArgs(nil), "c2"),
		}},
		ports.LlmResponse{Content: "There are two great beers to try."},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"find": {OK: true, StatusCode: 200, ReqID: "req-find-1", Body: map[string]any{
			"result": map[string]any{"items": []any{map[string]any{"name": "IPA"}, map[string]any{"name": "Stout"}}},
		}},
	}}
	j := journal.NewInMemoryJournal(nil)
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "list beers",
		Tools:    []map[string]any{findTool, returnTool},
		Settings: config.ClientSettings{AIProcessResult: true, MaxRounds: 4},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus, Journal: j})
	if result.Answer != "There are two great beers to try." {
		t.Fatalf("answer %q", result.Answer)
	}
	if result.Debug.AIProcessResultExit != "insight" {
		t.Fatalf("exit %q", result.Debug.AIProcessResultExit)
	}
	if len(zeus.calls) != 1 || zeus.calls[0].Verb != "find" {
		t.Fatalf("zeus calls %+v", zeus.calls)
	}
	if !zeus.calls[0].AllowPipeline {
		t.Fatal("agent path must allow pipeline")
	}
	if llm.calls[1].Tools != nil && len(llm.calls[1].Tools) != 0 {
		t.Fatalf("insight hop must have no tools: %+v", llm.calls[1].Tools)
	}
	if result.Structured == nil || result.Structured.Policy != "answer" {
		t.Fatalf("structured %+v", result.Structured)
	}
	if strings.Contains(result.Answer, "wish_i_knew") {
		t.Fatal("g2")
	}
	if len(result.Debug.Hops) == 0 || asString(result.Debug.Hops[0]["req_id"]) != "req-find-1" {
		t.Fatalf("hops %+v", result.Debug.Hops)
	}
	started := false
	finished := false
	for _, ev := range j.Events() {
		if ev.Type == journal.EventTurnStarted {
			started = true
		}
		if ev.Type == journal.EventTurnCompleted {
			finished = true
		}
	}
	if !started || !finished {
		t.Fatalf("journal started=%v finished=%v", started, finished)
	}
}

func TestCheapTerminalNoInsightHop(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("find", map[string]any{"entity_type": "Beer"}, "c1"),
			tc("return", returnArgs(map[string]any{"summary": "Terminal cheap summary."}), "c2"),
		}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "list beers",
		Tools:    []map[string]any{findTool, returnTool},
		Settings: config.ClientSettings{AIProcessResult: false, MaxRounds: 4},
	}, RunAgentTurnOpts{LLM: llm, Zeus: &scriptedZeus{}})
	if result.Answer != "Terminal cheap summary." {
		t.Fatalf("answer %q", result.Answer)
	}
	if result.Debug.AIProcessResultExit != "cheap_terminal" {
		t.Fatalf("exit %q", result.Debug.AIProcessResultExit)
	}
	if len(llm.calls) != 1 {
		t.Fatalf("calls %d", len(llm.calls))
	}
}

func TestCheapFinalAfterDataWithoutReturn(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("find", map[string]any{"entity_type": "Beer", "summary": "From tool args."}, "c1"),
		}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "find beers",
		Tools:    []map[string]any{findTool},
		Settings: config.ClientSettings{AIProcessResult: false, MaxRounds: 3},
	}, RunAgentTurnOpts{LLM: llm, Zeus: &scriptedZeus{}})
	if result.Answer != "From tool args." {
		t.Fatalf("answer %q", result.Answer)
	}
	if result.Debug.AIProcessResultExit != "cheap_final" {
		t.Fatalf("exit %q", result.Debug.AIProcessResultExit)
	}
	if len(llm.calls) != 1 {
		t.Fatalf("calls %d", len(llm.calls))
	}
}

func TestCheapFinalStaticWhenNoToolSummary(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("find", map[string]any{"entity_type": "Beer"}, "c1")}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "find",
		Tools:    []map[string]any{findTool},
		Settings: config.ClientSettings{AIProcessResult: false},
	}, RunAgentTurnOpts{LLM: llm, Zeus: &scriptedZeus{}})
	if result.Answer != CheapFinalStaticAnswer {
		t.Fatalf("answer %q", result.Answer)
	}
}

func TestDescribeDoesNotCheapFinal(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("describe", map[string]any{}, "c1")}},
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("find", map[string]any{"entity_type": "Airport", "summary": "US airports."}, "c2"),
		}},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"describe": {OK: true, StatusCode: 200, ReqID: "req-desc", Body: map[string]any{
			"result": map[string]any{"entity_types": map[string]any{"entities": []any{map[string]any{"name": "Airport"}}}},
		}},
		"find": {OK: true, StatusCode: 200, ReqID: "req-find", Body: map[string]any{
			"result": map[string]any{"items": []any{map[string]any{"name": "SFO"}}},
		}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "Give me the list of airports in US",
		Tools:    []map[string]any{describeTool, findTool},
		Settings: config.ClientSettings{AIProcessResult: false, MaxRounds: 4},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	if result.Answer != "US airports." {
		t.Fatalf("answer %q", result.Answer)
	}
	if result.Debug.AIProcessResultExit != "cheap_final" {
		t.Fatalf("exit %q", result.Debug.AIProcessResultExit)
	}
	if len(zeus.calls) != 2 || zeus.calls[0].Verb != "describe" || zeus.calls[1].Verb != "find" {
		t.Fatalf("verbs %+v", zeus.calls)
	}
	if len(llm.calls) != 2 {
		t.Fatalf("llm calls %d", len(llm.calls))
	}
}

func TestPeelLayerADumpFromInsight(t *testing.T) {
	dump := "```\n" +
		`summary: "Peeled salon answer."` + "\n" +
		"confidence: med\n" +
		`query_decomposition: {"intent": "x"}` + "\n" +
		`decomposition: {"targets": []}` + "\n" +
		"policy_action: answer\n" +
		`wish_i_knew: [{"gap": "secret"}]` + "\n" +
		"```"
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("return", returnArgs(map[string]any{"summary": "Bag summary."}), "c1")}},
		ports.LlmResponse{Content: dump},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "q",
		Tools:    []map[string]any{returnTool},
		Settings: config.ClientSettings{AIProcessResult: true, MaxRounds: 3},
	}, RunAgentTurnOpts{LLM: llm, Zeus: &scriptedZeus{}})
	if result.Answer != "Peeled salon answer." {
		t.Fatalf("answer %q", result.Answer)
	}
	if strings.Contains(result.Answer, "wish_i_knew") || strings.Contains(result.Answer, "secret") {
		t.Fatal("g2 leaked")
	}
}

func TestEmptyMessageErrors(t *testing.T) {
	result := RunAgentTurn(context.Background(), TurnRequest{Message: "  "}, RunAgentTurnOpts{LLM: &scriptedLLM{}})
	if result.Status != TurnError || result.Err == nil || result.Err.Code != domain.CodeAgentMessageEmpty {
		t.Fatalf("%+v", result.Err)
	}
}

func TestTurnIDIsUUIDv4AndErrorCarriesIDs(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		domain.NewLLM(domain.CodeAgentLLMRequestFailed, "llm", domain.WithMessage("boom")),
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message: "q",
		Target:  config.DataTarget{Bucket: "yelp-data", Scope: "_default"},
	}, RunAgentTurnOpts{LLM: llm, ZeusURL: "http://zeus.test:8080"})
	if !domain.IsUUIDv4(result.Debug.TurnID) || !domain.IsUUIDv4(result.Debug.ChatID) {
		t.Fatalf("ids turn=%s chat=%s", result.Debug.TurnID, result.Debug.ChatID)
	}
	if asString(result.Debug.Stamp["user"]) != "zeus_client" {
		t.Fatalf("stamp %v", result.Debug.Stamp)
	}
	if err := domain.AssertProductStamp(result.Debug.Stamp); err != nil {
		t.Fatalf("Helios-style filter: %v", err)
	}
	if result.Err == nil || asString(result.Err.Details["zeus.url"]) != "http://zeus.test:8080" {
		t.Fatalf("err %+v", result.Err)
	}
}

func TestStampSwitchSameTurnPath(t *testing.T) {
	tests := []struct {
		name        string
		stampUser   string
		want        string
		productPure bool
	}{
		{name: "default product", want: domain.ProductUser, productPure: true},
		{name: "explicit product", stampUser: domain.ProductUser, want: domain.ProductUser, productPure: true},
		{name: "hub admin", stampUser: domain.HubUser, want: domain.HubUser, productPure: false},
		{name: "unknown fail-closed", stampUser: "not-a-user", want: domain.ProductUser, productPure: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			llm := &scriptedLLM{script: []any{
				ports.LlmResponse{Content: "same answer"},
			}}
			result := RunAgentTurn(context.Background(), TurnRequest{Message: "hi"}, RunAgentTurnOpts{
				LLM:       llm,
				Version:   "0.1.0",
				StampUser: tc.stampUser,
			})
			if result.Answer != "same answer" || result.Status != TurnOK {
				t.Fatalf("%+v", result)
			}
			if asString(result.Debug.Stamp["user"]) != tc.want {
				t.Fatalf("stamp %v", result.Debug.Stamp)
			}
			err := domain.AssertProductStamp(result.Debug.Stamp)
			if tc.productPure {
				if err != nil {
					t.Fatalf("Helios-style filter: %v", err)
				}
			} else if err == nil {
				t.Fatal("Helios-style filter must reject Hub admin")
			}
		})
	}
}

func TestMiddlewareBeforeZeusMutatesArgs(t *testing.T) {
	chain := &MiddlewareChain{}
	chain.Add(mwBeforeZeus{name: "mutate", fn: func(_ *MiddlewareContext, _ string, args map[string]any) map[string]any {
		out := cloneAnyMap(args)
		out["limit"] = 99
		return out
	}})
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("find", map[string]any{"entity_type": "Beer", "limit": 1}, "c1")}},
	}}
	zeus := &scriptedZeus{}
	RunAgentTurn(context.Background(), TurnRequest{
		Message:  "q",
		Tools:    []map[string]any{findTool},
		Settings: config.ClientSettings{AIProcessResult: false},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus, Middleware: chain})
	if asIntAny(zeus.calls[0].Body["limit"]) != 99 {
		t.Fatalf("limit %v", zeus.calls[0].Body["limit"])
	}
}

type mwBeforeZeus struct {
	NoopMiddleware
	name string
	fn   func(*MiddlewareContext, string, map[string]any) map[string]any
}

func (m mwBeforeZeus) Name() string { return m.name }
func (m mwBeforeZeus) BeforeZeus(ctx *MiddlewareContext, name string, args map[string]any) map[string]any {
	return m.fn(ctx, name, args)
}

func TestAIProcessRaisesMaxRoundsFloor(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "ok"}}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "hi",
		Settings: config.ClientSettings{AIProcessResult: true, MaxRounds: 1},
	}, RunAgentTurnOpts{LLM: llm})
	found := false
	for _, n := range result.Debug.Notes {
		if strings.Contains(n, "max_rounds raised to 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes %v", result.Debug.Notes)
	}
}

func TestPipelineAllowFlag(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("pipeline", map[string]any{"steps": []any{}, "turn_complete": true, "summary": "Pipe done."}, "c1"),
		}},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"pipeline": {OK: true, StatusCode: 200, ReqID: "req-pipe", Body: map[string]any{"turn_complete": true, "summary": "Pipe done."}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "run",
		Tools:    []map[string]any{pipelineTool},
		Settings: config.ClientSettings{AIProcessResult: false},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	if !zeus.calls[0].AllowPipeline {
		t.Fatal("allow_pipeline")
	}
	if result.Debug.AIProcessResultExit != "cheap_terminal" {
		t.Fatalf("exit %q", result.Debug.AIProcessResultExit)
	}
	if !strings.Contains(result.Answer, "Pipe done") {
		t.Fatalf("answer %q", result.Answer)
	}
}

func TestToolHopStampsCorrelationHeaders(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("find", map[string]any{"entity_type": "Beer"}, "c1")}},
		ports.LlmResponse{Content: "ok"},
	}}
	zeus := &scriptedZeus{}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "find beers",
		ChatID:   "chat-rew",
		Tools:    []map[string]any{findTool},
		Settings: config.ClientSettings{ForceTrace: true, Mode: "analytics", AIProcessResult: true},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	if len(zeus.calls) == 0 {
		t.Fatal("expected find hop")
	}
	h := zeus.calls[0].Headers
	if h["X-Zeus-Chat-Id"] != "chat-rew" {
		t.Fatalf("chat %v", h)
	}
	if h["X-Zeus-Turn-Id"] != result.Debug.TurnID {
		t.Fatalf("turn header %v vs %s", h["X-Zeus-Turn-Id"], result.Debug.TurnID)
	}
	if !domain.IsUUIDv4(result.Debug.TurnID) {
		t.Fatal("turn id")
	}
	if h["X-Zeus-Call-Id"] != "c1" {
		t.Fatalf("call %v", h)
	}
	if h["X-Zeus-Trace-Class"] != "agent" {
		t.Fatalf("class %v", h)
	}
	if h["X-Zeus-Trace"] != "1" {
		t.Fatalf("trace %v", h)
	}
	if _, ok := h["X-Zeus-Req-Id"]; ok {
		t.Fatal("must not mint req id")
	}
	if zeus.calls[0].Rewind {
		t.Fatal("rewind default off")
	}
}

func TestG2StaysOutOfAnswerOnBadLayerA(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("return", map[string]any{
				"summary":     "ok",
				"confidence":  "high",
				"wish_i_knew": []any{map[string]any{"what": "secret"}},
			}, "c1"),
		}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "hi",
		Tools:    []map[string]any{returnTool},
		Settings: config.ClientSettings{AIProcessResult: false},
	}, RunAgentTurnOpts{LLM: llm, Zeus: &scriptedZeus{}})
	if result.Status != TurnError {
		t.Fatalf("status %s", result.Status)
	}
	if strings.Contains(result.Answer, "wish_i_knew") || strings.Contains(result.Answer, "secret") {
		t.Fatalf("g2 in answer %q", result.Answer)
	}
}

func TestFatToolJSONTruncatedBeforeNextLLM(t *testing.T) {
	fat := strings.Repeat("x", maxToolJSONBytes+4096)
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("find", map[string]any{"entity_type": "Beer"}, "c1")}},
		ports.LlmResponse{Content: "ok"},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"find": {OK: true, StatusCode: 200, ReqID: "req-fat", Body: map[string]any{
			"result": map[string]any{"items": []any{map[string]any{"blob": fat}}},
		}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "find beers",
		Tools:    []map[string]any{findTool},
		Settings: config.ClientSettings{AIProcessResult: true, MaxRounds: 3},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	if len(llm.calls) < 2 {
		t.Fatalf("need second llm hop, got %d", len(llm.calls))
	}
	found := false
	for _, m := range llm.calls[1].Messages {
		if asString(m["role"]) != "tool" {
			continue
		}
		c := asString(m["content"])
		if len(c) > maxToolJSONBytes+len("\n…[truncated]")+8 {
			t.Fatalf("tool content still fat: %d", len(c))
		}
		if strings.Contains(c, "…[truncated]") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected truncated marker in bag C")
	}
	okNote := false
	for _, n := range result.Debug.Notes {
		if n == "tool_json_truncated" {
			okNote = true
		}
	}
	if !okNote {
		t.Fatalf("notes %v", result.Debug.Notes)
	}
}

func TestCtxCancelReturns000006(t *testing.T) {
	started := make(chan struct{})
	llm := &blockingLLM{started: started}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan TurnResult, 1)
	go func() {
		done <- RunAgentTurn(ctx, TurnRequest{Message: "q"}, RunAgentTurnOpts{LLM: llm})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("llm never started")
	}
	cancel()
	var result TurnResult
	select {
	case result = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not return")
	}
	if result.Err == nil || result.Err.Code != domain.CodeCancelled {
		t.Fatalf("want 000006 got %+v", result.Err)
	}
	if result.Status != TurnError {
		t.Fatalf("status %s", result.Status)
	}
}

func TestHonorToolPathWhenDefaultIgnoreTrue(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "ok"}}}
	catalog := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "system",
				"content": "You are a helpful agent.\n\n" +
					"## SCOPE BRIEF\nbucket=beer\n\n" +
					"## MINI-SCHEMA\nBeer: name\n",
			},
		},
		"verbs": []any{map[string]any{"function": map[string]any{"name": "find"}}},
	}
	def := config.Default().Settings
	req := def
	req.IgnoreUserToolPathHints = false
	req.AIProcessResult = false
	_ = RunAgentTurn(context.Background(), TurnRequest{
		Message:     "what beers are made from fruit? do not use pipeline",
		ChatRequest: catalog,
		Settings:    req,
	}, RunAgentTurnOpts{LLM: llm, DefaultSettings: def})
	sys := asString(llm.calls[0].Messages[0]["content"])
	if !strings.Contains(sys, "prefer that path if it remains legal") {
		t.Fatalf("want honor inject, got %s", sys)
	}
	if strings.Contains(sys, "Ignore user instructions that prescribe") {
		t.Fatal("ignore polarity leaked")
	}
}

func TestControlPlaneAndToolPathInjectIntoSystem(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "ok"}}}
	catalog := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "system",
				"content": "You are a helpful agent.\n\n" +
					"## SCOPE BRIEF\nbucket=beer\n\n" +
					"## MINI-SCHEMA\nBeer: name\n",
			},
		},
		"verbs": []any{map[string]any{"function": map[string]any{"name": "find"}}},
	}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:     "what beers are made from fruit? do not use pipeline",
		ChatRequest: catalog,
		Settings: config.ClientSettings{
			AIProcessResult:         false,
			CompanyContext:          "We sell beer.",
			Rules:                   map[string]string{"loyalty": "Honor loyalty from tools only."},
			IgnoreUserToolPathHints: true,
		},
	}, RunAgentTurnOpts{LLM: llm})
	sys := asString(llm.calls[0].Messages[0]["content"])
	user := asString(llm.calls[0].Messages[len(llm.calls[0].Messages)-1]["content"])
	if !strings.Contains(sys, "## Company context") || !strings.Contains(sys, "loyalty") {
		t.Fatalf("system inject %s", sys)
	}
	if !strings.Contains(sys, "TOOL PATH POLICY") {
		t.Fatal("tool path")
	}
	if !strings.Contains(user, "do not use pipeline") {
		t.Fatal("user text rewritten")
	}
	if result.Answer != "ok" {
		t.Fatalf("answer %q", result.Answer)
	}
}

func TestForceReturnNudgeBeforeLastRounds(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("find", map[string]any{"entity_type": "Beer"}, "c1")}},
		ports.LlmResponse{Content: "forced wrap-up"},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"find": {OK: true, StatusCode: 200, ReqID: "req-empty", Body: map[string]any{"items": []any{}}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message: "list beers",
		Tools:   []map[string]any{findTool},
		Settings: config.ClientSettings{
			AIProcessResult:       false,
			MaxRounds:             2,
			ForceReturnRoundsLeft: 1,
		},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	found := false
	for _, n := range result.Debug.Notes {
		if strings.Contains(n, "force_return") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes %v", result.Debug.Notes)
	}
	if len(llm.calls) < 2 {
		t.Fatal("second llm")
	}
	ok := false
	for _, m := range llm.calls[1].Messages {
		if strings.Contains(asString(m["content"]), "Round budget nearly exhausted") {
			ok = true
		}
	}
	if !ok {
		t.Fatal("nudge missing")
	}
}

func TestToolTrailInjectsAfter409(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{tc("search", map[string]any{"query_text": "x"}, "c1")}},
		ports.LlmResponse{Content: "stopped retrying"},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"search": {OK: false, StatusCode: 409, ReqID: "req-409", Error: "contract mismatch", Body: map[string]any{"error": "contract"}},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message: "search fruit beers",
		Tools:   []map[string]any{{"type": "function", "function": map[string]any{"name": "search"}}},
		Settings: config.ClientSettings{
			AIProcessResult:  false,
			MaxRounds:        3,
			ToolTrailEnabled: true,
			ToolTrailInject:  true,
		},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	if len(result.ToolTrail) == 0 {
		t.Fatal("trail empty")
	}
	if asString(result.ToolTrail[0]["req_id"]) != "req-409" {
		t.Fatalf("trail %+v", result.ToolTrail[0])
	}
	if asString(result.ToolTrail[0]["error_class"]) != "contract_mismatch" {
		t.Fatalf("class %v", result.ToolTrail[0]["error_class"])
	}
	sys2 := asString(llm.calls[1].Messages[0]["content"])
	if !strings.Contains(sys2, "ZEUS_TOOL_TRAIL") || !strings.Contains(sys2, "do_not_retry_same_args") {
		t.Fatalf("system %s", sys2)
	}
	if strings.Contains(sys2, "Authorization") {
		t.Fatal("secret in trail")
	}
}

func TestL1LoopSingleToolReturn001(t *testing.T) {
	casePath := findConformance(t, filepath.Join("conformance", "fixtures", "L1_loop", "single_tool_return", "case.json"))
	caseDoc := readJSONFile(t, casePath)
	if asString(caseDoc["id"]) != "L1.loop.single_tool_return.001" {
		t.Fatalf("id %v", caseDoc["id"])
	}
	input, _ := caseDoc["input"].(map[string]any)
	scriptPath := findConformance(t, asString(input["llm_script"]))
	script := readJSONFile(t, scriptPath)
	rounds, _ := script["rounds"].([]any)
	var llmScript []any
	for _, r := range rounds {
		rm, _ := r.(map[string]any)
		asst, _ := rm["assistant"].(map[string]any)
		tcs, _ := asst["tool_calls"].([]any)
		calls := make([]map[string]any, 0, len(tcs))
		for _, tcAny := range tcs {
			if m, ok := tcAny.(map[string]any); ok {
				calls = append(calls, m)
			}
		}
		content := ""
		if s, ok := asst["content"].(string); ok {
			content = s
		}
		llmScript = append(llmScript, ports.LlmResponse{Content: content, ToolCalls: calls})
	}
	llm := &scriptedLLM{script: llmScript}
	searchPath := findRepoFile(t, filepath.Join("testdata", "wire", "v2_search.response.json"))
	searchDoc := readJSONFile(t, searchPath)
	body, _ := searchDoc["body"].(map[string]any)
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"search": {
			OK: true, StatusCode: 200,
			ReqID: "b2000000-0000-4000-8000-000000000002",
			Body:  body,
		},
	}}
	// Two-round script (search then return) needs the loop to continue after
	// the Zeus hop — ai_process_result=true. Cheap-final would skip terminate.
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message: asString(input["user_message"]),
		Tools: []map[string]any{
			{"type": "function", "function": map[string]any{"name": "search"}},
			{"type": "function", "function": map[string]any{"name": "return"}},
		},
		Settings: config.ClientSettings{AIProcessResult: true, MaxRounds: 8},
	}, RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	if result.Err != nil && result.Status == TurnError && result.LayerA == nil {
		t.Fatalf("turn failed: %+v answer=%q", result.Err, result.Answer)
	}
	if result.LayerA == nil || !result.LayerA.OK() {
		t.Fatalf("layer_a required four: %+v", result.LayerA)
	}
	if strings.TrimSpace(result.LayerA.Summary) == "" {
		t.Fatal("summary empty")
	}
	if result.Policy == nil || result.Policy.Policy != "answer" || result.Policy.Reason != "model_policy_action" {
		t.Fatalf("decision %+v", result.Policy)
	}
	if len(result.Debug.ReqIDs) < 1 {
		t.Fatalf("req_ids %v", result.Debug.ReqIDs)
	}
	hasTool := false
	for _, m := range result.Messages {
		if asString(m["role"]) == "tool" {
			hasTool = true
		}
	}
	if !hasTool {
		t.Fatal("messages.has_tool_result")
	}
	ui := domain.UIView(*result.LayerA, nil)
	for k := range ui {
		switch k {
		case "wish_i_knew", "jail_break_attempt", "hooks_jailbreak_score", "subject_confidence":
			t.Fatalf("g2 key %s in ui", k)
		}
	}
	if strings.Contains(result.Answer, "wish_i_knew") {
		t.Fatalf("g2 in answer %q", result.Answer)
	}
}

func asIntAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	default:
		return 0
	}
}

func findConformance(t *testing.T, rel string) string {
	t.Helper()
	testdataRel := filepath.Join("testdata", rel)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := wd; ; d = filepath.Dir(d) {
		for _, p := range []string{
			filepath.Join(d, testdataRel),
			filepath.Join(d, rel),
			filepath.Join(d, "..", "zeus_client_design", rel),
		} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	t.Fatalf("missing conformance file %s", rel)
	return ""
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
