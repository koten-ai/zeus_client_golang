// SPDX-License-Identifier: BUSL-1.1

// Package ports defines Zeus, LLM, catalog, secrets, clock, ids, HTTP, and jobs
// interfaces. Every method takes context.Context first (ZCG-11).
//
// ZCG-12 lands SecretStore (env names resolved at use time). Remaining ports
// are ZCG-11.
package ports
