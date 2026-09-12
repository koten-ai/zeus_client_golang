// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func policyLayer(extra map[string]any) LayerA {
	base := map[string]any{
		"summary":             "Here are results.",
		"query_decomposition": map[string]any{"intent": "List", "entity": "Beer"},
		"decomposition":       map[string]any{"targets": []any{map[string]any{"entity_type": "Beer"}}},
		"confidence":          "high",
	}
	for k, v := range extra {
		base[k] = v
	}
	return ParseLayerA(base, ParseLayerAOpts{})
}

func TestDefaultAnswerPolicy(t *testing.T) {
	d := DecidePolicy(policyLayer(map[string]any{"policy_action": "answer"}), DecidePolicyOpts{})
	if d.Policy != PolicyAnswer {
		t.Fatalf("policy %s", d.Policy)
	}
	if d.UIText != "Here are results." {
		t.Fatalf("ui %q", d.UIText)
	}
	if d.Reason != "model_policy_action" {
		t.Fatalf("reason %s", d.Reason)
	}
}

func TestHooksMustRefuse(t *testing.T) {
	d := DecidePolicy(policyLayer(nil), DecidePolicyOpts{HooksMustRefuse: true})
	if d.Policy != PolicyRefuse {
		t.Fatalf("policy %s", d.Policy)
	}
	if !d.Forced {
		t.Fatal("forced")
	}
	if d.Reason != "hooks_must_refuse" {
		t.Fatalf("reason %s", d.Reason)
	}
	if strings.Contains(d.UIText, "wish_i_knew") {
		t.Fatal("G2 in refuse chrome")
	}
	if strings.Contains(strings.ToLower(d.UIText), "jail_break") {
		t.Fatal("jail_break in refuse chrome")
	}
}

func TestJailbreakTriggersAndScore(t *testing.T) {
	layer := policyLayer(map[string]any{
		"business_rules_triggers": map[string]any{"no_prompt_dump": true},
		"jail_break_attempt":      0.8,
	})
	d := DecidePolicy(layer, DecidePolicyOpts{HooksJailbreakScore: 0.1})
	if d.Policy != PolicyRefuse {
		t.Fatalf("policy %s", d.Policy)
	}
	if d.Reason != "jailbreak_triggers_and_score" {
		t.Fatalf("reason %s", d.Reason)
	}
	if d.HooksJailbreakScore != 0.1 {
		t.Fatalf("hooks %v", d.HooksJailbreakScore)
	}
	if layer.JailBreakAttempt == nil || *layer.JailBreakAttempt != 0.8 {
		t.Fatalf("model score %v", layer.JailBreakAttempt)
	}
	art := d.Artifacts(layer)
	if art["jail_break_attempt"] != 0.8 {
		t.Fatalf("art model %v", art["jail_break_attempt"])
	}
	if art["hooks_jailbreak_score"] != 0.1 {
		t.Fatalf("art hooks %v", art["hooks_jailbreak_score"])
	}
	if _, ok := art["wish_i_knew"]; !ok {
		t.Fatal("G2 may live in artifacts")
	}
	ui := d.UI(layer)
	if _, ok := ui["wish_i_knew"]; ok {
		t.Fatal("wish_i_knew in ui")
	}
	if _, ok := ui["jail_break_attempt"]; ok {
		t.Fatal("jail_break_attempt in ui")
	}
}

func TestModelPolicyActionClarify(t *testing.T) {
	d := DecidePolicy(policyLayer(map[string]any{
		"policy_action": "clarify",
		"summary":       "Which city?",
	}), DecidePolicyOpts{})
	if d.Policy != PolicyClarify {
		t.Fatalf("policy %s", d.Policy)
	}
	if d.UIText != "Which city?" {
		t.Fatalf("ui %q", d.UIText)
	}
	if d.Reason != "model_policy_action" {
		t.Fatalf("reason %s", d.Reason)
	}
}

func TestStickyOrFlags(t *testing.T) {
	flags := StickyOrFlags(map[string]bool{"a": true}, map[string]bool{"a": false, "b": true})
	if !flags["a"] || !flags["b"] || len(flags) != 2 {
		t.Fatalf("flags %v", flags)
	}
}

func TestSoftRequirePolicyAction(t *testing.T) {
	s := PolicySettings{SoftRequirePolicyAction: true}
	d := DecidePolicy(policyLayer(nil), DecidePolicyOpts{Settings: s, BrandInjectPresent: true})
	if !d.SoftRequirePolicyActionMissing {
		t.Fatal("soft missing")
	}
}

func TestLayerAValidationFailedForcesError(t *testing.T) {
	bad := ParseLayerA(map[string]any{"confidence": "high"}, ParseLayerAOpts{})
	d := DecidePolicy(bad, DecidePolicyOpts{})
	if d.Policy != PolicyError {
		t.Fatalf("policy %s", d.Policy)
	}
	if !d.Forced {
		t.Fatal("forced")
	}
	if d.Reason != "layer_a_validation_failed" {
		t.Fatalf("reason %s", d.Reason)
	}
}

func TestStickyFlagsFromSettings(t *testing.T) {
	s := PolicySettings{StickyFlags: map[string]bool{"loyalty": true}}
	layer := policyLayer(map[string]any{
		"business_rules_triggers": map[string]any{"promo": true},
	})
	d := DecidePolicy(layer, DecidePolicyOpts{Settings: s})
	if !d.Flags["loyalty"] || !d.Flags["promo"] {
		t.Fatalf("flags %v", d.Flags)
	}
}

func TestL2PolicyMatrix001(t *testing.T) {
	casePath := findConformance(t, filepath.Join("conformance", "fixtures", "L2_control_plane", "policy_table_matrix", "case.json"))
	caseDoc := readJSONFile(t, casePath)
	if asString(caseDoc["id"]) != "L2.policy.matrix.001" {
		t.Fatalf("id %v", caseDoc["id"])
	}
	expect, _ := caseDoc["expect"].(map[string]any)
	wantCount := int(mustJSONNumber(t, expect["row_count"]))
	rowsPath := filepath.Join(filepath.Dir(casePath), "rows.json")
	raw, err := os.ReadFile(rowsPath)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Rows []struct {
			ID    string `json:"id"`
			Input struct {
				LayerA              map[string]any `json:"layer_a"`
				HooksMustRefuse     bool           `json:"hooks_must_refuse"`
				HooksJailbreakScore float64        `json:"hooks_jailbreak_score"`
			} `json:"input"`
			Expect struct {
				Policy string `json:"policy"`
				Forced bool   `json:"forced"`
				Reason string `json:"reason"`
			} `json:"expect"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Rows) != wantCount {
		t.Fatalf("row_count %d want %d", len(file.Rows), wantCount)
	}
	for _, row := range file.Rows {
		t.Run(row.ID, func(t *testing.T) {
			layer := ParseLayerA(row.Input.LayerA, ParseLayerAOpts{})
			d := DecidePolicy(layer, DecidePolicyOpts{
				HooksMustRefuse:     row.Input.HooksMustRefuse,
				HooksJailbreakScore: row.Input.HooksJailbreakScore,
			})
			if d.Policy != row.Expect.Policy || d.Forced != row.Expect.Forced || d.Reason != row.Expect.Reason {
				t.Fatalf("got policy=%s forced=%v reason=%s want policy=%s forced=%v reason=%s (layer.ok=%v errors=%v)",
					d.Policy, d.Forced, d.Reason, row.Expect.Policy, row.Expect.Forced, row.Expect.Reason,
					layer.OK(), layer.Errors)
			}
		})
	}
}
