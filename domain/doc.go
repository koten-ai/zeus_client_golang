// SPDX-License-Identifier: BUSL-1.1

// Package domain is the hexagonal core: ids, errors, contract, catalog,
// Layer A, policy, messages, journal, stamps.
//
// It must stay free of net/http and provider SDKs. Adapters depend inward.
package domain
