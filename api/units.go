// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"

	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/observability"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const unitsAPIComponent = "api.units"

// UnitsOptions constructs UnitsAPI (Python UnitsAPI).
type UnitsOptions struct {
	LLM              ports.LlmPort
	Zeus             ports.ZeusPort
	Journal          journal.ExecutionJournal
	Catalog          ports.CatalogStore
	Config           config.RuntimeConfig
	Version          string
	Clock            ports.Clock
	IDs              ports.IDFactory
	Log              func(level, msg string, attrs map[string]any)
	Metrics          observability.MetricsPort
	LLMForSlice      func(config.ResolvedLlmSlice) ports.LlmPort
	FetchChatRequest application.ChatRequestFetch
	Middleware       *application.MiddlewareChain
}

// UnitCallParams is AgentTurn / ZeusDirect kwargs (Python job_id, wave, plan_epoch, job_models).
type UnitCallParams struct {
	JobID     string
	Wave      int
	PlanEpoch int
	JobModels map[string]any
}

// UnitsAPI is the Mode 3 WorkUnit facade (Python UnitsAPI). Layer C — not the job engine.
type UnitsAPI struct {
	host any
	opts UnitsOptions
}

// NewUnitsAPI binds a Client (or test fake) as the facade host.
func NewUnitsAPI(host any) *UnitsAPI {
	if host == nil {
		return nil
	}
	return &UnitsAPI{host: host}
}

// NewUnitsAPIWith binds host plus ports/config (Client.Units).
func NewUnitsAPIWith(host any, opts UnitsOptions) *UnitsAPI {
	return &UnitsAPI{host: host, opts: opts}
}

// Host is the bound Client. Nil-safe.
func (u *UnitsAPI) Host() any {
	if u == nil {
		return nil
	}
	return u.host
}

func (u *UnitsAPI) runOpts(params UnitCallParams) application.UnitRunOpts {
	mw := u.opts.Middleware
	if mw == nil {
		mw = application.DefaultMiddlewareChain()
	}
	return application.UnitRunOpts{
		JobID:            params.JobID,
		Wave:             params.Wave,
		PlanEpoch:        params.PlanEpoch,
		JobModels:        params.JobModels,
		Journal:          u.opts.Journal,
		Clock:            u.opts.Clock,
		IDs:              u.opts.IDs,
		Config:           u.opts.Config,
		LLM:              u.opts.LLM,
		Zeus:             u.opts.Zeus,
		Catalog:          u.opts.Catalog,
		Version:          u.opts.Version,
		Log:              u.opts.Log,
		LLMForSlice:      u.opts.LLMForSlice,
		FetchChatRequest: u.opts.FetchChatRequest,
		Middleware:       mw,
	}
}

// AgentTurn is units.agent_turn — isolated Mode 1. Pure-data units dispatch to ZeusDirect.
func (u *UnitsAPI) AgentTurn(ctx context.Context, unit domain.UnitConfig, params UnitCallParams) (domain.UnitResult, error) {
	if u == nil {
		return domain.UnitResult{}, domain.New(domain.CodeNotImplemented, unitsAPIComponent,
			domain.WithMessage("Units API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if unit.Kind == domain.UnitKindZeusDirect {
		return u.ZeusDirect(ctx, unit, params)
	}
	return application.RunAgentUnit(ctx, unit, u.runOpts(params))
}

// ZeusDirect is units.zeus_direct — isolated Mode 2. Agent-kind units are 130002.
func (u *UnitsAPI) ZeusDirect(ctx context.Context, unit domain.UnitConfig, params UnitCallParams) (domain.UnitResult, error) {
	if u == nil {
		return domain.UnitResult{}, domain.New(domain.CodeNotImplemented, unitsAPIComponent,
			domain.WithMessage("Units API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if unit.Kind == domain.UnitKindAgentTurn {
		return domain.UnitResult{}, domain.NewJob(domain.CodeJobsInvalidUnitMap, unitsAPIComponent)
	}
	return application.RunDirectUnit(ctx, unit, u.runOpts(params))
}
