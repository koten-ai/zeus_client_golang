// SPDX-License-Identifier: BUSL-1.1

// Package domain is the hexagonal core: ids, errors, contract, catalog,
// Layer A, policy, messages, journal, stamps.
//
// It must stay free of net/http and provider SDKs. Adapters depend inward.
//
// ZCG-10 lands typed IDs and the family ErrorCode catalogue (Python
// zeus_client.domain.errors / ids). Later tickets fill contract, catalog,
// Layer A, policy, and journal.
package domain
