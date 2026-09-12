// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnitConfigPublicDictOmitsSecrets(t *testing.T) {
	u := UnitConfig{
		UnitID:      "u1",
		Kind:        UnitKindAgentTurn,
		ZeusURL:     "http://127.0.0.1:8080",
		Bucket:      "beer-sample",
		Scope:       "sales",
		Collection:  "_default",
		Goal:        "shortlist fruit beers",
		CatalogMode: "analytics",
		BaseID:      "base-5.3",
		LLM: map[string]any{
			"model":       "grok",
			"api_key":     "sk-secret",
			"api_key_env": "XAI_API_KEY",
		},
	}
	if u.Kind != UnitKindAgentTurn {
		t.Fatalf("kind %q", u.Kind)
	}
	d := u.ToPublicDict()
	blob := strings.ToLower(fmt.Sprint(d))
	if strings.Contains(blob, "password") {
		t.Fatalf("public dict leaked password: %v", d)
	}
	if _, ok := d["api_key"]; ok {
		t.Fatal("top-level api_key")
	}
	llm, _ := d["llm"].(map[string]any)
	if _, ok := llm["api_key"]; ok {
		t.Fatal("llm.api_key must be stripped")
	}
	if llm["api_key_env"] != "XAI_API_KEY" {
		t.Fatalf("api_key_env: %v", llm["api_key_env"])
	}
	if d["share_session_id"] != false {
		t.Fatalf("share_session_id bool: %v", d["share_session_id"])
	}
	tgt := u.Target()
	if tgt.Bucket != "beer-sample" || tgt.Scope != "sales" || tgt.Collection != "_default" {
		t.Fatalf("target %+v", tgt)
	}
}

func TestJobEventIsMinified(t *testing.T) {
	ev := JobEvent{Seq: 1, Type: "job.started", JobID: "j1", TSMS: 1, Payload: map[string]any{"status": "accepted"}}
	if ev.UnitID != "" {
		t.Fatalf("unit_id %q", ev.UnitID)
	}
	d := ev.ToPublicDict()
	if d["unit_id"] != nil {
		t.Fatalf("public unit_id %v", d["unit_id"])
	}
	if _, ok := d["password"]; ok {
		t.Fatal("password key on event")
	}
	blob := fmt.Sprint(d)
	if strings.Contains(strings.ToLower(blob), "password") {
		t.Fatalf("password in event: %v", d)
	}
}

func TestInvalidBudgetIs130003(t *testing.T) {
	b := DefaultJobBudgets()
	b.MaxWorkers = 0
	requireCode(t, b.Validate(), CodeJobsBudgetInvalid)

	if err := DefaultJobBudgets().Validate(); err != nil {
		t.Fatal(err)
	}
	zeroWall := DefaultJobBudgets()
	zeroWall.WallMS = 0
	requireCode(t, zeroWall.Validate(), CodeJobsBudgetInvalid)
	zeroWaves := DefaultJobBudgets()
	zeroWaves.MaxWaves = 0
	requireCode(t, zeroWaves.Validate(), CodeJobsBudgetInvalid)
}

func TestValidateUnitMap(t *testing.T) {
	direct := func(id, scope, share string) UnitConfig {
		return UnitConfig{
			UnitID:         id,
			Kind:           UnitKindZeusDirect,
			Goal:           "x",
			ZeusURL:        "http://z",
			Bucket:         "b",
			Scope:          scope,
			Collection:     "c",
			ShareSessionID: share,
		}
	}
	agent := func(id, scope, catalogMode, baseID string, chat map[string]any) UnitConfig {
		return UnitConfig{
			UnitID:      id,
			Kind:        UnitKindAgentTurn,
			Goal:        "x",
			ZeusURL:     "http://z",
			Bucket:      "b",
			Scope:       scope,
			Collection:  "c",
			CatalogMode: catalogMode,
			BaseID:      baseID,
			ChatRequest: chat,
		}
	}

	tests := []struct {
		name string
		in   []UnitConfig
		code Code
	}{
		{name: "empty_map", code: CodeJobsInvalidUnitMap},
		{
			name: "missing_scope_130002",
			in: []UnitConfig{{
				UnitID: "u1", Kind: UnitKindZeusDirect, Goal: "x", ZeusURL: "http://z",
			}},
			code: CodeJobsInvalidUnitMap,
		},
		{
			name: "agent_missing_catalog_130011",
			in:   []UnitConfig{agent("u1", "s", "", "", nil)},
			code: CodeUnitsCatalogMissing,
		},
		{
			name: "shared_session_130013",
			in:   []UnitConfig{direct("u1", "s", "sess_shared"), direct("u2", "east", "sess_shared")},
			code: CodeUnitsIsolation,
		},
		{
			name: "duplicate_unit_id_130002",
			in:   []UnitConfig{direct("u1", "s", ""), direct("u1", "east", "")},
			code: CodeJobsInvalidUnitMap,
		},
		{
			name: "http_json_not_implemented",
			in: []UnitConfig{{
				UnitID: "u1", Kind: UnitKindHTTPJSON, Goal: "x", ZeusURL: "http://z",
				Bucket: "b", Scope: "s", Collection: "c",
			}},
			code: CodeNotImplemented,
		},
		{
			name: "unknown_kind_130002",
			in: []UnitConfig{{
				UnitID: "u1", Kind: "pack_desk", Goal: "x", ZeusURL: "http://z",
				Bucket: "b", Scope: "s", Collection: "c",
			}},
			code: CodeJobsInvalidUnitMap,
		},
		{
			name: "valid_two_scope",
			in: []UnitConfig{
				direct("u1", "s", ""),
				agent("u2", "east", "analytics", "", nil),
			},
		},
		{
			name: "single_share_session_ok",
			in:   []UnitConfig{direct("u1", "s", "sess_one")},
		},
		{
			name: "agent_base_id_only",
			in:   []UnitConfig{agent("u1", "s", "", "base-5.3", nil)},
		},
		{
			name: "agent_chat_request_only",
			in:   []UnitConfig{agent("u1", "s", "", "", map[string]any{})},
		},
		{
			name: "distinct_share_session_ok",
			in:   []UnitConfig{direct("u1", "s", "sess_a"), direct("u2", "east", "sess_b")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUnitMap(tc.in)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("valid map: %v", err)
				}
				return
			}
			requireCode(t, err, tc.code)
		})
	}
}

func TestJobHandleAcceptedDefault(t *testing.T) {
	h := JobHandle{JobID: "j1", Status: JobStatusAccepted}
	if h.Status != "accepted" || h.Seq != 0 {
		t.Fatalf("%+v", h)
	}
}

func TestUnitResultStatuses(t *testing.T) {
	want := []UnitStatus{UnitStatusOK, UnitStatusError, UnitStatusTimeout, UnitStatusCancelled, UnitStatusDeadEnd}
	got := []string{string(want[0]), string(want[1]), string(want[2]), string(want[3]), string(want[4])}
	if strings.Join(got, ",") != "ok,error,timeout,cancelled,dead_end" {
		t.Fatal(got)
	}
}
