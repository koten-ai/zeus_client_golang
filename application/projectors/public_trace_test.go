// SPDX-License-Identifier: BUSL-1.1

package projectors

import (
	"fmt"
	"strings"
	"testing"
)

func TestPublicTraceIncludesSessionWhenIDsPresent(t *testing.T) {
	pt := BuildPublicTrace(PublicTrace{
		TurnID:              "turn_1",
		Answer:              "ok",
		Status:              "ok",
		Rounds:              1,
		Hops:                []map[string]any{{"req_id": "req-a", "name": "find"}},
		AIProcessResult:     true,
		AIProcessResultExit: "direct",
		Session: map[string]any{
			"id": "sess_1", "req_ids": []any{"req-a"}, "preferred_req_id": "req-a", "contract_status": "match",
		},
	})
	sess, _ := pt["session"].(map[string]any)
	if sess["id"] != "sess_1" {
		t.Fatalf("%v", sess)
	}
}

func TestPublicTraceOmitsSessionWhenEmpty(t *testing.T) {
	pt := BuildPublicTrace(PublicTrace{TurnID: "turn_1", Answer: "ok", Status: "ok", Rounds: 1})
	if _, ok := pt["session"]; ok {
		t.Fatalf("session present: %v", pt["session"])
	}
}

func TestPublicTraceIncludesStamp(t *testing.T) {
	pt := BuildPublicTrace(PublicTrace{
		TurnID: "t",
		Answer: "ok",
		Status: "ok",
		Rounds: 1,
		Stamp:  map[string]any{"user": "zeus_client", "version": "2.1.0"},
	})
	if pt["user"] != "zeus_client" {
		t.Fatalf("user %v", pt["user"])
	}
	st, _ := pt["stamp"].(map[string]any)
	if st["user"] != "zeus_client" {
		t.Fatalf("stamp %v", st)
	}
}

func TestPublicTraceIncludesInject(t *testing.T) {
	pt := BuildPublicTrace(PublicTrace{
		TurnID:              "turn_1",
		Answer:              "ok",
		Status:              "ok",
		Rounds:              1,
		AIProcessResult:     true,
		AIProcessResultExit: "direct",
		Inject:              map[string]any{"has_scope_brief": true, "brief_sha12": "abc123def456"},
	})
	inj, _ := pt["inject"].(map[string]any)
	if inj["brief_sha12"] != "abc123def456" {
		t.Fatalf("%v", inj)
	}
}

func TestPublicTraceNeverPutsG2InAnswer(t *testing.T) {
	pt := BuildPublicTrace(PublicTrace{
		TurnID: "t",
		Answer: "hello",
		Status: "ok",
		Rounds: 1,
		LayerA: map[string]any{
			"wish_i_knew":           "secret g2 dump",
			"jail_break_attempt":    false,
			"hooks_jailbreak_score": 0.1,
		},
	})
	if pt["answer"] != "hello" {
		t.Fatalf("answer %v", pt["answer"])
	}
	if strings.Contains(fmt.Sprint(pt["answer"]), "wish_i_knew") {
		t.Fatal("G2 leaked into answer")
	}
	keys, _ := pt["artifacts_keys"].([]string)
	if len(keys) != 3 {
		t.Fatalf("artifacts_keys %v", pt["artifacts_keys"])
	}
}
