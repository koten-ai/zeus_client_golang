// SPDX-License-Identifier: BUSL-1.1

package observability

import "testing"

func TestTypeaheadBurstOneSecondCallDenied(t *testing.T) {
	lim := NewTokenBucketLimiter()
	lim.Configure("typeahead", 1, 1)
	if !lim.Allow("typeahead", 1) {
		t.Fatal("first")
	}
	if lim.Allow("typeahead", 1) {
		t.Fatal("burst exhausted")
	}
}

func TestLimiterMissingSurfaceAllows(t *testing.T) {
	lim := NewTokenBucketLimiter()
	if !lim.Allow("typeahead", 1) {
		t.Fatal("no bucket")
	}
	var nilLim *TokenBucketLimiter
	if !nilLim.Allow("typeahead", 1) {
		t.Fatal("nil")
	}
}
