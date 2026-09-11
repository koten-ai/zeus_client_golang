// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"strings"
	"testing"
)

func TestDefaultJailbreakKeysPresent(t *testing.T) {
	pack, err := MergeRules(MergeRulesOptions{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for k := range JailbreakRuleIDs {
		if _, ok := pack[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}

func TestRequestRulesCannotRewordJailbreakWithoutOverride(t *testing.T) {
	pack, err := MergeRules(MergeRulesOptions{
		RequestRules: map[string]string{"no_invent_data": "Custom invent law."},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if pack["no_invent_data"] == "Custom invent law." {
		t.Fatal("request overlay reworded jailbreak key")
	}
	if !strings.Contains(pack["no_invent_data"], "Do not invent products") {
		t.Fatalf("%q", pack["no_invent_data"])
	}
	if _, ok := pack["no_prompt_dump"]; !ok {
		t.Fatal("no_prompt_dump")
	}
}

func TestRequestRulesOverrideTextRequiresFlag(t *testing.T) {
	pack, err := MergeRules(MergeRulesOptions{
		RequestRules:     map[string]string{"no_invent_data": "Custom invent law."},
		OverrideDefaults: true,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if pack["no_invent_data"] != "Custom invent law." {
		t.Fatalf("%q", pack["no_invent_data"])
	}
}

func TestTenantRulesMayRewordJailbreakKeys(t *testing.T) {
	pack, err := MergeRules(MergeRulesOptions{
		TenantRules: map[string]string{"no_invent_data": "Tenant invent law."},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if pack["no_invent_data"] != "Tenant invent law." {
		t.Fatalf("%q", pack["no_invent_data"])
	}
}

func TestCannotClearDefaultWithoutOverride(t *testing.T) {
	_, err := MergeRules(MergeRulesOptions{
		RequestRules: map[string]string{"no_prompt_dump": ""},
	}, true)
	if err == nil {
		t.Fatal("expected error")
	}
	de, ok := AsError(err)
	if !ok || de.Code != CodeInvalidArgument {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(de.Message, "override_defaults") {
		t.Fatalf("%s", de.Message)
	}
}

func TestFreezeSetsRulesetID(t *testing.T) {
	pack, rid, err := FreezeSessionRules(nil, map[string]string{
		"coupon_presented": "Honor coupon codes from tools only.",
	}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pack["coupon_presented"]; !ok {
		t.Fatal("coupon")
	}
	if !strings.HasPrefix(rid, "rules:") {
		t.Fatalf("rid %q", rid)
	}
	if _, ok := pack["no_prompt_dump"]; !ok {
		t.Fatal("sdk default")
	}
	again, rid2, err := FreezeSessionRules(nil, map[string]string{
		"coupon_presented": "Honor coupon codes from tools only.",
	}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if rid != rid2 || len(again) != len(pack) {
		t.Fatal("ruleset id must be stable")
	}
}

func TestAppendOnlyMidSession(t *testing.T) {
	frozen, err := MergeRules(MergeRulesOptions{}, true)
	if err != nil {
		t.Fatal(err)
	}
	out, err := AppendSessionRules(frozen, map[string]string{"extra": "new rule"})
	if err != nil {
		t.Fatal(err)
	}
	if out["extra"] != "new rule" {
		t.Fatal("append")
	}
	_, err = AppendSessionRules(out, map[string]string{"extra": "changed"})
	if err == nil {
		t.Fatal("expected frozen")
	}
	if de, ok := AsError(err); !ok || !strings.Contains(de.Message, "frozen") {
		t.Fatalf("%v", err)
	}
}

func TestMergeRulesFrozenBlocksExisting(t *testing.T) {
	merged := MergeRulesFrozen(
		map[string]string{"a": "keep", "b": "old"},
		map[string]string{"b": "attack", "c": "new"},
		true,
	)
	if merged["a"] != "keep" || merged["b"] != "old" || merged["c"] != "new" {
		t.Fatalf("%v", merged)
	}
}
