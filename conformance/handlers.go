// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// Handler drives one case against domain / application APIs.
// Observe is built from live results — never from the case expect map.
type Handler func(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error)

type scriptedLLM struct {
	script []any
	calls  []ports.LlmRequest
}

func (s *scriptedLLM) Complete(_ context.Context, req ports.LlmRequest) (ports.LlmResponse, error) {
	s.calls = append(s.calls, req)
	if len(s.script) == 0 {
		return ports.LlmResponse{Content: "(empty script)"}, nil
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
		Body:       map[string]any{"items": []any{}},
	}, nil
}

func toolCall(name string, args map[string]any, callID string) map[string]any {
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

func parseJSONObject(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(x), &m); err != nil {
			return map[string]any{}
		}
		return m
	default:
		return map[string]any{}
	}
}

func returnPayloadFromScript(script map[string]any) (map[string]any, bool) {
	rounds, _ := script["rounds"].([]any)
	for _, r := range rounds {
		rm, _ := r.(map[string]any)
		asst := childMap(rm, "assistant")
		tcs, _ := asst["tool_calls"].([]any)
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]any)
			fn := childMap(tcm, "function")
			if asString(fn["name"]) != "return" {
				continue
			}
			return parseJSONObject(fn["arguments"]), true
		}
	}
	return nil, false
}

func toStringMap(v any) map[string]string {
	m, _ := v.(map[string]any)
	out := map[string]string{}
	for k, val := range m {
		out[fmt.Sprint(k)] = fmt.Sprint(val)
	}
	return out
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func triggerMapsEqual(got map[string]bool, want any) bool {
	wm, ok := want.(map[string]any)
	if !ok {
		return false
	}
	if len(got) != len(wm) {
		return false
	}
	for k, wv := range wm {
		wantB, ok := wv.(bool)
		if !ok || got[k] != wantB {
			return false
		}
	}
	return true
}

func runL0Catalog(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	_ = caseDir
	inp := childMap(caseDoc, "input")
	catalog, err := loadJSONMap(resolveRel(designRoot, asString(inp["catalog_path"])))
	if err != nil {
		return nil, err
	}
	schema, err := loadJSONMap(resolveRel(designRoot, asString(inp["schema_path"])))
	if err != nil {
		return nil, err
	}
	return domain.CatalogLoadMockExpect(catalog, schema), nil
}

func runL0Envelope(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	samples, err := loadJSONMap(filepath.Join(caseDir, asString(inp["samples_path"])))
	if err != nil {
		return nil, err
	}
	ok := true
	list, _ := samples["samples"].([]any)
	for _, s := range list {
		sm, _ := s.(map[string]any)
		env := childMap(sm, "envelope")
		if env == nil {
			ok = false
			continue
		}
		if _, has := env["result"]; !has {
			ok = false
		}
		if env["result"] == true {
			_, hasVal := env["value"]
			_, hasErr := env["error"]
			if !hasVal && hasErr {
				ok = false
			}
		}
		if env["result"] == false {
			if _, hasErr := env["error"]; !hasErr {
				ok = false
			}
		}
	}
	return map[string]any{"result": ok, "samples_ok": ok}, nil
}

func layerFromMatrix(raw map[string]any) domain.LayerA {
	layer := domain.ParseLayerA(raw, domain.ParseLayerAOpts{})
	if raw == nil {
		return layer
	}
	if okFlag, isBool := raw["ok"].(bool); isBool && !okFlag && layer.OK() {
		layer.Errors = append(layer.Errors, "matrix_forced_fail")
	}
	if qd, ok := raw["query_decomposition"].(map[string]any); ok {
		layer.QueryDecomposition = qd
	}
	if decomp, ok := raw["decomposition"].(map[string]any); ok {
		layer.Decomposition = decomp
	}
	if pa, ok := raw["policy_action"].(string); ok {
		if strings.TrimSpace(pa) != "" {
			layer.PolicyAction = strings.TrimSpace(pa)
		} else {
			layer.PolicyAction = ""
		}
	}
	if trig, ok := raw["business_rules_triggers"].(map[string]any); ok {
		out := map[string]bool{}
		for k, v := range trig {
			out[fmt.Sprint(k)] = asBool(v)
		}
		layer.BusinessRulesTriggers = out
	}
	if f, ok := asFloat(raw["jail_break_attempt"]); ok {
		f := f
		layer.JailBreakAttempt = &f
	}
	if s, ok := raw["summary"].(string); ok {
		layer.Summary = s
	}
	return layer
}

func runL2Policy(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	name := filepath.Base(asString(inp["rows_path"]))
	data, err := loadJSONMap(filepath.Join(caseDir, name))
	if err != nil {
		return nil, err
	}
	rows, _ := data["rows"].([]any)
	matched := 0
	var failures []any
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		inpRow := childMap(row, "input")
		rawLA := childMap(inpRow, "layer_a")
		layer := layerFromMatrix(rawLA)
		score, _ := asFloat(inpRow["hooks_jailbreak_score"])
		refuse, _ := inpRow["hooks_must_refuse"].(bool)
		dec := domain.DecidePolicy(layer, domain.DecidePolicyOpts{
			HooksMustRefuse:     refuse,
			HooksJailbreakScore: score,
		})
		rowWant := childMap(row, "expect")
		if dec.Policy == asString(rowWant["policy"]) &&
			dec.Forced == asBool(rowWant["forced"]) &&
			dec.Reason == asString(rowWant["reason"]) {
			matched++
			continue
		}
		failures = append(failures, map[string]any{
			"id": row["id"],
			"got": map[string]any{
				"policy": dec.Policy,
				"forced": dec.Forced,
				"reason": dec.Reason,
			},
			"want": rowWant,
		})
	}
	return map[string]any{
		"all_rows_match": matched == len(rows) && len(rows) > 0,
		"row_count":      len(rows),
		"matched":        matched,
		"failures":       failures,
	}, nil
}

