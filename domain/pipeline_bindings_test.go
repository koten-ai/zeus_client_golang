// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"reflect"
	"testing"
)

func peachBody() map[string]any {
	return map[string]any{
		"steps": []any{
			map[string]any{
				"as":          "peach_beers",
				"verb":        "search",
				"entity_type": "Beer",
				"strategy":    "fts",
				"query_text":  "peach",
			},
			map[string]any{
				"as":     "ranked",
				"verb":   "project",
				"ids":    "@peach_beers.ids",
				"fields": []any{"name", "style"},
			},
		},
		"summary": "keep @peach_beers.ids in the summary",
	}
}

func TestFTSProjectIDsBecomeDocKeys(t *testing.T) {
	body, changed := RewriteFTSIdsToDocKeys(peachBody())
	if !reflect.DeepEqual(changed, []string{"peach_beers"}) {
		t.Fatalf("changed %v", changed)
	}
	steps := body["steps"].([]any)
	project := steps[1].(map[string]any)
	if project["ids"] != "@peach_beers.doc_keys" {
		t.Fatalf("ids %v", project["ids"])
	}
	if body["summary"] != "keep @peach_beers.ids in the summary" {
		t.Fatalf("summary %v", body["summary"])
	}
	search := steps[0].(map[string]any)
	if search["strategy"] != "fts" {
		t.Fatalf("strategy %v", search["strategy"])
	}
}

func TestOrderPassthroughRewritesTheNextIDs(t *testing.T) {
	body := map[string]any{
		"steps": []any{
			map[string]any{"as": "peach_beers", "verb": "search", "strategy": "fts", "query_text": "peach"},
			map[string]any{"as": "ranked", "verb": "order", "ids": "@peach_beers.ids", "by": "id"},
			map[string]any{"as": "rows", "verb": "project", "ids": "@ranked.ids", "fields": []any{"name"}},
		},
	}
	out, changed := RewriteFTSIdsToDocKeys(body)
	if !reflect.DeepEqual(changed, []string{"peach_beers", "ranked"}) {
		t.Fatalf("changed %v", changed)
	}
	steps := out["steps"].([]any)
	if steps[1].(map[string]any)["ids"] != "@peach_beers.doc_keys" {
		t.Fatalf("order ids %v", steps[1])
	}
	if steps[2].(map[string]any)["ids"] != "@ranked.doc_keys" {
		t.Fatalf("project ids %v", steps[2])
	}
}

func TestHybridIDsStay(t *testing.T) {
	body := map[string]any{
		"steps": []any{
			map[string]any{"as": "hits", "verb": "search", "strategy": "hybrid", "query_text": "peach"},
			map[string]any{"as": "ranked", "verb": "project", "ids": "@hits.ids"},
		},
	}
	out, changed := RewriteFTSIdsToDocKeys(body)
	if len(changed) != 0 {
		t.Fatalf("changed %v", changed)
	}
	steps := out["steps"].([]any)
	if steps[1].(map[string]any)["ids"] != "@hits.ids" {
		t.Fatalf("ids %v", steps[1])
	}
}

func TestNodeIDsAndDocKeysStay(t *testing.T) {
	body := map[string]any{
		"steps": []any{
			map[string]any{"as": "hits", "verb": "search", "strategy": "fts", "query_text": "peach"},
			map[string]any{
				"as":       "ranked",
				"verb":     "project",
				"ids":      "@hits.node_ids",
				"src_keys": "@hits.doc_keys",
			},
		},
	}
	out, changed := RewriteFTSIdsToDocKeys(body)
	if len(changed) != 0 {
		t.Fatalf("changed %v", changed)
	}
	project := out["steps"].([]any)[1].(map[string]any)
	if project["ids"] != "@hits.node_ids" || project["src_keys"] != "@hits.doc_keys" {
		t.Fatalf("%v", project)
	}
}

func TestNestedParamsAndLongerStepName(t *testing.T) {
	body := map[string]any{
		"steps": []any{
			map[string]any{"as": "peach", "verb": "search", "strategy": "fts", "query_text": "peach"},
			map[string]any{"as": "peach_beers", "verb": "search", "strategy": "fts", "query_text": "peach"},
			map[string]any{"verb": "project", "params": map[string]any{"ids": []any{"@peach.ids", "@peach_beers.ids"}}},
		},
	}
	out, changed := RewriteFTSIdsToDocKeys(body)
	if !reflect.DeepEqual(changed, []string{"peach", "peach_beers"}) {
		t.Fatalf("changed %v", changed)
	}
	params := out["steps"].([]any)[2].(map[string]any)["params"].(map[string]any)
	got := params["ids"].([]any)
	if got[0] != "@peach.doc_keys" || got[1] != "@peach_beers.doc_keys" {
		t.Fatalf("%v", got)
	}
}

func TestStrategyInsideParams(t *testing.T) {
	body := map[string]any{
		"steps": []any{
			map[string]any{"as": "hits", "verb": "search", "params": map[string]any{"strategy": "fts", "query_text": "peach"}},
			map[string]any{"as": "rows", "verb": "project", "ids": "@hits.ids"},
		},
	}
	out, changed := RewriteFTSIdsToDocKeys(body)
	if !reflect.DeepEqual(changed, []string{"hits"}) {
		t.Fatalf("changed %v", changed)
	}
	if out["steps"].([]any)[1].(map[string]any)["ids"] != "@hits.doc_keys" {
		t.Fatalf("%v", out["steps"])
	}
}

func TestNoStepsIsUnchanged(t *testing.T) {
	body := map[string]any{"summary": "hi"}
	out, changed := RewriteFTSIdsToDocKeys(body)
	if len(changed) != 0 {
		t.Fatalf("changed %v", changed)
	}
	if !reflect.DeepEqual(out, body) {
		t.Fatalf("%v", out)
	}
}

func TestRewriteDoesNotMutateTheInput(t *testing.T) {
	body := peachBody()
	_, _ = RewriteFTSIdsToDocKeys(body)
	project := body["steps"].([]any)[1].(map[string]any)
	if project["ids"] != "@peach_beers.ids" {
		t.Fatalf("input mutated %v", project["ids"])
	}
}
