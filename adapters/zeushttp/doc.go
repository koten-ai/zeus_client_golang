// SPDX-License-Identifier: BUSL-1.1

// Package zeushttp is the Zeus HTTP adapter (Python adapters.zeus_http).
//
// ZCG-8 header helpers + req_id capture. ZCG-17 AuthResolver (none / basic /
// bearer / session; certificate is NOT_IMPLEMENTED). ZCG-13 Port dispatches
// Direct V2 verbs (pipeline rejected on the public surface). Idempotent
// reads retry transport/5xx via RetryBudget (API_ZEUS §6.6). ZCG-19
// SessionClient is /v2/session create / turn / rehydrate / trace (ZCG-16).
// Live HTTP lives in internal/httpx — inject ports.HttpPort.
package zeushttp
