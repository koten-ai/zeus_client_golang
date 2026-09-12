// SPDX-License-Identifier: BUSL-1.1

package detective

import (
	"strings"
	"testing"
)

const systemWithInject = "You are helpful.\n\n## SCOPE BRIEF\nYelp businesses in Tampa.\n\n## MINI-SCHEMA\nBusiness: name, city\n"

func TestSchemaV1KeysAndPlaybookIDs(t *testing.T) {
	brief := Build(BriefingArgs{
		TurnID: "t1",
		Answer: "Found two salons.",
		Status: "ok",
		Rounds: 1,
		Hops: []map[string]any{
			{"req_id": "req-a", "name": "find", "status": 200, "ok": true, "snippet": `{"result":{"items":[{"id":"1"}]}}`},
		},
		Messages:   []map[string]any{{"role": "system", "content": systemWithInject}},
		Tools:      []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}},
		HubBaseURL: "http://127.0.0.1:9091",
		SessionID:  "sess-1",
		Target: map[string]any{
			"bucket": "yelp-data", "scope": "_default", "collection": "_default", "mode": "analytics",
		},
	})
	if brief["version"] != 1 || brief["source"] != "client" || brief["hub_hydrated"] != false {
		t.Fatalf("header %+v", brief)
	}
	for _, k := range []string{"version", "source", "hub_hydrated", "overview", "prompt", "diagnosis"} {
		if _, ok := brief[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	prompt := brief["prompt"].(map[string]any)
	if prompt["verdict"] != "pass" {
		t.Fatalf("verdict %v", prompt["verdict"])
	}
	inj := prompt["inject"].(map[string]any)
	if inj["has_scope_brief"] != true || inj["has_mini_schema"] != true {
		t.Fatalf("inject %+v", inj)
	}
	ov := brief["overview"].(map[string]any)
	if ov["preferred_req_id"] != "req-a" {
		t.Fatalf("pref %v", ov["preferred_req_id"])
	}
	links := ov["hub_links"].(map[string]string)
	if _, ok := links["req"]; !ok {
		t.Fatal("hub req link")
	}
	if _, ok := links["session"]; !ok {
		t.Fatal("hub session link")
	}
	diag := brief["diagnosis"].(map[string]any)
	if diag["prompt_grade"] != "pass" {
		t.Fatalf("prompt_grade %v", diag["prompt_grade"])
	}
	sp := diag["support_pack"].(map[string]any)
	md, _ := sp["markdown"].(string)
	if !strings.Contains(md, "## 1. Ids") || !strings.Contains(md, "## 9. Journal export") {
		t.Fatalf("support pack markdown:\n%s", md)
	}
	have := map[string]struct{}{}
	for _, id := range PlaybookIDs {
		have[id] = struct{}{}
	}
	for _, id := range []string{"boundary_collections", "missing_inject", "tool_errors", "hollow_answer", "contract_drift"} {
		if _, ok := have[id]; !ok {
			t.Errorf("missing playbook id %s", id)
		}
	}
}

func TestOverviewTokensFromPublicTrace(t *testing.T) {
	brief := Build(BriefingArgs{
		TurnID: "t-tok",
		Answer: "ok",
		Status: "ok",
		Rounds: 2,
		PublicTrace: map[string]any{
			"steps": []any{
				map[string]any{"type": "llm", "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}},
				map[string]any{"type": "force_final", "usage": map[string]any{"prompt_tokens": 40, "completion_tokens": 12, "total_tokens": 52}},
			},
			"tokens": map[string]any{"prompt": 140, "completion": 32, "total": 172, "cached": 0, "extra": 0, "ok": true},
		},
		Messages: []map[string]any{{"role": "system", "content": systemWithInject}},
	})
	tok := brief["overview"].(map[string]any)["tokens"].(map[string]any)
	if tok["prompt"] != 140 || tok["completion"] != 32 || tok["total"] != 172 || tok["ok"] != true {
		t.Fatalf("tokens %+v", tok)
	}
}

func TestMissingInjectPlaybook(t *testing.T) {
	brief := Build(BriefingArgs{
		Answer:   "hi",
		Messages: []map[string]any{{"role": "system", "content": "No markers here."}},
		Tools:    []map[string]any{},
	})
	if brief["prompt"].(map[string]any)["verdict"] != "fail" {
		t.Fatalf("verdict %v", brief["prompt"].(map[string]any)["verdict"])
	}
	ids := playbookIDSet(brief)
	if _, ok := ids["missing_inject"]; !ok {
		t.Fatalf("playbooks %v", ids)
	}
}

func TestBoundaryCollectionsPlaybook(t *testing.T) {
	brief := Build(BriefingArgs{
		Answer: "",
		Hops: []map[string]any{
			{"req_id": "r1", "name": "find", "status": 400, "ok": false, "snippet": `unknown boundary: "collections"`},
		},
		Messages: []map[string]any{{"role": "system", "content": systemWithInject}},
		Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}},
	})
	ids := playbookIDSet(brief)
	if _, ok := ids["boundary_collections"]; !ok {
		t.Fatalf("playbooks %v", ids)
	}
}

