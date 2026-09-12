// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func TestDebugBundleToMapIncludesNineGatherKeys(t *testing.T) {
	b := DebugBundle{
		TurnID:         "turn_abc",
		ChatID:         "chat_1",
		SessionID:      "sess_1",
		PreferredReqID: "req-a",
		ReqIDs:         []string{"req-a", "req-b"},
		ZeusURL:        "http://127.0.0.1:8080",
		ClientVersion:  "0.1.0",
		Target: map[string]any{
			"bucket": "yelp-data", "scope": "_default", "collection": "_default", "mode": "analytics",
		},
		Catalog:        map[string]any{"has_scope_brief": true, "has_mini_schema": true, "tools_count": 13},
		ContractStatus: "match",
		Tokens:         map[string]any{"prompt": 10, "completion": 4, "total": 14, "cached": 0, "extra": 0, "ok": true},
		ExportRef:      "turn_abc",
		Stamp:          map[string]any{"user": "zeus_client", "version": "0.1.0"},
		TraceID:        "abc",
	}
	d := b.ToMap()
	if d["turn_id"] != "turn_abc" || d["chat_id"] != "chat_1" || d["session_id"] != "sess_1" {
		t.Fatalf("ids %+v", d)
	}
	if d["preferred_req_id"] != "req-a" {
		t.Fatal("preferred")
	}
	if d["zeus_url"] != "http://127.0.0.1:8080" {
		t.Fatal("url")
	}
	if d["client_version"] != "0.1.0" {
		t.Fatal("version")
	}
	if d["target"].(map[string]any)["bucket"] != "yelp-data" {
		t.Fatal("target")
	}
	if d["catalog"].(map[string]any)["tools_count"] != 13 {
		t.Fatal("catalog")
	}
	if d["contract_status"] != "match" {
		t.Fatal("contract")
	}
	if d["tokens"].(map[string]any)["ok"] != true {
		t.Fatal("tokens")
	}
	if d["export_ref"] != "turn_abc" {
		t.Fatal("export_ref")
	}
	if d["stamp"].(map[string]any)["user"] != "zeus_client" {
		t.Fatal("stamp")
	}
	if d["trace_id"] != "abc" {
		t.Fatal("trace")
	}
	if _, ok := d["answer"]; ok {
		t.Fatal("answer must not be on gather surface")
	}
}

func TestDebugBundleDefaultsStillConstruct(t *testing.T) {
	b := DebugBundle{Rounds: 1}
	if len(b.ReqIDs) != 0 {
		t.Fatal("req_ids")
	}
	if b.ExportRef != "" {
		t.Fatal("export_ref")
	}
	if b.ToMap()["journal_schema"] != journal.JournalSchemaVersion {
		t.Fatalf("schema %v", b.ToMap()["journal_schema"])
	}
}

func TestFinishStampsChatAndEmptyReqIDsOnDirectExit(t *testing.T) {
	j := journal.NewInMemoryJournal(nil)
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "hello"}}}
	result := RunAgentTurn(context.Background(), TurnRequest{Message: "hi", ChatID: "chat_x"}, RunAgentTurnOpts{
		LLM: llm, Journal: j, ZeusURL: "http://127.0.0.1:8080", Version: "0.1.0",
	})
	if !domain.IsUUIDv4(result.Debug.TurnID) {
		t.Fatalf("turn_id %q", result.Debug.TurnID)
	}
	if result.Debug.ChatID != "chat_x" {
		t.Fatalf("chat %q", result.Debug.ChatID)
	}
	if result.Debug.SessionID != "" {
		t.Fatal("session")
	}
	if len(result.Debug.ReqIDs) != 0 || result.Debug.PreferredReqID != "" {
		t.Fatalf("req %v %q", result.Debug.ReqIDs, result.Debug.PreferredReqID)
	}
	if result.Debug.ZeusURL != "http://127.0.0.1:8080" {
		t.Fatal("url")
	}
	if result.Debug.ClientVersion != "0.1.0" {
		t.Fatalf("version %q", result.Debug.ClientVersion)
	}
	if result.Debug.ExportRef != result.Debug.TurnID {
		t.Fatal("export_ref")
	}
	if result.Debug.Tokens == nil {
		t.Fatal("tokens")
	}
	exp := ExportJournalRedacted(j, result.Debug.ExportRef, nil)
	found := false
	for _, e := range exp.Events {
		if e.Type == journal.EventTurnStarted {
			found = true
		}
	}
	if !found {
		t.Fatal("turn.started missing")
	}
}

