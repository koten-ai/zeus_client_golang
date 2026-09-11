// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"context"

	"github.com/koten-ai/zeus_client_golang/config"
)

// ZeusPort is the Zeus data-plane adapter (Python ZeusPort).
// Auth mint is ZCG-17; verbs are ZCG-13. Interface only on this ticket.
type ZeusPort interface {
	ResolveAuth(ctx context.Context, target config.DataTarget, force bool) (AuthContext, error)
	CallVerb(ctx context.Context, req VerbRequest) (VerbHopResult, error)
}
