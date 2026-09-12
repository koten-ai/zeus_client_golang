// SPDX-License-Identifier: BUSL-1.1

// Package jobshttp is Pattern B WatchJob: GET /v1/jobs/{id}/events?from_seq=
// plus Last-Event-ID. Speaks the Go JobEvent wire so mixed-language UIs work.
//
// Runtime implements ports.Jobs. Watch consumes SSE. Run / Get / Cancel stay
// 130001 until the sidecar grows those routes (Python HttpxJobRuntime).
// Handler serves the same wire for Pattern B consumers.
//
// This is not the in-process engine. Do not reimplement RunJob / replan /
// store / chaos. SSE is not durable SoT.
package jobshttp
