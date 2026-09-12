// SPDX-License-Identifier: BUSL-1.1

// Package domain is the hexagonal core: ids, errors, contract, catalog,
// Layer A, policy, messages, journal, stamps.
//
// It must stay free of net/http and provider SDKs. Adapters depend inward.
//
// ZCG-10 lands typed IDs and the family ErrorCode catalogue (Python
// zeus_client.domain.errors / ids). ZCG-9 lands domain/journal. ZCG-18
// lands contract hash oracles + product stamps (never invent production
// hashes). ZCG-15 lands catalog path/lineage/mini_schema/floor (fail-closed).
// ZCG-20 lands named rules freeze, tool-trail Bag B, and inject proof
// (BRIEF/MINI sha12). ZCG-19 lands SessionHandle (server-minted id).
// ZCG-14 lands LLM classify (050010–050018). Later-wins llm roles live in config.
// ZCG-22 lands Layer A parse/peel + policy.decide (G2 never in answer).
// Control-plane splice is application.
package domain
