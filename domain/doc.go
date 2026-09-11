// SPDX-License-Identifier: BUSL-1.1

// Package domain is the hexagonal core: ids, errors, contract, catalog,
// Layer A, policy, messages, journal, stamps.
//
// It must stay free of net/http and provider SDKs. Adapters depend inward.
//
// ZCG-10 lands typed IDs and the family ErrorCode catalogue (Python
// zeus_client.domain.errors / ids). ZCG-9 lands domain/journal. ZCG-18
// lands contract hash oracles + product stamps (never invent production
// hashes). Catalog FS is ZCG-15.
package domain
