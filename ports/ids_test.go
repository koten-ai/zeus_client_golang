// SPDX-License-Identifier: BUSL-1.1

package ports

import (
	"context"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
)

func TestUUIDFactoryTurnIDIsV4(t *testing.T) {
	var f UUIDFactory
	id := f.TurnID(context.Background())
	if !domain.IsUUIDv4(id) {
		t.Fatalf("TurnID %q", id)
	}
	if !domain.IsUUIDv4(f.ChatID(context.Background())) {
		t.Fatal("ChatID")
	}
	if !domain.IsUUIDv4(f.CallID(context.Background())) {
		t.Fatal("CallID")
	}
	span := f.SpanID(context.Background())
	if len(span) != 16 {
		t.Fatalf("SpanID len %d %q", len(span), span)
	}
}

func TestUUIDFactoryWrapsInner(t *testing.T) {
	inner := stubIDs{}
	f := UUIDFactory{Inner: inner}
	if got := f.TurnID(context.Background()); got != "turn-1" {
		t.Fatalf("%q", got)
	}
}

type stubIDs struct{}

func (stubIDs) TurnID() string { return "turn-1" }
func (stubIDs) ChatID() string { return "chat-1" }
func (stubIDs) CallID() string { return "call-1" }
func (stubIDs) SpanID() string { return "span-1" }
