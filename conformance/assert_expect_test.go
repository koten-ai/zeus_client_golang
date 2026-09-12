// SPDX-License-Identifier: BUSL-1.1

package conformance

import "testing"

func TestAssertExpectsUnknownKeyFails(t *testing.T) {
	diffs := AssertExpects(map[string]any{"result": true}, map[string]any{"no_such": true})
	if len(diffs) == 0 {
		t.Fatal("unknown expect key must fail")
	}
}

func TestAssertExpectsGTE(t *testing.T) {
	diffs := AssertExpects(
		map[string]any{"catalog.verb_count_gte": 5},
		map[string]any{"catalog.verb_count_gte": 2},
	)
	if len(diffs) != 0 {
		t.Fatalf("%v", diffs)
	}
	diffs = AssertExpects(
		map[string]any{"catalog.verb_count_gte": 1},
		map[string]any{"catalog.verb_count_gte": 2},
	)
	if len(diffs) == 0 {
		t.Fatal("want gte fail")
	}
}

func TestAssertExpectsNumericIntVsJSONFloat(t *testing.T) {
	diffs := AssertExpects(map[string]any{"row_count": 6}, map[string]any{"row_count": float64(6)})
	if len(diffs) != 0 {
		t.Fatalf("%v", diffs)
	}
}

func TestAssertCaseContains(t *testing.T) {
	diffs := AssertCase(
		map[string]any{"lineage_contains": "base-6.1 extra"},
		map[string]any{"lineage_contains": "base-6.1"},
	)
	if len(diffs) != 0 {
		t.Fatalf("%v", diffs)
	}
	diffs = AssertCase(
		map[string]any{"lineage_contains": "other"},
		map[string]any{"lineage_contains": "base-6.1"},
	)
	if len(diffs) == 0 {
		t.Fatal("want contains fail")
	}
}

func TestGetPathNested(t *testing.T) {
	got := GetPath(map[string]any{"diagnosis": map[string]any{"prompt_grade": "pass"}}, "diagnosis.prompt_grade")
	if got != "pass" {
		t.Fatalf("%v", got)
	}
	if GetPath(map[string]any{}, "a.b") != nil {
		t.Fatal("missing")
	}
}
