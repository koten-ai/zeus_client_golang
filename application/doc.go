// SPDX-License-Identifier: BUSL-1.1

// Package application holds use-cases: agent turn, data verbs, typeahead,
// catalog brief (live SCOPE BRIEF merge), session lifecycle, and projectors.
// Live catalog sync is residual (see BLOCKED.md).
//
// ZCG-13 lands RunDataVerb (no public pipeline) and RunTypeaheadSearch
// (Direct only — never agent.Run). ZCG-20 lands hash-excluded Bag B inject
// (control plane + tool-path policy). ZCG-19 lands SessionLifecycle
// (create / continue / rehydrate; server-minted session.id). ZCG-16 lands
// application/projectors (session-trace join + public_trace). ZCG-24 lands
// RunAgentTurn (sequential bags A–D; G2 never in answer; ctx cancel 000006).
// ZCG-25 lands SecurityHooks + jailbreak inspect on the default chain.
// ZCG-26 lands provider token rollup (application/tokens.go).
// ZCG-27 lands Detective projector (application/detective) + debug_export
// (redacted journal / span tree). Attached on TurnResult.Debug when hub
// profile / StampUser=admin / debug.detective_briefing=true (package default off).
// ZCG-31 lands isolated Units AgentTurn / ZeusDirect (ctx cancel; durable
// sessions off; no job engine).
package application