func TestFinishStampsReqIDsFromFindHop(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{
		ToolCalls: []map[string]any{
			tc("find", map[string]any{"entity_type": "Airport"}, "c1"),
			tc("return", map[string]any{
				"summary": "20 US airports were returned.",
				"query_decomposition": map[string]any{
					"intent": "List", "entity": "Airport", "facets": map[string]any{"country": "United States"},
				},
				"decomposition": map[string]any{
					"targets":    []any{map[string]any{"entity_type": "Airport"}},
					"predicates": map[string]any{"country": "United States"},
					"output":     "rows",
				},
				"confidence":    "high",
				"policy_action": "answer",
			}, "c2"),
		},
	}}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"find": {
			OK: true, StatusCode: 200, ReqID: "req-find-airports",
			Body: map[string]any{
				"result": map[string]any{"items": []any{map[string]any{"name": "Sleetmute Airport"}}, "returned_count": 1},
				"meta":   map[string]any{"step_costs": []any{map[string]any{"verb": "find", "result_size": 1}}},
			},
			URL: "http://z/v2/b/s/c/find",
		},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:  "airports",
		ChatID:   "chat_airports",
		Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}},
		Settings: config.ClientSettings{AIProcessResult: false},
	}, RunAgentTurnOpts{
		LLM: llm, Zeus: zeus,
		DebugPolicy: config.DebugPolicy{DetectiveBriefing: true},
		Env:         map[string]string{"ZEUS_CLIENT_DETECTIVE": "1"},
	})
	if len(result.Debug.ReqIDs) != 1 || result.Debug.ReqIDs[0] != "req-find-airports" {
		t.Fatalf("req_ids %v", result.Debug.ReqIDs)
	}
	if result.Debug.PreferredReqID != "req-find-airports" {
		t.Fatalf("pref %q", result.Debug.PreferredReqID)
	}
	if len(result.Debug.Hops) == 0 {
		t.Fatal("hops")
	}
	hop := result.Debug.Hops[0]
	if !strings.HasSuffix(asString(hop["url"]), "/find") {
		t.Fatalf("url %v", hop["url"])
	}
	if hop["result_size"] != 1 && hop["result_size"] != 1.0 {
		t.Fatalf("result_size %v", hop["result_size"])
	}
	la, _ := result.Debug.PublicTrace["layer_a"].(map[string]any)
	qd, _ := la["query_decomposition"].(map[string]any)
	if qd["entity"] != "Airport" {
		t.Fatalf("layer_a %+v", la)
	}
	if la["via"] != "return" {
		t.Fatalf("via %v", la["via"])
	}
	if _, ok := la["wish_i_knew"]; ok {
		t.Fatal("g2 in compact layer_a")
	}
	if strings.Contains(result.Answer, "wish_i_knew") {
		t.Fatal("g2 in answer")
	}
	if result.Debug.Detective == nil {
		t.Fatal("detective")
	}
	md := result.Debug.Detective["diagnosis"].(map[string]any)["support_pack"].(map[string]any)["markdown"].(string)
	if !strings.Contains(md, "## 1. Ids") || !strings.Contains(md, "## 9. Journal export") {
		t.Fatalf("support pack:\n%s", md)
	}
}

