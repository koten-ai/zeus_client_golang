// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

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
		},
		"verbs":    []any{map[string]any{"function": map[string]any{"name": "find", "parameters": map[string]any{}}}},
		"contract": map[string]any{"hash": "md5:placeholder"},
	}
}

func TestSliceBlockAndSHA12(t *testing.T) {
	sys := catalogWithBrief()["messages"].([]any)[0].(map[string]any)["content"].(string)
	brief := SliceBlock(sys, "brief")
	mini := SliceBlock(sys, "mini")
	if !contains(brief, "## SCOPE BRIEF") || contains(brief, "## MINI-SCHEMA") {
		t.Fatalf("brief %q", brief)
	}
	if !contains(mini, "## MINI-SCHEMA") || contains(mini, "## SCOPE BRIEF") {
		t.Fatalf("mini %q", mini)
	}
	if SHA12("") != "" {
		t.Fatal("empty")
	}
	if SHA12(brief) == SHA12(mini) || len(SHA12(brief)) != 12 {
		t.Fatalf("sha %s %s", SHA12(brief), SHA12(mini))
	}
}

func TestInjectProofBagSHA12(t *testing.T) {
	sys := catalogWithBrief()["messages"].([]any)[0].(map[string]any)["content"].(string)
	bag := InjectProofBag(sys, "")
	if !bag.ScopeBrief.Present || !bag.MiniSchema.Present {
		t.Fatalf("%+v", bag)
	}
	if bag.BriefSHA12() != SHA12(SliceBlock(sys, "brief")) {
		t.Fatal("brief sha")
	}
	if bag.MiniSHA12() != SHA12(SliceBlock(sys, "mini")) {
		t.Fatal("mini sha")
	}
	m := bag.Map()
	if m["source"] != "client_llm" {
		t.Fatalf("%v", m)
	}
}