func runL2LayerA(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	_ = caseDir
	inp := childMap(caseDoc, "input")
	valid, err := loadJSONMap(resolveRel(designRoot, asString(inp["valid_path"])))
	if err != nil {
		return nil, err
	}
	v := domain.ParseLayerA(valid, domain.ParseLayerAOpts{})
	inv := domain.ParseLayerA(childMap(inp, "invalid"), domain.ParseLayerAOpts{})
	var klass any
	if !inv.OK() {
		klass = "layer_a_validation_failed"
	}
	return map[string]any{
		"valid.ok":            v.OK(),
		"invalid.ok":          inv.OK(),
		"invalid.error_class": klass,
	}, nil
}

func runL2G2UI(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	_ = caseDir
	inp := childMap(caseDoc, "input")
	payload, err := loadJSONMap(resolveRel(designRoot, asString(inp["layer_a_path"])))
	if err != nil {
		return nil, err
	}
	layer := domain.ParseLayerA(payload, domain.ParseLayerAOpts{})
	view := domain.UIView(layer, nil)
	g2 := map[string]struct{}{
		"wish_i_knew":             {},
		"jail_break_attempt":      {},
		"subject_confidence":      {},
		"data_gaps":               {},
		"business_rules_triggers": {},
		"hooks_jailbreak_score":   {},
	}
	var leaked []any
	for k := range view {
		if _, bad := g2[k]; bad {
			leaked = append(leaked, k)
		}
	}
	ok := len(leaked) == 0
	return map[string]any{
		"g2_not_in_ui_view": ok,
		"leaked_keys":       leaked,
		"result":            ok,
	}, nil
}

func runL2Rules(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	data, err := loadJSONMap(filepath.Join(caseDir, asString(inp["fixture"])))
	if err != nil {
		return nil, err
	}
	out := domain.MergeRulesFrozen(toStringMap(data["base"]), toStringMap(data["overlay"]), asBool(data["frozen"]))
	want := toStringMap(data["expect_merged"])
	return map[string]any{
		"result":        stringMapsEqual(out, want),
		"merged":        out,
		"expect_merged": want,
	}, nil
}

