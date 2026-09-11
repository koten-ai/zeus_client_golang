// SPDX-License-Identifier: BUSL-1.1

// Package catalogfs is the filesystem CatalogStore (Python adapters.catalog_fs).
//
// Fail-closed path order is domain.ResolveCatalogPath. Load extracts the
// stamped contract hash and never invents one. Direct verbs are ZCG-13.
package catalogfs
