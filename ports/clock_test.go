// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"context"
	"testing"
	"time"
)

func TestSystemClockNowMS(t *testing.T) {
	var c SystemClock
	n := c.NowMS(context.Background())
	if n <= 0 {
		t.Fatalf("NowMS %d", n)
	}
	wall := time.Now().UnixMilli()
	if n > wall+5000 || wall-n > 5000 {
		t.Fatalf("NowMS %d vs wall %d", n, wall)
	}
}

func TestSystemClockMonotonicNondecreasing(t *testing.T) {
	var c SystemClock
	a := c.MonotonicMS(context.Background())
	time.Sleep(2 * time.Millisecond)
	b := c.MonotonicMS(context.Background())
	if b < a {
		t.Fatalf("monotonic decreased %d → %d", a, b)
	}
}
