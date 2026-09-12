// SPDX-License-Identifier: BUSL-1.1

// Package observability is slog family events, metrics, and hop telemetry (P-Obs).
//
// ZCG-26 lands FamilyLogger (INFO/ERROR/DEBUG/TRACE, REDACT) and InMemoryMetrics.
// Hop telemetry hangs attrs on existing events (zeus_client.zeus.req,
// zeus_client.llm.request_finished, zeus_client.turn.finished). Do not invent
// zeus_client.hop.telemetry. Omit tokens.input=0 to mean "not an LLM hop".
// Omit memory.heap_bytes until stamped.
package observability
