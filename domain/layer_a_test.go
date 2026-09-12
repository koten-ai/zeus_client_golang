// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validReturn(extra map[string]any) map[string]any {
	base := map[string]any{
		"summary":             "Found two beers.",
		"query_decomposition": map[string]any{"intent": "List", "entity": "Beer"},
		"decomposition":       map[string]any{"targets": []any{map[string]any{"entity_type": "Beer"}}, "predicates": map[string]any{}},
		"confidence":          "high",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func TestRequiredFourOK(t *testing.T) {
	layer := ParseLayerA(validReturn(nil), ParseLayerAOpts{})
	if !layer.OK() {
		t.Fatalf("errors %v", layer.Errors)
	}
	if !strings.HasPrefix(layer.Summary, "Found") {
		t.Fatalf("summary %q", layer.Summary)
	}
	if layer.Confidence != "high" {
		t.Fatalf("confidence %q", layer.Confidence)
	}
}

func TestMissingSummaryErrors(t *testing.T) {
	layer := ParseLayerA(map[string]any{
		"query_decomposition": map[string]any{},
		"decomposition":       map[string]any{},
		"confidence":          "med",
	}, ParseLayerAOpts{})
	if layer.OK() {
		t.Fatal("expected not ok")
	}
	if !errorsContain(layer.Errors, "summary") {
		t.Fatalf("errors %v", layer.Errors)
	}
}

func TestEmptySummaryErrors(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{"summary": ""}), ParseLayerAOpts{})
	if layer.OK() {
		t.Fatal("expected not ok")
	}
	if !errorsContain(layer.Errors, "summary") {
		t.Fatalf("errors %v", layer.Errors)
	}
}

func TestQDMustBeObject(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{"query_decomposition": "list beers"}), ParseLayerAOpts{})
	if layer.OK() {
		t.Fatal("expected not ok")
	}
	if !errorsContain(layer.Errors, "query_decomposition") {
		t.Fatalf("errors %v", layer.Errors)
	}
}

func TestConfidenceNotInEnum(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{"confidence": "medium"}), ParseLayerAOpts{})
	if layer.OK() {
		t.Fatal("expected not ok")
	}
	if !errorsContain(layer.Errors, "confidence") {
		t.Fatalf("errors %v", layer.Errors)
	}
}

func TestObjectTriggers(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{
		"business_rules_triggers": map[string]any{"no_invent_data": true, "coupon": false},
	}), ParseLayerAOpts{})
	if layer.BusinessRulesTriggers["no_invent_data"] != true {
		t.Fatalf("%v", layer.BusinessRulesTriggers)
	}
	if layer.BusinessRulesTriggers["coupon"] != false {
		t.Fatalf("%v", layer.BusinessRulesTriggers)
	}
}

func TestArrayTriggersRejectedByDefault(t *testing.T) {
	trig, errs := NormalizeTriggers([]any{true}, []string{"a"})
	if len(trig) != 0 {
		t.Fatalf("trig %v", trig)
	}
	if !errorsContain(errs, "rejected") {
		t.Fatalf("errs %v", errs)
	}
	layer := ParseLayerA(validReturn(map[string]any{
		"business_rules_triggers": []any{true, false},
	}), ParseLayerAOpts{})
	if layer.OK() {
		t.Fatal("expected not ok")
	}
	if len(layer.BusinessRulesTriggers) != 0 {
		t.Fatalf("triggers %v", layer.BusinessRulesTriggers)
	}
	if !errorsContain(layer.Errors, "arrays rejected") {
		t.Fatalf("errors %v", layer.Errors)
	}
}

func TestUIViewStripsG2(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{
		"jail_break_attempt":      0.9,
		"wish_i_knew":             []any{map[string]any{"gap": "x"}},
		"business_rules_triggers": map[string]any{"no_prompt_dump": true},
	}), ParseLayerAOpts{})
	ui := UIView(layer, nil)
	for _, k := range []string{"wish_i_knew", "jail_break_attempt", "business_rules_triggers", "hooks_jailbreak_score"} {
		if _, ok := ui[k]; ok {
			t.Errorf("G2 key %s in ui", k)
		}
	}
	if ui["summary"] != layer.Summary {
		t.Fatalf("summary %v", ui["summary"])
	}
}

