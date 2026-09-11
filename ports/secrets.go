// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// SecretStore looks up a secret by env-style name (Python SecretStorePort).
// Config stores names only; values are resolved at use time.
type SecretStore interface {
	Get(ctx context.Context, name string) (value string, ok bool)
}
