// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// SameZeusHost reports whether unit and process Zeus URLs are the same host
// (trailing slash ignored). Empty either side → true (Python same_zeus_host).
func SameZeusHost(unitURL, processURL string) bool {
	left := strings.TrimRight(unitURL, "/")
	right := strings.TrimRight(processURL, "/")
	if left == "" || right == "" {
		return true
	}
	return left == right
}

// VerbOverrides is the per-unit Zeus URL / auth-name slice (Python verb_overrides).
// Values are env *names* only.
type VerbOverrides struct {
	BaseURL     string
	AuthMode    string
	PasswordEnv string
	TokenEnv    string
	Username    string
}

// UnitVerbOverrides copies unit transport fields.
func UnitVerbOverrides(unit domain.UnitConfig) VerbOverrides {
	return VerbOverrides{
		BaseURL:     unit.ZeusURL,
		AuthMode:    unit.AuthMode,
		PasswordEnv: unit.PasswordEnv,
		TokenEnv:    unit.TokenEnv,
		Username:    unit.Username,
	}
}

// PatchVerbRequest stamps a hop with the unit URL/auth names (Python patch_verb_request).
// Empty unit fields keep the request value (agent path). Direct units pass
// overrides on DataVerbOptions instead so auth_mode=none does not inherit process basic.
func PatchVerbRequest(req ports.VerbRequest, unit domain.UnitConfig) ports.VerbRequest {
	over := UnitVerbOverrides(unit)
	if over.BaseURL != "" {
		req.BaseURL = over.BaseURL
	}
	if over.AuthMode != "" {
		req.AuthMode = over.AuthMode
	}
	if over.PasswordEnv != "" {
		req.PasswordEnv = over.PasswordEnv
	}
	if over.TokenEnv != "" {
		req.TokenEnv = over.TokenEnv
	}
	if over.Username != "" {
		req.Username = over.Username
	}
	return req
}

// UnitScopedZeusPort wraps ZeusPort and stamps each hop with the unit slice.
type UnitScopedZeusPort struct {
	Inner ports.ZeusPort
	Unit  domain.UnitConfig
}

var _ ports.ZeusPort = UnitScopedZeusPort{}

// ResolveAuth forwards to the inner port.
func (p UnitScopedZeusPort) ResolveAuth(ctx context.Context, target config.DataTarget, force bool) (ports.AuthContext, error) {
	if p.Inner == nil {
		return ports.AuthContext{}, domain.New(domain.CodeNotImplemented, "application.units_transport",
			domain.WithMessage("Zeus port not wired on runtime"))
	}
	return p.Inner.ResolveAuth(ctx, target, force)
}

// CallVerb stamps the unit URL/auth names then forwards.
func (p UnitScopedZeusPort) CallVerb(ctx context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	if p.Inner == nil {
		return ports.VerbHopResult{}, domain.New(domain.CodeNotImplemented, "application.units_transport",
			domain.WithMessage("Zeus port not wired on runtime"))
	}
	return p.Inner.CallVerb(ctx, PatchVerbRequest(req, p.Unit))
}

func unitDataTarget(unit domain.UnitConfig) config.DataTarget {
	return config.DataTarget{
		Bucket:     unit.Bucket,
		Scope:      unit.Scope,
		Collection: unit.Collection,
	}
}
