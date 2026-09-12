// SPDX-License-Identifier: BUSL-1.1

// Package adapters holds driven adapters (zeushttp, llmopenai, catalogfs,
// secretsenv, jobsfake, jobsma, jobshttp). Domain must not import this package.
// OTLP exporter is residual (family slog + REDACT only; see BLOCKED.md).
//
// ZCG-8 header helpers; ZCG-17 AuthResolver; ZCG-15 catalogfs; ZCG-13 Direct
// verbs (zeushttp.Port); ZCG-19/ZCG-16 SessionClient (create/turn/rehydrate/trace);
// ZCG-14 llmopenai (OpenAI-compat + classify). Live HTTP is internal/httpx.
// ZCG-39 jobsfake (sequential L4 seed). ZCG-29 jobsma (Pattern A Engine.RunJob).
// ZCG-37 jobshttp (SSE WatchJob from_seq / Last-Event-ID).
package adapters
