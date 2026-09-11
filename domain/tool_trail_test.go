// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"strings"
	"testing"
)

func TestErrorClassFor(t *testing.T) {
	if ErrorClassFor(true, 200, "") != "" {
		t.Fatal("ok")
	}
	if ErrorClassFor(false, 409, "") != "contract_mismatch" {
		t.Fatal("409")
	}
	if ErrorClassFor(false, 400, "") != "do_not_retry_same_args" {
		t.Fatal("400")
	}
	if ErrorClassFor(false, 429, "") != "retryable_later" {
		t.Fatal("429")
	}
	if ErrorClassFor(false, 0, "timeout waiting") != "retryable_later" {
		t.Fatal("timeout")
	}
	if ErrorClassFor(false, 500, "") != "zeus_error" {
		t.Fatal("500")
	}
}

func TestTrailEntryAndRender(t *testing.T) {
	entry := TrailEntryFromHop(map[string]any{
		"ok": false, "status": 409, "verb": "find", "req_id": "req-409", "url": "/v2/x/find",
	}, 1, false)
	if entry["error_class"] != "contract_mismatch" || entry["req_id"] != "req-409" {
		t.Fatalf("%v", entry)
	}
	if entry["l0"] != "zeus.find" {
		t.Fatalf("l0 %v", entry["l0"])
	}
	snip := RenderTrailInject([]map[string]any{entry}, 16)
	if !strings.Contains(snip, TrailHeading) || !strings.Contains(snip, "req_id=req-409") {
		t.Fatalf("%s", snip)
	}
}

func TestUpsertTrailOnSystemDoesNotTouchUser(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "system", "content": "sys\n\n## SCOPE BRIEF\nscope x"},
		map[string]any{"role": "user", "content": "do not use pipeline"},
	}
	UpsertTrailOnSystem(msgs, RenderTrailInject([]map[string]any{
		TrailEntryFromHop(map[string]any{"ok": true, "status": 200, "verb": "find", "req_id": "r1"}, 1, false),
	}, 16))
	sys := msgs[0].(map[string]any)["content"].(string)
	user := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(sys, TrailHeading) {
		t.Fatalf("sys %s", sys)
	}
	if user != "do not use pipeline" {
		t.Fatalf("user mutated: %q", user)
	}
	UpsertTrailOnSystem(msgs, RenderTrailInject([]map[string]any{
		TrailEntryFromHop(map[string]any{"ok": false, "status": 409, "verb": "search", "req_id": "r2"}, 2, false),
	}, 16))
	sys2 := msgs[0].(map[string]any)["content"].(string)
	if strings.Count(sys2, TrailHeading) != 1 {
		t.Fatalf("trail not replaced: %s", sys2)
	}
	if strings.Contains(sys2, "r1") {
		t.Fatal("old trail")
	}
	if !strings.Contains(sys2, "r2") {
		t.Fatal("new trail")
	}
}
