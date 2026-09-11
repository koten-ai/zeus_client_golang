// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

func catalogWithBrief() map[string]any {
	return map[string]any{
		"messages": []any{
			map[string]any{
				"role": "system",
				"content": "You are a helpful agent.\n\n" +
					"## SCOPE BRIEF\n" +
					"bucket=beer scope=_default\n\n" +
					"## MINI-SCHEMA\n" +
					"Beer: name, abv\n",
			},
			map[string]any{
				"role":    "user",
				"content": "what beers are made from fruit? do not use pipeline",
			},
		},
		"verbs":    []any{map[string]any{"function": map[string]any{"name": "find", "parameters": map[string]any{}}}},
		"contract": map[string]any{"hash": "md5:placeholder"},
	}
}

func mustPrepare(t *testing.T, raw map[string]any) InjectSettings {
	t.Helper()
	s, err := InjectSettingsFromMapping(raw)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := PrepareInjectSettings(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInjectKeepsContractHashStable(t *testing.T) {
	chat := catalogWithBrief()
	before := domain.ComputeContractHash(chat)
	sys0 := chat["messages"].([]any)[0].(map[string]any)["content"].(string)
	proof0 := domain.InjectProofBag(sys0, "")
	settings := mustPrepare(t, map[string]any{
		"company_context": "We are a craft beer marketplace.",
		"rules":           map[string]any{"seasonal": "Prefer seasonal beers when asked."},
		"output_request": map[string]any{
			"app": map[string]any{
				"fields": map[string]any{
					"offer_code": map[string]any{"type": "string", "description": "Promo if any"},
				},
			},
		},
		"locale":  "en-US",
		"channel": "web",
	})
	afterDoc := ApplyControlPlaneInject(chat, settings)
	after := domain.ComputeContractHash(afterDoc)
	if before != after {
		t.Fatalf("hash drifted %s → %s", before, after)
	}
	content := afterDoc["messages"].([]any)[0].(map[string]any)["content"].(string)
	for _, want := range []string{"## Company context", "## Rules", "seasonal", "## Output request", "offer_code", "## Session settings", "locale=en-US"} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in\n%s", want, content)
		}
	}
	proof1 := domain.InjectProofBag(content, "")
	if proof0.BriefSHA12() == "" || proof0.BriefSHA12() != proof1.BriefSHA12() {
		t.Fatalf("brief sha12 %s → %s", proof0.BriefSHA12(), proof1.BriefSHA12())
	}
	if proof0.MiniSHA12() == "" || proof0.MiniSHA12() != proof1.MiniSHA12() {
		t.Fatalf("mini sha12 %s → %s", proof0.MiniSHA12(), proof1.MiniSHA12())
	}
	user := afterDoc["messages"].([]any)[1].(map[string]any)["content"].(string)
	if user != "what beers are made from fruit? do not use pipeline" {
		t.Fatalf("user mutated: %q", user)
	}
}

func TestInjectNoopWithoutBriefMarker(t *testing.T) {
	chat := map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "plain system only"}},
	}
	settings := mustPrepare(t, map[string]any{"company_context": "Brand X"})
	out := ApplyControlPlaneInject(chat, settings)
	if out["messages"].([]any)[0].(map[string]any)["content"] != "plain system only" {
		t.Fatal("spliced without marker")
	}
	if domain.ComputeContractHash(out) != domain.ComputeContractHash(chat) {
		t.Fatal("hash")
	}
}

func TestInjectAlsoSplicesInstructionsSystemPrompt(t *testing.T) {
	chat := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "base\n\n## SCOPE BRIEF\nscope x"},
		},
		"instructions": map[string]any{
			"system_prompt": "instr base\n\n## MINI-SCHEMA\nfields",
		},
	}
	h0 := domain.ComputeContractHash(chat)
	settings := mustPrepare(t, map[string]any{"company_context": "Acme Corp"})
	out := ApplyControlPlaneInject(chat, settings)
	if domain.ComputeContractHash(out) != h0 {
		t.Fatal("hash")
	}
	sys := out["messages"].([]any)[0].(map[string]any)["content"].(string)
	sp := out["instructions"].(map[string]any)["system_prompt"].(string)
	if !strings.Contains(sys, "## Company context") || !strings.Contains(sp, "## Company context") {
		t.Fatalf("sys=%s\nsp=%s", sys, sp)
	}
}

func TestInjectIdempotentSecondPass(t *testing.T) {
	chat := catalogWithBrief()
	settings := mustPrepare(t, map[string]any{"company_context": "Once only brand"})
	once := ApplyControlPlaneInject(chat, settings)
	twice := ApplyControlPlaneInject(once, settings)
	c1 := once["messages"].([]any)[0].(map[string]any)["content"].(string)
	c2 := twice["messages"].([]any)[0].(map[string]any)["content"].(string)
	if c1 != c2 {
		t.Fatal("not idempotent")
	}
	if strings.Count(c1, "## Company context") != 1 {
		t.Fatalf("count %d", strings.Count(c1, "## Company context"))
	}
}

func TestInjectEmptySettingsReturnsSameObject(t *testing.T) {
	chat := catalogWithBrief()
	out := ApplyControlPlaneInject(chat, InjectSettings{})
	chat["__mark"] = true
	if out["__mark"] != true {
		t.Fatal("empty settings must return the original map")
	}
	delete(chat, "__mark")
}