func TestArtifactsKeepG2AndDualScore(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{"jail_break_attempt": 0.4}), ParseLayerAOpts{})
	art := ArtifactsView(layer, 0.9, "answer", nil)
	if art["jail_break_attempt"] != 0.4 {
		t.Fatalf("model score %v", art["jail_break_attempt"])
	}
	if art["hooks_jailbreak_score"] != 0.9 {
		t.Fatalf("hooks score %v", art["hooks_jailbreak_score"])
	}
	if art["policy"] != "answer" {
		t.Fatalf("policy %v", art["policy"])
	}
}

func TestAppOutputTypeCheckStrip(t *testing.T) {
	fields := map[string]any{"offer_code": map[string]any{"type": "string", "description": "promo"}}
	out, errs := ValidateAppOutput(map[string]any{"offer_code": 123}, fields, "strip")
	if len(out) != 0 {
		t.Fatalf("out %v", out)
	}
	if len(errs) == 0 {
		t.Fatal("expected errs")
	}
}

func TestAppOutputOK(t *testing.T) {
	fields := map[string]any{"offer_code": map[string]any{"type": "string", "description": "promo"}}
	out, errs := ValidateAppOutput(map[string]any{"offer_code": "SAVE10"}, fields, "strip")
	if out["offer_code"] != "SAVE10" {
		t.Fatalf("out %v", out)
	}
	if len(errs) != 0 {
		t.Fatalf("errs %v", errs)
	}
}

func TestParseAppOutputWithOutputRequest(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{"app_output": map[string]any{"offer_code": "X"}}), ParseLayerAOpts{
		OutputRequest: map[string]any{
			"app": map[string]any{"fields": map[string]any{"offer_code": map[string]any{"type": "string", "description": "code"}}},
		},
	})
	if layer.AppOutput["offer_code"] != "X" {
		t.Fatalf("app_output %v", layer.AppOutput)
	}
}

func TestPeelLayerASummaryFencedYAMLDump(t *testing.T) {
	dump := "```\n" +
		`summary: "Found two great salons in Tampa."` + "\n" +
		"confidence: med\n" +
		`query_decomposition: {"intent": "salons"}` + "\n" +
		`decomposition: {"targets": []}` + "\n" +
		"policy_action: answer\n" +
		"wish_i_knew: []\n" +
		"```"
	got, ok := PeelLayerASummary(dump)
	if !ok || got != "Found two great salons in Tampa." {
		t.Fatalf("peel %q ok=%v", got, ok)
	}
	if UserFacingAnswer(dump, "", "") != "Found two great salons in Tampa." {
		t.Fatalf("user facing %q", UserFacingAnswer(dump, "", ""))
	}
	if UserFacingAnswer(dump, "UI preferred", "") != "UI preferred" {
		t.Fatalf("ui_text win %q", UserFacingAnswer(dump, "UI preferred", ""))
	}
	if strings.Contains(UserFacingAnswer(dump, "", ""), "wish_i_knew") {
		t.Fatal("G2 leaked via peel")
	}
}

func TestUserFacingAnswerPassesProse(t *testing.T) {
	prose := "Here are three salons worth visiting."
	if _, ok := PeelLayerASummary(prose); ok {
		t.Fatal("prose must not peel")
	}
	if UserFacingAnswer(prose, "", "") != prose {
		t.Fatalf("got %q", UserFacingAnswer(prose, "", ""))
	}
}

func TestPeelLayerASummaryUnescapesNewlines(t *testing.T) {
	dump := `summary: "Line one.\nLine two."` + "\n" +
		"confidence: high\n" +
		"policy_action: answer\n" +
		`query_decomposition: {"intent": "x"}` + "\n"
	got, ok := PeelLayerASummary(dump)
	if !ok || got != "Line one.\nLine two." {
		t.Fatalf("peel %q ok=%v", got, ok)
	}
}

const htmlPipeline = "```html\n" +
	"<pipeline>\n" +
	`<steps>[{"as": "airports", "verb": "find", "entity_type": "Airport", ` +
	`"where": {"country": "United States"}, "limit": 50, "return": "ids"}]</steps>` + "\n" +
	`<return>["rows"]</return>` + "\n" +
	"<summary>Airports in the United States (sample of matching Airport records).</summary>\n" +
	`<query_decomposition>{"intent": "list_airports", "entity": "Airport"}</query_decomposition>` + "\n" +
	"<confidence>high</confidence>\n" +
	`<decomposition>{"targets": ["Airport"]}</decomposition>` + "\n" +
	"</pipeline>\n" +
	"```"

