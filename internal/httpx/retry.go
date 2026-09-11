// SPDX-License-Identifier: BUSL-1.1

package httpx

import "strings"

// ShouldRetry is the P-HTTP retry policy (API_RESULT §3). The transport does
// not auto-retry: callers (auth remint, LLM backoff) decide. 4xx is never
// retried here — one 401 remint is ZCG-17, not a transport loop.
func ShouldRetry(method string, status int) bool {
	if status >= 400 && status < 500 {
		return false
	}
	if status >= 500 && status <= 599 {
		m := strings.ToUpper(strings.TrimSpace(method))
		return m == "GET" || m == "HEAD"
	}
	return false
}