func TestAgentTurnAttachesDetectiveSoft(t *testing.T) {
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{
			tc("return", map[string]any{
				"summary":             "Done.",
				"query_decomposition": map[string]any{"intent": "x"},
				"decomposition":       map[string]any{"targets": []any{}},
				"confidence":          "high",
				"policy_action":       "answer",
			}, "c1"),
		}},
		ports.LlmResponse{Content: "Done."},
	}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:      "hi",
		SystemPrompt: "You are helpful.\n\n## SCOPE BRIEF\nYelp businesses in Tampa.\n\n## MINI-SCHEMA\nBusiness: name, city\n",
		Tools:        []map[string]any{{"type": "function", "function": map[string]any{"name": "return"}}},
		Settings:     config.ClientSettings{AIProcessResult: true, MaxRounds: 3},
	}, RunAgentTurnOpts{
		LLM:         llm,
		Zeus:        &scriptedZeus{},
		DebugPolicy: config.DebugPolicy{DetectiveBriefing: true, HubBaseURL: "http://hub:9091"},
		Env:         map[string]string{"ZEUS_CLIENT_DETECTIVE": "1"},
	})
	if result.Debug.Detective == nil {
		t.Fatal("detective nil")
	}
	if result.Debug.Detective["version"] != 1 {
		t.Fatalf("version %v", result.Debug.Detective["version"])
	}
	if strings.Contains(result.Answer, "wish_i_knew") {
		t.Fatal("g2")
	}
	ptDet, _ := result.Debug.PublicTrace["detective"].(map[string]any)
	if ptDet["version"] != 1 {
		t.Fatalf("public_trace.detective %+v", result.Debug.PublicTrace["detective"])
	}
}

func TestProductDefaultConfigSkipsDetective(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "plain"}}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:      "hi",
		SystemPrompt: "You are helpful.\n\n## SCOPE BRIEF\nYelp.\n\n## MINI-SCHEMA\nBusiness\n",
	}, RunAgentTurnOpts{
		LLM:         llm,
		DebugPolicy: config.Default().Debug,
		StampUser:   "zeus_client",
		Env:         map[string]string{},
	})
	if result.Debug.Detective != nil {
		t.Fatal("product default must not build detective")
	}
	if _, ok := result.Debug.PublicTrace["detective"]; ok {
		t.Fatal("public_trace.detective")
	}
	if result.Answer != "plain" {
		t.Fatalf("answer %q", result.Answer)
	}
}

func TestHubAdminStampGathersDetective(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "plain"}}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:      "hi",
		SystemPrompt: "You are helpful.\n\n## SCOPE BRIEF\nYelp.\n\n## MINI-SCHEMA\nBusiness\n",
	}, RunAgentTurnOpts{
		LLM:       llm,
		StampUser: "admin",
		Env:       map[string]string{},
	})
	if result.Debug.Detective == nil {
		t.Fatal("StampUser=admin must gather detective")
	}
	if result.Debug.Detective["version"] != 1 {
		t.Fatalf("version %v", result.Debug.Detective["version"])
	}
	ptDet, _ := result.Debug.PublicTrace["detective"].(map[string]any)
	if ptDet["version"] != 1 {
		t.Fatalf("public_trace.detective %+v", result.Debug.PublicTrace["detective"])
	}
}

func TestAgentTurnKillSwitchSkipsDetective(t *testing.T) {
	llm := &scriptedLLM{script: []any{ports.LlmResponse{Content: "plain"}}}
	result := RunAgentTurn(context.Background(), TurnRequest{
		Message:      "hi",
		SystemPrompt: "You are helpful.\n\n## SCOPE BRIEF\nYelp.\n\n## MINI-SCHEMA\nBusiness\n",
	}, RunAgentTurnOpts{
		LLM:         llm,
		DebugPolicy: config.DebugPolicy{DetectiveBriefing: true},
		Env:         map[string]string{"ZEUS_CLIENT_DETECTIVE": "0"},
	})
	if result.Debug.Detective != nil {
		t.Fatal("kill-switch should skip")
	}
	if result.Answer != "plain" {
		t.Fatalf("answer %q", result.Answer)
	}
}

func TestToolsFromRequestFallsBackToChatRequestVerbs(t *testing.T) {
	req := TurnRequest{
		Message:     "x",
		ChatRequest: map[string]any{"verbs": []any{map[string]any{"type": "function", "function": map[string]any{"name": "find"}}}},
	}
	tools := toolsFromRequest(req)
	if len(tools) != 1 {
		t.Fatalf("len %d", len(tools))
	}
	fn, _ := tools[0]["function"].(map[string]any)
	if fn["name"] != "find" {
		t.Fatalf("%+v", tools[0])
	}
}
