// SPDX-License-Identifier: BUSL-1.1

package observability

import (
	"sync"
	"time"
)

// TokenBucket is a thread-safe token bucket (Python observability.rate_limit).
// rate tokens/sec, capacity burst.
type TokenBucket struct {
	rate    float64
	burst   float64
	tokens  float64
	updated time.Time
	mu      sync.Mutex
}

// NewTokenBucket constructs a full bucket. rate and burst must be > 0.
func NewTokenBucket(rate, burst float64) *TokenBucket {
	if rate <= 0 || burst <= 0 {
		return nil
	}
	return &TokenBucket{
		rate:    rate,
		burst:   burst,
		tokens:  burst,
		updated: time.Now(),
	}
}

// Allow consumes cost tokens when available.
func (b *TokenBucket) Allow(cost float64) bool {
	if b == nil {
		return true
	}
	if cost <= 0 {
		return true
	}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed := now.Sub(b.updated).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	b.updated = now
	b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
	if b.tokens >= cost {
		b.tokens -= cost
		return true
	}
	return false
}

// TokenBucketLimiter is named surface → bucket (e.g. "typeahead").
type TokenBucketLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
}

// NewTokenBucketLimiter constructs an empty limiter (missing surface → allow).
func NewTokenBucketLimiter() *TokenBucketLimiter {
	return &TokenBucketLimiter{buckets: map[string]*TokenBucket{}}
}

// Configure replaces the bucket for surface.
func (l *TokenBucketLimiter) Configure(surface string, rate, burst float64) {
	if l == nil || surface == "" {
		return
	}
	b := NewTokenBucket(rate, burst)
	if b == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*TokenBucket{}
	}
	l.buckets[surface] = b
}

// Allow is true when the surface has no bucket or the bucket has tokens.
func (l *TokenBucketLimiter) Allow(surface string, cost float64) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	b := l.buckets[surface]
	l.mu.Unlock()
	if b == nil {
		return true
	}
	return b.Allow(cost)
}
