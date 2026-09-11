// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"context"

	"github.com/koten-ai/zeus_client_golang/domain"
)

// IDFactory mints client ids (Python ports.IdFactory).
// Never mint session.id. Never reuse a hop req_id as chat.id.
type IDFactory interface {
	TurnID(ctx context.Context) string
	ChatID(ctx context.Context) string
	CallID(ctx context.Context) string
	SpanID(ctx context.Context) string
}

// UUIDFactory wraps domain.IDFactory with context (Python UuidIdFactory).
// A nil Inner uses domain.UUIDFactory.
type UUIDFactory struct {
	Inner domain.IDFactory
}

var _ IDFactory = UUIDFactory{}

func (f UUIDFactory) inner() domain.IDFactory {
	if f.Inner != nil {
		return f.Inner
	}
	return domain.UUIDFactory{}
}

func (f UUIDFactory) TurnID(_ context.Context) string { return f.inner().TurnID() }
func (f UUIDFactory) ChatID(_ context.Context) string { return f.inner().ChatID() }
func (f UUIDFactory) CallID(_ context.Context) string { return f.inner().CallID() }
func (f UUIDFactory) SpanID(_ context.Context) string { return f.inner().SpanID() }