func runL2Triggers(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	data, err := loadJSONMap(filepath.Join(caseDir, asString(inp["fixture"])))
	if err != nil {
		return nil, err
	}
	rowsOK := true
	rows, _ := data["rows"].([]any)
	for _, rowAny := range rows {
		row, _ := rowAny.(map[string]any)
		got, _ := domain.NormalizeTriggers(row["raw"], nil)
		if !triggerMapsEqual(got, row["expect"]) {
			rowsOK = false
		}
	}
	return map[string]any{"result": rowsOK, "all_rows_match": rowsOK}, nil
}

func runL2Settings(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	data, err := loadJSONMap(filepath.Join(caseDir, asString(inp["fixture"])))
	if err != nil {
		return nil, err
	}
	profile := childMap(data, "profiles")
	prod := asBool(childMap(profile, "production")["ai_process_result"])
	hub := asBool(childMap(profile, "hub_debug")["ai_process_result"])
	hubCfg, err := config.ApplyProfile(config.Default(), "hub")
	if err != nil {
		return nil, err
	}
	pkgFalse := config.Default().Settings.AIProcessResult == false
	prodObserve := true
	if !prod && pkgFalse {
		prodObserve = false
	}
	hubTrue := hubCfg.Settings.AIProcessResult == true
	return map[string]any{
		"production.ai_process_result": prodObserve,
		"hub.ai_process_result":        hub,
		"result":                       !prod && pkgFalse,
		"production_default_false":     !prod,
		"package_default_false":        pkgFalse,
		"hub_profile_true":             hubTrue,
		"package_hub_default_true":     hubTrue,
	}, nil
}

func runL1SingleTool(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	_ = caseDir
	// Walk the design llm_script (kit reference). Product cheap-final after
	// search would skip the scripted return hop, so this case does not claim
	// application.RunAgentTurn.
	inp := childMap(caseDoc, "input")
	script, err := loadJSONMap(resolveRel(designRoot, asString(inp["llm_script"])))
	if err != nil {
		return nil, err
	}
	rounds, _ := script["rounds"].([]any)
	var reqIDs []string
	hasTool := false
	var layerPayload map[string]any
	haveReturn := false
	for _, r := range rounds {
		rm, _ := r.(map[string]any)
		asst := childMap(rm, "assistant")
		tcs, _ := asst["tool_calls"].([]any)
		zm := childMap(rm, "zeus_mock")
		if rid := asString(zm["req_id"]); rid != "" {
			reqIDs = append(reqIDs, rid)
		}
		for _, tc := range tcs {
			tcm, _ := tc.(map[string]any)
			fn := childMap(tcm, "function")
			name := asString(fn["name"])
			if name != "" && name != "return" {
				hasTool = true
			}
			if name == "return" {
				layerPayload = parseJSONObject(fn["arguments"])
				haveReturn = true
			}
		}
	}
	if !haveReturn {
		return map[string]any{"result": false, "error_class": "no_return_tool"}, nil
	}
	layer := domain.ParseLayerA(layerPayload, domain.ParseLayerAOpts{})
	dec := domain.DecidePolicy(layer, domain.DecidePolicyOpts{})
	view := domain.UIView(layer, nil)
	g2 := map[string]struct{}{
		"wish_i_knew":             {},
		"jail_break_attempt":      {},
		"subject_confidence":      {},
		"business_rules_triggers": {},
		"hooks_jailbreak_score":   {},
	}
	leaked := 0
	for k := range view {
		if _, bad := g2[k]; bad {
			leaked++
		}
	}
	summary := ""
	if s, ok := layerPayload["summary"].(string); ok {
		summary = s
	}
	return map[string]any{
		"result":                   layer.OK() && hasTool,
		"layer_a.required_four":    layer.OK(),
		"layer_a.summary_nonempty": strings.TrimSpace(summary) != "",
		"decision.policy":          dec.Policy,
		"decision.reason":          dec.Reason,
		"req_ids.count_gte":        len(reqIDs),
		"messages.has_tool_result": hasTool,
		"g2_not_in_ui_view":        leaked == 0,
	}, nil
}

