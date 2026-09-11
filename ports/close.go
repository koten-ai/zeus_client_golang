// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// Closer is an optional adapter shutdown hook (Python aclose / shutdown).
// HttpPort includes Close; Zeus/LLM/Jobs may implement this without it
// being on the port interface.
type Closer interface {
	Close(ctx context.Context) error
}
