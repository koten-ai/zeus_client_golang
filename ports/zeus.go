// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"context"

	"github.com/koten-ai/zeus_client_golang/config"
)

// ZeusPort is the Zeus data-plane adapter (Python ZeusPort).
// ResolveAuth is implemented by adapters/zeushttp.AuthResolver (ZCG-17).
// CallVerb is ZCG-13.
type ZeusPort interface {
	ResolveAuth(ctx context.Context, target config.DataTarget, force bool) (AuthContext, error)
	CallVerb(ctx context.Context, req VerbRequest) (VerbHopResult, error)
}
