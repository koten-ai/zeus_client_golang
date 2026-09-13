// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import "testing"

func TestVerbAllowsSDKRetry(t *testing.T) {
	for _, v := range []string{"find", "get", "search", "describe", "explain", "traverse"} {
		if !VerbAllowsSDKRetry(v) {
			t.Fatalf("%s should allow SDK retry", v)
		}
	}
	for _, v := range []string{"set", "pipeline", "return", "order", "enrich", "project", "analyze"} {
		if VerbAllowsSDKRetry(v) {
			t.Fatalf("%s must not SDK-retry", v)
		}
	}
}

func TestZeusHopRetryable(t *testing.T) {
	if !ZeusHopRetryable("find", 503, false) {
		t.Fatal("find 503")
	}
	if !ZeusHopRetryable("find", 0, true) {
		t.Fatal("find transport")
	}
	if ZeusHopRetryable("find", 409, false) {
		t.Fatal("409")
	}
	if ZeusHopRetryable("find", 429, false) {
		t.Fatal("zeus 429 is not SDK-retried")
	}
	if ZeusHopRetryable("set", 503, false) {
		t.Fatal("set 503")
	}
	if ZeusHopRetryable("pipeline", 503, true) {
		t.Fatal("pipeline")
	}
}