func TestParsePipelineEnvelopeFromHTMLFence(t *testing.T) {
	args := ParsePipelineEnvelope(htmlPipeline)
	if args == nil {
		t.Fatal("expected pipeline args")
	}
	steps, ok := args["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatalf("steps %v", args["steps"])
	}
	step0, ok := steps[0].(map[string]any)
	if !ok || step0["verb"] != "find" {
		t.Fatalf("step0 %v", steps[0])
	}
	ret, ok := args["return"].([]any)
	if !ok || len(ret) != 1 || ret[0] != "rows" {
		t.Fatalf("return %v", args["return"])
	}
	if args["confidence"] != "high" {
		t.Fatalf("confidence %v", args["confidence"])
	}
	qd, ok := args["query_decomposition"].(map[string]any)
	if !ok || qd["entity"] != "Airport" {
		t.Fatalf("qd %v", args["query_decomposition"])
	}
}

func TestUserFacingAnswerPeelsPipelineXMLSummary(t *testing.T) {
	want := "Airports in the United States (sample of matching Airport records)."
	if UserFacingAnswer(htmlPipeline, "", "") != want {
		t.Fatalf("got %q", UserFacingAnswer(htmlPipeline, "", ""))
	}
	if ParsePipelineEnvelope("Hello from Zeus.") != nil {
		t.Fatal("prose is not a pipeline")
	}
	if UserFacingAnswer("Hello from Zeus.", "", "") != "Hello from Zeus." {
		t.Fatal("prose pass-through")
	}
}

func TestCompactLayerAOmitsG2(t *testing.T) {
	layer := ParseLayerA(validReturn(map[string]any{
		"jail_break_attempt": 0.9,
		"wish_i_knew":        []any{map[string]any{"gap": "x"}},
	}), ParseLayerAOpts{})
	c := CompactLayerA(layer, "client_terminate")
	for _, k := range []string{"wish_i_knew", "jail_break_attempt", "hooks_jailbreak_score", "business_rules_triggers"} {
		if _, ok := c[k]; ok {
			t.Errorf("G2 key %s in compact", k)
		}
	}
	if c["ok"] != true {
		t.Fatalf("ok %v", c["ok"])
	}
}

func TestL2LayerARequiredFour001(t *testing.T) {
	casePath := findConformance(t, filepath.Join("conformance", "fixtures", "L2_control_plane", "layer_a_required_four", "case.json"))
	doc := readJSONFile(t, casePath)
	if asString(doc["id"]) != "L2.layer_a.required_four.001" {
		t.Fatalf("id %v", doc["id"])
	}
	input, _ := doc["input"].(map[string]any)
	validPath := asString(input["valid_path"])
	golden := readJSONFile(t, findConformance(t, validPath))
	valid := ParseLayerA(golden, ParseLayerAOpts{})
	if !valid.OK() {
		t.Fatalf("valid.ok false: %v", valid.Errors)
	}
	invalidRaw, _ := input["invalid"].(map[string]any)
	invalid := ParseLayerA(invalidRaw, ParseLayerAOpts{})
	if invalid.OK() {
		t.Fatal("invalid.ok want false")
	}
	d := DecidePolicy(invalid, DecidePolicyOpts{})
	if d.Policy != PolicyError || !d.Forced || d.Reason != "layer_a_validation_failed" {
		t.Fatalf("invalid.error_class %+v", d)
	}
	expect, _ := doc["expect"].(map[string]any)
	if expect["valid.ok"] != true || expect["invalid.ok"] != false {
		t.Fatalf("expect %v", expect)
	}
	if asString(expect["invalid.error_class"]) != "layer_a_validation_failed" {
		t.Fatalf("error_class %v", expect["invalid.error_class"])
	}
}

func errorsContain(errs []string, needle string) bool {
	for _, e := range errs {
		if strings.Contains(e, needle) {
			return true
		}
	}
	return false
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

func mustJSONNumber(t *testing.T, v any) float64 {
	t.Helper()
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			t.Fatal(err)
		}
		return f
	case int:
		return float64(x)
	default:
		t.Fatalf("not a number: %T %v", v, v)
		return 0
	}
}
