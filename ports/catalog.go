// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// CatalogStore is filesystem / cache catalog IO (Python CatalogStorePort).
// Fail-closed: missing pack is an error, not a guess. Adapter is ZCG-15.
type CatalogStore interface {
	Load(ctx context.Context, key CatalogKey) (CatalogDocument, error)
	Save(ctx context.Context, key CatalogKey, doc CatalogDocument) error
}
