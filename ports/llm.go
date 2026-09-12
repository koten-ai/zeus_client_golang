// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// LlmPort is the completion adapter (Python LlmPort.complete).
// OpenAI-compatible client is adapters/llmopenai (ZCG-14).
// Complete must not mutate req.Messages or req.Tools.
type LlmPort interface {
	Complete(ctx context.Context, req LlmRequest) (LlmResponse, error)
}