func TestG2NotInOverviewOrSupportPack(t *testing.T) {
	brief := Build(BriefingArgs{
		Answer: "Clean answer only.",
		LayerA: map[string]any{
			"summary":            "Clean answer only.",
			"wish_i_knew":        []any{map[string]any{"gap": "secret"}},
			"jail_break_attempt": 0.9,
		},
		Messages: []map[string]any{{"role": "system", "content": systemWithInject}},
	})
	ov := brief["overview"].(map[string]any)
	prev, _ := ov["answer_preview"].(string)
	if strings.Contains(prev, "secret") || strings.Contains(prev, "wish_i_knew") {
		t.Fatalf("g2 in overview preview %q", prev)
	}
	md := brief["diagnosis"].(map[string]any)["support_pack"].(map[string]any)["markdown"].(string)
	if strings.Contains(md, "wish_i_knew") || strings.Contains(md, "secret") || strings.Contains(md, "jail_break") {
		t.Fatalf("g2 in support pack:\n%s", md)
	}
}

func TestKillSwitchEnvAndPolicy(t *testing.T) {
	if Enabled(true, map[string]string{"ZEUS_CLIENT_DETECTIVE": "0"}) {
		t.Fatal("env 0")
	}
	if Enabled(false, map[string]string{}) {
		t.Fatal("policy false")
	}
	if !Enabled(true, map[string]string{"ZEUS_CLIENT_DETECTIVE": "1"}) {
		t.Fatal("env 1")
	}
	off := false
	if SafeBuild(BriefingArgs{Enabled: &off, Answer: "x", Messages: []map[string]any{{"role": "system", "content": "s"}}}) != nil {
		t.Fatal("policy off should skip")
	}
	on := true
	if SafeBuild(BriefingArgs{Enabled: &on, Env: map[string]string{"ZEUS_CLIENT_DETECTIVE": "0"}, Answer: "x"}) != nil {
		t.Fatal("env 0 should skip")
	}
}

func TestHubHydrateDoesNotClobberClientPass(t *testing.T) {
	base := Build(BriefingArgs{
		Messages: []map[string]any{{"role": "system", "content": systemWithInject}},
		Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "find"}}},
		Answer:   "ok",
	})
	if base["prompt"].(map[string]any)["verdict"] != "pass" {
		t.Fatal("base verdict")
	}
	merged := MergeHubHydrate(base, map[string]any{
		"prompt_checklist": map[string]any{"verdict": "fail"},
		"snapshot":         map[string]any{},
	}, "req-x")
	if merged["hub_hydrated"] != true || merged["source"] != "client+hub" {
		t.Fatalf("merged header %+v", merged)
	}
	if merged["prompt"].(map[string]any)["verdict"] != "pass" {
		t.Fatal("client pass clobbered")
	}
	notes, _ := merged["prompt"].(map[string]any)["notes"].([]string)
	found := false
	for _, n := range notes {
		if strings.Contains(n, "hub_prompt_conflict") {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes %v", notes)
	}
}

func TestSafeBuildNeverRaises(t *testing.T) {
	out := SafeBuild(BriefingArgs{})
	if out != nil && out["version"] != 1 {
		t.Fatalf("%+v", out)
	}
}

func TestSliceSha12IsNotWholeSystem(t *testing.T) {
	system := "You are helpful.\n\n## SCOPE BRIEF\nAAA\n\n## MINI-SCHEMA\nBBB\n"
	flags := CatalogFlagsOf(system, nil)
	if flags["brief_sha12"] != SHA12(SliceBlock(system, "brief")) {
		t.Fatalf("brief sha %v", flags["brief_sha12"])
	}
	if flags["brief_sha12"] == SHA12(system) {
		t.Fatal("brief sha12 must not hash whole system")
	}
	if flags["mini_sha12"] == SHA12(system) {
		t.Fatal("mini sha12 must not hash whole system")
	}
}

func TestInjectForSessionTraceOmitsPreviewsAndMatchesSlice(t *testing.T) {
	system := "You are helpful.\n\n## SCOPE BRIEF\nAAA\n\n## MINI-SCHEMA\nBBB\n"
	inj := InjectForSessionTrace(system, "borrowed", false)
	if inj["source"] != "borrowed" {
		t.Fatalf("source %v", inj["source"])
	}
	brief := inj["scope_brief"].(map[string]any)
	mini := inj["mini_schema"].(map[string]any)
	if brief["sha12"] != SHA12(SliceBlock(system, "brief")) {
		t.Fatalf("brief sha %v", brief["sha12"])
	}
	if mini["sha12"] != SHA12(SliceBlock(system, "mini")) {
		t.Fatalf("mini sha %v", mini["sha12"])
	}
	if _, ok := brief["text"]; ok {
		t.Fatal("slim inject must omit text")
	}
	if _, ok := mini["text"]; ok {
		t.Fatal("slim inject must omit text")
	}
}

func playbookIDSet(brief map[string]any) map[string]struct{} {
	diag := brief["diagnosis"].(map[string]any)
	pbs, _ := diag["playbooks"].([]map[string]any)
	out := map[string]struct{}{}
	for _, p := range pbs {
		out[asString(p["id"])] = struct{}{}
	}
	return out
}
