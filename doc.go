// SPDX-License-Identifier: BUSL-1.1

// Package zeusclient is the native Go Zeus Client.
//
// Same Client law as zeus_client_python (hexagonal domain / ports / adapters).
// Construct with New and release with Close. Config is a snapshot.
// Zeus / Agent / Catalog / Session / Jobs / Units / Debug are real facades.
// Import:
//
//	import zeusclient "github.com/koten-ai/zeus_client_golang"
//
// Claim level is candidate until a human MATRIX row. Never treat this
// package as supported from CI green alone.
package zeusclient
