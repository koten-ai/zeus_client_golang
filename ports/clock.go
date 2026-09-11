// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"context"
	"time"
)

// Clock is wall + monotonic millis (Python ports.Clock).
type Clock interface {
	NowMS(ctx context.Context) int64
	MonotonicMS(ctx context.Context) int64
}

// monoOrigin is an arbitrary start for MonotonicMS. time.Since uses the
// monotonic clock; this is not a process-global Client.
var monoOrigin = time.Now()

// SystemClock is the production clock (Python ports.clock.SystemClock).
type SystemClock struct{}

var _ Clock = SystemClock{}

// NowMS is Unix epoch milliseconds.
func (SystemClock) NowMS(_ context.Context) int64 {
	return time.Now().UnixMilli()
}

// MonotonicMS is milliseconds since process clock origin (not wall time).
func (SystemClock) MonotonicMS(_ context.Context) int64 {
	return time.Since(monoOrigin).Milliseconds()
}
