// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
)

func TestCorrelationHeadersFullAgent(t *testing.T) {
	h := CorrelationHeaders(Correlation{
		ChatID:     "chat-1",
		TurnID:     "turn-2",
		CallID:     "call-3",
		Mode:       "analytics",
		ForceTrace: true,
		TraceClass: TraceClassAgent,
	})
	if h["X-Zeus-Chat-Id"] != "chat-1" || h["X-Zeus-Turn-Id"] != "turn-2" || h["X-Zeus-Call-Id"] != "call-3" {
		t.Fatalf("%v", h)
	}
	if h["X-Zeus-Mode"] != "analytics" || h["X-Zeus-Trace"] != "1" || h["X-Zeus-Trace-Class"] != "agent" {
		t.Fatalf("%v", h)
	}
	if _, ok := h["X-Zeus-Req-Id"]; ok {
		t.Fatal("must not set X-Zeus-Req-Id")
	}
	if _, ok := h["X-Zeus-Session"]; ok {
		t.Fatal("must not set X-Zeus-Session")
	}
	if _, ok := h["X-Zeus-Chat-Session-Id"]; ok {
		t.Fatal("chat session")
	}
}

func TestCorrelationHeadersChatSessionAndSha12(t *testing.T) {
	h := CorrelationHeaders(Correlation{
		ChatSessionID: "sess_20260903T120000_000001",
		BriefSha12:    "3e77b8258d09",
		MiniSha12:     "5b4692baf06f",
	})
	if h["X-Zeus-Chat-Session-Id"] != "sess_20260903T120000_000001" {
		t.Fatal("session")
	}
	if h["X-Zeus-Brief-Sha12"] != "3e77b8258d09" || h["X-Zeus-Mini-Sha12"] != "5b4692baf06f" {
		t.Fatalf("%v", h)
	}
	if _, ok := h["X-Zeus-Session"]; ok {
		t.Fatal("session stamp")
	}
	if _, ok := h["X-Zeus-Req-Id"]; ok {
		t.Fatal("req id")
	}
}

func TestCorrelationHeadersOmitsEmpty(t *testing.T) {
	if len(CorrelationHeaders(Correlation{})) != 0 {
		t.Fatal("empty")
	}
	h := CorrelationHeaders(Correlation{ChatID: "c"})
	if len(h) != 1 || h["X-Zeus-Chat-Id"] != "c" {
		t.Fatalf("%v", h)
	}
}

func TestTraceClassClosedSet(t *testing.T) {
	if TraceClassAgent != "agent" || TraceClassSession != "session" {
		t.Fatal("class")
	}
	if TraceClassDirectInteractive != "direct.interactive" || TraceClassDirectRead != "direct.read" {
		t.Fatal("direct")
	}
}

func TestForceTraceFalseDoesNotSetHeader(t *testing.T) {
	h := ApplyForceTraceHeader(map[string]string{"X-Zeus-Mode": "analytics"}, false)
	if _, ok := h["X-Zeus-Trace"]; ok {
		t.Fatal("trace")
	}
}

func TestModeHeaderDoesNotOverwrite(t *testing.T) {
	h := ApplyModeHeader(map[string]string{"X-Zeus-Mode": "research"}, "analytics")
	if h["X-Zeus-Mode"] != "research" {
		t.Fatalf("%v", h)
	}
}

func TestRewindQueryParamsOffByDefault(t *testing.T) {
	if len(RewindQueryParams(false)) != 0 {
		t.Fatal("off")
	}
	on := RewindQueryParams(true)
	if on["rewind"] != "true" {
		t.Fatalf("%v", on)
	}
}

func TestVerbBodyWithoutRewindStripsFlag(t *testing.T) {
	got := VerbBodyWithoutRewind(map[string]any{"entity_type": "Beer", "rewind": true})
	if _, ok := got["rewind"]; ok {
		t.Fatal("rewind leaked")
	}
	if got["entity_type"] != "Beer" {
		t.Fatalf("%v", got)
	}
	if len(VerbBodyWithoutRewind(nil)) != 0 {
		t.Fatal("nil")
	}
}

func TestProductStampHeaders(t *testing.T) {
	h := ProductStampHeaders("0.1.0-dev")
	if h["X-Zeus-Client"] != ProductUser {
		t.Fatalf("%v", h)
	}
	if h["X-Zeus-Client-Version"] != "0.1.0-dev" {
		t.Fatalf("%v", h)
	}
}

func TestStampHeadersHubAdmin(t *testing.T) {
	h := StampHeaders(domain.HubUser, "0.1.0-dev")
	if h["X-Zeus-Client"] != domain.HubUser {
		t.Fatalf("%v", h)
	}
	p := StampHeaders("", "0.1.0-dev")
	if p["X-Zeus-Client"] != ProductUser {
		t.Fatalf("default %v", p)
	}
}

func TestMergeHeadersLaterWins(t *testing.T) {
	h := MergeHeaders(
		map[string]string{"A": "1", "B": "2"},
		map[string]string{"B": "3"},
	)
	if h["A"] != "1" || h["B"] != "3" {
		t.Fatalf("%v", h)
	}
}
