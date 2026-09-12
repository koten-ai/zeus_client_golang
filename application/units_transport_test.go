// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"testing"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func TestSameZeusHostNormalizesSlash(t *testing.T) {
	if !SameZeusHost("http://z:8080/", "http://z:8080") {
		t.Fatal("slash")
	}
	if SameZeusHost("http://z-b:8080", "http://z:8080") {
		t.Fatal("distinct")
	}
	if !SameZeusHost("", "http://z:8080") {
		t.Fatal("empty unit")
	}
}

type recZeusPort struct {
	calls []ports.VerbRequest
	auth  int
}

func (r *recZeusPort) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	r.auth++
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recZeusPort) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.calls = append(r.calls, req)
	return ports.VerbHopResult{OK: true, StatusCode: 200, ReqID: "req-1", Body: map[string]any{}}, nil
}

func TestUnitScopedPortForwardsResolveAuthAndStampsURL(t *testing.T) {
	inner := &recZeusPort{}
	unit := domain.UnitConfig{UnitID: "u1", ZeusURL: "http://zeus-b:8080", AuthMode: "none"}
	port := UnitScopedZeusPort{Inner: inner, Unit: unit}
	if _, err := port.ResolveAuth(context.Background(), config.DataTarget{}, false); err != nil {
		t.Fatal(err)
	}
	if inner.auth != 1 {
		t.Fatalf("auth %d", inner.auth)
	}
	_, err := port.CallVerb(context.Background(), ports.VerbRequest{Verb: "find", BaseURL: "http://process:8080"})
	if err != nil {
		t.Fatal(err)
	}
	if inner.calls[0].BaseURL != "http://zeus-b:8080" {
		t.Fatalf("url %q", inner.calls[0].BaseURL)
	}
	if inner.calls[0].AuthMode != "none" {
		t.Fatalf("auth %q", inner.calls[0].AuthMode)
	}
}
