// SPDX-License-Identifier: BUSL-1.1

package domain

import "testing"

func TestContractStatusFromRehydrateMatch(t *testing.T) {
	if ContractStatusFromRehydrate("cid", "md5:aaa", map[string]any{"hash": "md5:aaa", "contract_id": "cid"}) != "match" {
		t.Fatal("match")
	}
}

func TestContractStatusFromRehydrateDrift(t *testing.T) {
	if ContractStatusFromRehydrate("cid", "md5:want", map[string]any{"hash": "md5:other", "contract_id": "cid"}) != "drift" {
		t.Fatal("drift")
	}
}

func TestContractStatusFromRehydrateNone(t *testing.T) {
	if ContractStatusFromRehydrate("", "", map[string]any{}) != "none" {
		t.Fatal("none")
	}
}

func TestContractStatusIncompleteHashesPreferMatch(t *testing.T) {
	if ContractStatusFromRehydrate("cid", "", map[string]any{"contract_id": "cid", "hash": ""}) != "match" {
		t.Fatal("incomplete")
	}
}

func TestNormalizeContractStatus(t *testing.T) {
	if NormalizeContractStatus("ok") != "match" {
		t.Fatal("ok")
	}
	if NormalizeContractStatus("") != "none" {
		t.Fatal("empty")
	}
	if NormalizeContractStatus("mismatch") != "mismatch" {
		t.Fatal("mismatch")
	}
}

func TestSessionHandleWithUpdates(t *testing.T) {
	h := SessionHandle{SessionID: "sid", Round: 1, Created: true}
	r := 2
	created := false
	out := h.WithUpdates(SessionHandleUpdates{Round: &r, Created: &created})
	if out.Round != 2 || out.Created || out.SessionID != "sid" {
		t.Fatalf("%+v", out)
	}
	if h.Round != 1 || !h.Created {
		t.Fatal("original mutated")
	}
}

func TestSessionHandleDoesNotInventID(t *testing.T) {
	var h SessionHandle
	if h.SessionID != "" {
		t.Fatal("zero handle must not carry a session id")
	}
}
