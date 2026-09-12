// SPDX-License-Identifier: BUSL-1.1

// Package application holds use-cases: agent turn, data verbs, typeahead,
// catalog sync, session lifecycle, and projectors.
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
// (redacted journal / span tree) attached on TurnResult.Debug.
package application