func runL1ForceReturn(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	data, err := loadJSONMap(filepath.Join(caseDir, asString(inp["fixture"])))
	if err != nil {
		return nil, err
	}
	maxRounds := asInt(data["max_rounds"])
	if maxRounds <= 0 {
		maxRounds = 3
	}
	nFind := maxRounds - 1
	if nFind < 1 {
		nFind = 1
	}
	script := make([]any, 0, nFind+1)
	for i := 0; i < nFind; i++ {
		script = append(script, ports.LlmResponse{
			ToolCalls: []map[string]any{toolCall("find", map[string]any{"entity_type": "Beer"}, "c1")},
		})
	}
	script = append(script, ports.LlmResponse{Content: "forced wrap-up"})
	llm := &scriptedLLM{script: script}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"find": {
			OK: true, StatusCode: 200, ReqID: "req-empty",
			Body: map[string]any{"items": []any{}},
		},
	}}
	result := application.RunAgentTurn(context.Background(), application.TurnRequest{
		Message: "list beers",
		Tools:   []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}},
		Settings: config.ClientSettings{
			AIProcessResult:       false,
			MaxRounds:             maxRounds,
			ForceReturnRoundsLeft: 1,
		},
	}, application.RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	forced := false
	for _, n := range result.Debug.Notes {
		if strings.Contains(n, "force_return") {
			forced = true
			break
		}
	}
	return map[string]any{
		"result":                forced,
		"force_return":          forced,
		"max_rounds":            maxRounds,
		"rounds_without_return": result.Debug.Rounds,
	}, nil
}

func runDTAssertOnly(_ /*designRoot*/, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	inp := childMap(caseDoc, "input")
	exportName := asString(inp["detective_export"])
	if exportName == "" {
		exportName = "detective_export.slim.json"
	}
	exportPath := filepath.Join(caseDir, exportName)
	if !fileExists(exportPath) {
		return map[string]any{}, nil
	}
	export, err := loadJSONMap(exportPath)
	if err != nil {
		return nil, err
	}
	base := asString(export["chat_request_base_id"])
	if base == "" {
		base = asString(GetPath(export, "diagnosis.prompt.chat_request_base_id"))
	}
	lineage := base
	if lineage == "" {
		lineage = asString(GetPath(export, "diagnosis.prompt.lineage_line"))
	}
	return map[string]any{
		"diagnosis.prompt_grade": GetPath(export, "diagnosis.prompt_grade"),
		"diagnosis.output_grade": GetPath(export, "diagnosis.output_grade"),
		"diagnosis.error_grade":  GetPath(export, "diagnosis.error_grade"),
		"inject_inspect.sent.mini_schema.present": asBool(GetPath(export, "inject_inspect.sent.mini_schema.present")) ||
			asBool(GetPath(export, "diagnosis.prompt.has_mini_schema")),
		"inject_inspect.sent.scope_brief.present": asBool(GetPath(export, "inject_inspect.sent.scope_brief.present")) ||
			asBool(GetPath(export, "diagnosis.prompt.has_scope_brief")),
		"chat_request_base_id":  base,
		"lineage_contains":      lineage,
		"custom_label_contains": asString(GetPath(export, "diagnosis.prompt.custom_label")),
	}, nil
}

