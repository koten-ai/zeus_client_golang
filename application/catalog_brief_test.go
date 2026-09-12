// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
)

func TestLiveModeCandidatesDedupesAnalytics(t *testing.T) {
	got := LiveModeCandidates("analytics", "")
	if got[0] != "analytics" {
		t.Fatalf("%v", got)
	}
	n := 0
	for _, m := range got {
		if m == "analytics" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dup analytics %v", got)
	}
}

func TestEnsureScopeBriefAlreadyPresentReturnsSameMap(t *testing.T) {
	doc := map[string]any{
		"messages": []any{map[string]any{"content": "## SCOPE BRIEF\nbucket=b\n"}},
	}
	out := EnsureScopeBrief(context.Background(), doc, nil, "b", "s", "analytics")
	if out.Merged || out.Note != "scope_brief: already present" {
		t.Fatalf("%+v", out)
	}
	doc["__mark"] = true
	if out.Body["__mark"] != true {
		t.Fatal("already-present must not clone")
	}
}

func TestEnsureScopeBriefSkipsWithoutFetch(t *testing.T) {
	doc := map[string]any{"messages": []any{map[string]any{"content": "locked"}}}
	out := EnsureScopeBrief(context.Background(), doc, nil, "b", "s", "analytics")
	if out.Merged || !strings.Contains(out.Note, "no catalog_remote") {
		t.Fatalf("%+v", out)
	}
}

func TestEnsureScopeBriefMergesLive(t *testing.T) {
	doc := map[string]any{"messages": []any{map[string]any{"content": "locked"}}}
	fetch := func(_ context.Context, _, _, _ string) (map[string]any, error) {
		return map[string]any{
			"messages": []any{map[string]any{"content": "## SCOPE BRIEF\nscope: b/s\n"}},
		}, nil
	}
	out := EnsureScopeBrief(context.Background(), doc, fetch, "b", "s", "analytics")
	if !out.Merged {
		t.Fatalf("%+v", out)
	}
	if domain.ExtractScopeBrief(out.Body) == "" {
		t.Fatalf("%v", out.Body)
	}
}
