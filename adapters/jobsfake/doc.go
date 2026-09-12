// SPDX-License-Identifier: BUSL-1.1

// Package jobsfake is a sequential L4 test host (Python adapters.jobs_fake).
//
// It runs units in input order via UnitRunner (api.UnitHost → UnitsAPI).
// No planner LLM. No errgroup (that is ZCG-29 / koten_multi_agent_golang).
// Not the product engine.
package jobsfake