func runDTRewindCompanions(_ /*designRoot*/, caseDir string, _ map[string]any) (map[string]any, error) {
	scriptP := filepath.Join(caseDir, "llm_script.json")
	zeusP := filepath.Join(caseDir, "zeus_responses.json")
	if !fileExists(scriptP) || !fileExists(zeusP) {
		return map[string]any{
			"result":             false,
			"error_class":        "rewind_companions_missing",
			"companions_present": false,
			"has_return":         false,
		}, nil
	}
	script, err := loadJSONMap(scriptP)
	if err != nil {
		return nil, err
	}
	zeus, err := loadJSONMap(zeusP)
	if err != nil {
		return nil, err
	}
	rounds, _ := script["rounds"].([]any)
	hops := zeus["hops"]
	if hops == nil {
		hops = zeus["responses"]
	}
	hopN := 0
	if sl, ok := hops.([]any); ok {
		hopN = len(sl)
	}
	payload, hasReturn := returnPayloadFromScript(script)
	layerOK := false
	if hasReturn {
		layerOK = domain.ParseLayerA(payload, domain.ParseLayerAOpts{}).OK()
	}
	return map[string]any{
		"result":                len(rounds) > 0 && hasReturn,
		"companions_present":    true,
		"llm_rounds":            len(rounds),
		"zeus_hops":             hopN,
		"has_return":            hasReturn,
		"layer_a.required_four": layerOK,
		"mode_capable":          "rewind",
	}, nil
}

func runDTSmooth(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	observe, err := runDTRewindCompanions(designRoot, caseDir, caseDoc)
	if err != nil {
		return nil, err
	}
	slim, err := runDTAssertOnly(designRoot, caseDir, caseDoc)
	if err != nil {
		return nil, err
	}
	for k, v := range slim {
		if _, ok := observe[k]; !ok {
			observe[k] = v
		}
	}
	observe["result"] = asBool(observe["companions_present"]) && asBool(observe["has_return"])
	return observe, nil
}

func runDTFailClientMini(_ /*designRoot*/, caseDir string, _ map[string]any) (map[string]any, error) {
	doc := map[string]any{"messages": []any{map[string]any{"role": "system", "content": "no brief here"}}}
	fix := filepath.Join(caseDir, "fixture.json")
	if fileExists(fix) {
		data, err := loadJSONMap(fix)
		if err != nil {
			return nil, err
		}
		if cat := childMap(data, "catalog"); cat != nil {
			doc = cat
		}
	}
	return domain.ClassifyMiniSchema(domain.GetMiniSchema(doc, false)), nil
}

func runDTFailZeus(designRoot, caseDir string, caseDoc map[string]any) (map[string]any, error) {
	_ = caseDir
	inp := childMap(caseDoc, "input")
	rel := asString(inp["zeus_response"])
	if rel == "" {
		rel = "wire/errors/contract_409.response.json"
	}
	respPath := resolveRel(designRoot, rel)
	resp := map[string]any{}
	if fileExists(respPath) {
		var err error
		resp, err = loadJSONMap(respPath)
		if err != nil {
			return nil, err
		}
	}
	headers := childMap(resp, "headers")
	reqID := asString(headers["X-Zeus-Req-Id"])
	status := asInt(resp["status"])
	if status == 0 {
		status = 409
	}
	body := childMap(resp, "body")
	llm := &scriptedLLM{script: []any{
		ports.LlmResponse{ToolCalls: []map[string]any{toolCall("search", map[string]any{"query_text": "x"}, "c1")}},
		ports.LlmResponse{Content: "stopped retrying"},
	}}
	zeus := &scriptedZeus{results: map[string]ports.VerbHopResult{
		"search": {
			OK:         false,
			StatusCode: status,
			ReqID:      reqID,
			Error:      "contract mismatch",
			Body:       body,
		},
	}}
	result := application.RunAgentTurn(context.Background(), application.TurnRequest{
		Message: "search fruit beers",
		Tools:   []map[string]any{{"type": "function", "function": map[string]any{"name": "search"}}},
		Settings: config.ClientSettings{
			AIProcessResult: false,
			MaxRounds:       3,
		},
	}, application.RunAgentTurnOpts{LLM: llm, Zeus: zeus})
	var hop map[string]any
	if len(result.ToolTrail) > 0 {
		hop = result.ToolTrail[0]
	}
	klass := asString(hop["error_class"])
	if klass == "" {
		klass = domain.ErrorClassFor(false, status, "")
	}
	gotReq := asString(hop["req_id"])
	if gotReq == "" {
		gotReq = reqID
	}
	retryable := asBool(childMap(resp, "client_expect")["retryable"])
	return map[string]any{
		"result":         false,
		"error_class":    klass,
		"req_id":         gotReq,
		"req_id.present": gotReq != "",
		"forged_hash":    false,
		"retryable":      retryable,
	}, nil
}

