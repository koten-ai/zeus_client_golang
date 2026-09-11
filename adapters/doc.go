// SPDX-License-Identifier: BUSL-1.1

// Package adapters holds driven adapters (zeushttp, llmopenai, catalogfs,
// secretsenv, otlp, jobsfake). Domain must not import this package.
//
// ZCG-8 header helpers; ZCG-17 AuthResolver; ZCG-15 catalogfs; ZCG-13 Direct
// verbs (zeushttp.Port); ZCG-19/ZCG-16 SessionClient (create/turn/rehydrate/trace).
// Live HTTP is internal/httpx.
package adapters
