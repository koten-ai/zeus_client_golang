// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"math/rand"
	"strings"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

// Idempotent read verbs may SDK-retry transport / HTTP 5xx (API_ZEUS §6.6).
var idempotentReadVerbs = map[string]struct{}{
	"describe": {},
	"explain":  {},
	"get":      {},
	"find":     {},
	"search":   {},
	"traverse": {},
}

// VerbAllowsSDKRetry reports whether Mode 1 may auto-retry this verb.
func VerbAllowsSDKRetry(verb string) bool {
	_, ok := idempotentReadVerbs[strings.TrimSpace(verb)]
	return ok
}

// ZeusHopRetryable is transport (status 0) or HTTP 5xx on an idempotent read.
// Never 4xx / 409 / Zeus 429 (trail may still say retryable_later for the LLM).
func ZeusHopRetryable(verb string, status int, transportErr bool) bool {
	if !VerbAllowsSDKRetry(verb) {
		return false
	}
	if transportErr || status == 0 {
		return true
	}
	return status >= 500 && status <= 599
}

func (p *Port) budgetForCall() domain.RetryBudget {
	if p == nil {
		return domain.NewRetryBudget(0)
	}
	if p.budget != nil {
		b := *p.budget
		b.AttemptsUsed = 0
		b.MSUsed = 0
		if b.MaxExtraMS == 0 {
			b.MaxExtraMS = 30_000
		}
		return b
	}
	return domain.NewRetryBudget(p.retry.MaxAttempts)
}

func backoffMS(policy config.RetryPolicy, attempt int) int {
	shift := attempt
	if shift < 0 {
		shift = 0
	}
	if shift > 16 {
		shift = 16
	}
	base := policy.BaseDelayMS * (1 << shift)
	if policy.MaxDelayMS > 0 && base > policy.MaxDelayMS {
		base = policy.MaxDelayMS
	}
	if policy.Jitter && base > 0 {
		base = int(float64(base) * (0.5 + rand.Float64()*0.5))
	}
	if base < 0 {
		return 0
	}
	return base
}