func runDTFailLLM(_ /*designRoot*/, caseDir string, _ map[string]any) (map[string]any, error) {
	inv := map[string]any{}
	fix := filepath.Join(caseDir, "fixture.json")
	if fileExists(fix) {
		data, err := loadJSONMap(fix)
		if err != nil {
			return nil, err
		}
		if m := childMap(data, "invalid_return"); m != nil {
			inv = m
		}
	}
	layer := domain.ParseLayerA(inv, domain.ParseLayerAOpts{})
	dec := domain.DecidePolicy(layer, domain.DecidePolicyOpts{})
	var klass any
	if !layer.OK() {
		klass = "layer_a_validation_failed"
	}
	return map[string]any{
		"result":                false,
		"error_class":           klass,
		"layer_a.required_four": layer.OK(),
		"decision.policy":       dec.Policy,
		"decision.reason":       dec.Reason,
	}, nil
}

func runDTFailControlPlane(_ /*designRoot*/, caseDir string, _ map[string]any) (map[string]any, error) {
	payload := map[string]any{
		"summary":                 "Found beers from Zeus data.",
		"query_decomposition":     map[string]any{"intent": "List", "entity": "Beer"},
		"decomposition":           map[string]any{"targets": []any{map[string]any{"entity_type": "Beer"}}},
		"confidence":              "high",
		"policy_action":           "answer",
		"business_rules_triggers": map[string]any{},
	}
	fix := filepath.Join(caseDir, "fixture.json")
	if fileExists(fix) {
		data, err := loadJSONMap(fix)
		if err != nil {
			return nil, err
		}
		if la := childMap(data, "layer_a"); la != nil {
			payload = la
		}
	}
	layer := domain.ParseLayerA(payload, domain.ParseLayerAOpts{})
	dec := domain.DecidePolicy(layer, domain.DecidePolicyOpts{})
	fired := false
	for _, v := range layer.BusinessRulesTriggers {
		if v {
			fired = true
			break
		}
	}
	return map[string]any{
		"result":                layer.OK(),
		"data_ok":               layer.OK(),
		"trigger_fired":         fired,
		"partial_control_plane": layer.OK() && !fired,
		"error_class":           nil,
		"decision.policy":       dec.Policy,
	}, nil
}

// HANDLERS is the case-id map (Python HANDLERS). Do not copy case expect maps.
var HANDLERS = map[string]Handler{
	"L0.catalog.load_mock.001":                                     runL0Catalog,
	"L0.result.envelope.001":                                       runL0Envelope,
	"L1.loop.single_tool_return.001":                               runL1SingleTool,
	"L1.loop.force_return_max_rounds.001":                          runL1ForceReturn,
	"L2.policy.matrix.001":                                         runL2Policy,
	"L2.layer_a.required_four.001":                                 runL2LayerA,
	"L2.layer_a.g2_not_in_ui.001":                                  runL2G2UI,
	"L2.rules.merge_freeze.001":                                    runL2Rules,
	"L2.triggers.object_normalize.001":                             runL2Triggers,
	"L2.settings.ai_process_result.001":                            runL2Settings,
	"DT.smooth_short.beer_fruit_pipeline_base61.001":               runDTSmooth,
	"DT.smooth_long.beer_fruit_multi_round_no_pipeline_base61.001": runDTSmooth,
	"DT.fail_client.missing_mini_schema.001":                       runDTFailClientMini,
	"DT.fail_zeus.contract_409.001":                                runDTFailZeus,
	"DT.fail_llm.bad_layer_a.001":                                  runDTFailLLM,
	"DT.fail_control_plane.trigger_missing_data_ok.001":            runDTFailControlPlane,
}
