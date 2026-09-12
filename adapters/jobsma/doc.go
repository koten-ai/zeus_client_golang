// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

// Package jobsma is Pattern A: in-process host that *links*
// koten_multi_agent_golang (Python has no equivalent — Python is Pattern B).
//
// Do not reimplement RunJob / replan / store / chaos here. Engine.RunJob owns
// errgroup + MaxWorkers semaphore, wall timeout, mid-wave cancel, and the
// plan-epoch fence (backend/memory.Store). WorkUnits call this client's
// Units.AgentTurn / ZeusDirect (isolated Client law).
//
// FakeJobRuntime (adapters/jobsfake) stays the sequential L4 seed.
// MATRIX demo is ZCG-33; this package is the docs-level seam.
package jobsma
