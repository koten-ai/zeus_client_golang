// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestTurnIDRoundTrip(t *testing.T) {
	id, err := ParseTurnID("turn_abc")
	if err != nil {
		t.Fatal(err)
	}
	if string(id) != "turn_abc" {
		t.Fatalf("%q", id)
	}
	if id != TurnID("turn_abc") {
		t.Fatal("type round-trip")
	}
}

func TestAllIDWrappersRoundTrip(t *testing.T) {
	turn, err := ParseTurnID("t1")
	if err != nil || string(turn) != "t1" {
		t.Fatalf("TurnID %q %v", turn, err)
	}
	chat, err := ParseChatID("c1")
	if err != nil || string(chat) != "c1" {
		t.Fatalf("ChatID %q %v", chat, err)
	}
	call, err := ParseCallID("call1")
	if err != nil || string(call) != "call1" {
		t.Fatalf("CallID %q %v", call, err)
	}
	sess, err := ParseSessionID("sid1")
	if err != nil || string(sess) != "sid1" {
		t.Fatalf("SessionID %q %v", sess, err)
	}
	req, err := ParseReqID("req1")
	if err != nil || string(req) != "req1" {
		t.Fatalf("ReqID %q %v", req, err)
	}
}

func TestMintTurnIDPrefixAndNonempty(t *testing.T) {
	id := MintTurnID("turn_")
	if !strings.HasPrefix(string(id), "turn_") {
		t.Fatalf("%q", id)
	}
	if len(id) <= len("turn_") {
		t.Fatalf("short %q", id)
	}
	if IsUUIDv4(string(id)) {
		t.Fatal("prefixed id is not a Zeus req_id")
	}
}

func TestIDWrappersRejectEmpty(t *testing.T) {
	if _, err := ParseTurnID(""); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := ParseReqID("   "); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("ws: %v", err)
	}
}

func TestNewZeusReqIDIsUUIDv4(t *testing.T) {
	rid := NewZeusReqID()
	if !IsUUIDv4(rid) {
		t.Fatalf("%q", rid)
	}
	ids := make(map[string]struct{}, 10000)
	for i := 0; i < 10000; i++ {
		id := NewZeusReqID()
		if !IsUUIDv4(id) {
			t.Fatalf("not v4: %q", id)
		}
		ids[id] = struct{}{}
	}
	if len(ids) != 10000 {
		t.Fatalf("unique %d", len(ids))
	}
}

func TestUUIDFactoryMintsV4(t *testing.T) {
	var fac UUIDFactory
	if !IsUUIDv4(fac.TurnID()) {
		t.Fatal("turn")
	}
	if !IsUUIDv4(fac.ChatID()) {
		t.Fatal("chat")
	}
	if !IsUUIDv4(fac.CallID()) {
		t.Fatal("call")
	}
	if len(NewTraceID()) != 32 {
		t.Fatalf("trace %d", len(NewTraceID()))
	}
	span := fac.SpanID()
	if len(span) != 16 {
		t.Fatalf("span %q", span)
	}
}

func TestNeverReuseReqIDAsChatID(t *testing.T) {
	var fac UUIDFactory
	seen := make(map[string]string, 4000)
	for i := 0; i < 1000; i++ {
		req := string(MintReqID())
		chat := fac.ChatID()
		turn := fac.TurnID()
		call := fac.CallID()
		if req == chat || req == turn || chat == turn {
			t.Fatalf("reused hop id as chat/turn req=%s chat=%s turn=%s", req, chat, turn)
		}
		for kind, id := range map[string]string{"req": req, "chat": chat, "turn": turn, "call": call} {
			if prev, ok := seen[id]; ok {
				t.Fatalf("collision %s and %s on %s", prev, kind, id)
			}
			seen[id] = kind
		}
	}
}

func TestIsUUIDv4RejectsOtherShapes(t *testing.T) {
	for _, s := range []string{
		"",
		"not-a-uuid",
		"16bd540b-5e26-1b2a-87ba-4727af320127", // version 1
		"16BD540B-5E26-4B2A-87BA-4727AF320127", // uppercase
		NewTraceID(),                           // 32-hex, not hyphenated v4
	} {
		if IsUUIDv4(s) {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestNoMintSessionID(t *testing.T) {
	// Session ids are server-minted. The factory has no SessionID method.
	var fac IDFactory = UUIDFactory{}
	_ = fac
	_, err := ParseSessionID("server-minted-sid")
	if err != nil {
		t.Fatal(err)
	}
}
