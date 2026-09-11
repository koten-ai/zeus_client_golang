// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// LlmPort is the completion adapter (Python LlmPort.complete).
// OpenAI-compatible client is ZCG-14. Interface only on this ticket.
type LlmPort interface {
	Complete(ctx context.Context, req LlmRequest) (LlmResponse, error)
}
