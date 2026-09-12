// SPDX-License-Identifier: BUSL-1.1

// Package conformance is the offline suite adapter (G8 / ZCG-23).
//
// Same suite_version as sdk_bootstrap.pins.json. Handlers drive existing
// domain / application APIs and must not copy case.expect into observe.
// Claim stays candidate; this package does not award supported.
package conformance
