// SPDX-License-Identifier: BUSL-1.1

// Package httpx is the shared HTTP transport (timeouts, req_id, no process-global client).
//
// Each Client owns a private *http.Client and *http.Transport (no process-global
// client or transport). Requests use http.NewRequestWithContext so cancel
// aborts in-flight hops. Default Zeus timeout is 30s. Capture X-Zeus-Req-Id
// on every response including 4xx/5xx.
package httpx