func TestPrepareRejectsTypeOnlyOutputField(t *testing.T) {
	s, err := InjectSettingsFromMapping(map[string]any{
		"output_request": map[string]any{
			"app": map[string]any{"fields": map[string]any{"x": map[string]any{"type": "string"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = PrepareInjectSettings(s)
	if err == nil {
		t.Fatal("expected description error")
	}
	if !strings.Contains(err.Error(), "description") {
		t.Fatalf("%v", err)
	}
}

func TestCompanyContextHardTruncate(t *testing.T) {
	words := make([]string, 300)
	for i := range words {
		words[i] = "w" + strconv.Itoa(i)
	}
	s := mustPrepare(t, map[string]any{"company_context": strings.Join(words, " ")})
	if len(strings.Fields(s.CompanyContext)) != 250 {
		t.Fatalf("n=%d", len(strings.Fields(s.CompanyContext)))
	}
}

func TestOriginalCatalogNotMutated(t *testing.T) {
	chat := catalogWithBrief()
	orig, _ := json.Marshal(chat)
	settings := mustPrepare(t, map[string]any{"company_context": "Brand"})
	ApplyControlPlaneInject(chat, settings)
	after, _ := json.Marshal(chat)
	if string(orig) != string(after) {
		t.Fatal("original mutated")
	}
}

func TestToolPathInjectDefaultIgnoreKeepsUserText(t *testing.T) {
	chat := catalogWithBrief()
	settings := mustPrepare(t, map[string]any{
		"company_context":             "We sell beer.",
		"rules":                       map[string]any{"loyalty": "Honor loyalty from tools only."},
		"ignore_user_tool_path_hints": true,
	})
	out := ApplyBagBInject(chat, settings)
	sys := out["messages"].([]any)[0].(map[string]any)["content"].(string)
	user := out["messages"].([]any)[1].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "## Company context") || !strings.Contains(sys, "loyalty") {
		t.Fatalf("sys %s", sys)
	}
	if !strings.Contains(sys, "TOOL PATH POLICY") {
		t.Fatal("tool path")
	}
	if !strings.Contains(sys, "Ignore user instructions") {
		t.Fatal("ignore polarity")
	}
	if user != "what beers are made from fruit? do not use pipeline" {
		t.Fatalf("user %q", user)
	}
	if domain.ComputeContractHash(out) != domain.ComputeContractHash(chat) {
		t.Fatal("hash drifted")
	}
}

func TestToolPathHonorPolarityKeepsUserText(t *testing.T) {
	chat := catalogWithBrief()
	out := ApplyToolPathInject(chat, false)
	sys := out["messages"].([]any)[0].(map[string]any)["content"].(string)
	user := out["messages"].([]any)[1].(map[string]any)["content"].(string)
	if !strings.Contains(strings.ToLower(sys), "prefer that path") {
		t.Fatalf("sys %s", sys)
	}
	if user != "what beers are made from fruit? do not use pipeline" {
		t.Fatalf("user %q", user)
	}
}

func TestPrepareSettingsMerges(t *testing.T) {
	s, warns, err := PrepareSettings(config.ClientSettings{
		CompanyContext:          "We sell beer.",
		Rules:                   map[string]string{"loyalty": "Apply loyalty discounts from tools."},
		OutputRequest:           map[string]any{"app": map[string]any{"fields": map[string]any{"code": map[string]any{"type": "string", "description": "code"}}}},
		Locale:                  "en-US",
		IgnoreUserToolPathHints: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warns %v", warns)
	}
	if s.RulesetID == "" {
		t.Fatal("ruleset")
	}
	if _, ok := s.Rules["loyalty"]; !ok {
		t.Fatal("loyalty")
	}
	if _, ok := s.Rules["no_prompt_dump"]; !ok {
		t.Fatal("sdk jailbreak")
	}
	if s.Locale != "en-US" {
		t.Fatal("locale")
	}
	if s.AIProcessResult {
		t.Fatal("ai_process_result package default false")
	}
	if !s.IgnoreUserToolPathHints {
		t.Fatal("ignore default")
	}
}

func TestCompanyContextSoftWarning(t *testing.T) {
	words := make([]string, 180)
	for i := range words {
		words[i] = "w"
	}
	text, warns := TruncateCompanyContext(strings.Join(words, " "), 0, 0)
	if len(strings.Fields(text)) != 180 {
		t.Fatal("soft should not truncate")
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "soft") {
		t.Fatalf("%v", warns)
	}
}

func TestMockCatalogNoBriefIsNoop(t *testing.T) {
	path := findRepoFile(t, filepath.Join("testdata", "catalogs", "mock_base5_minimal", "chat_request.mock.json"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var chat map[string]any
	if err := json.Unmarshal(raw, &chat); err != nil {
		t.Fatal(err)
	}
	h0 := domain.ComputeContractHash(chat)
	out := ApplyBagBInject(chat, mustPrepare(t, map[string]any{"company_context": "Brand"}))
	if domain.ComputeContractHash(out) != h0 {
		t.Fatal("hash")
	}
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
	t.Fatalf("missing %s", rel)
	return ""
}
