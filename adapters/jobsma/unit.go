// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

package jobsma

import (
	"context"
	"fmt"

	"github.com/koten-ai/koten_multi_agent_golang/workunit"
	"github.com/koten-ai/zeus_client_golang/domain"
)

// UnitRunner is AgentTurn / ZeusDirect (api.UnitsAPI / api.UnitHost).
// Defined here so this adapter does not import api (hexagon + test cycle).
type UnitRunner interface {
	AgentTurn(ctx context.Context, unit domain.UnitConfig, jobID string, jobModels map[string]any) (domain.UnitResult, error)
	ZeusDirect(ctx context.Context, unit domain.UnitConfig, jobID string) (domain.UnitResult, error)
}

// clientUnit is one WorkUnit that runs isolated Client law.
type clientUnit struct {
	cfg    domain.UnitConfig
	host   UnitRunner
	models map[string]any
}

var _ workunit.WorkUnit = (*clientUnit)(nil)

func (u *clientUnit) ID() string { return u.cfg.UnitID }

func (u *clientUnit) Brief() workunit.Brief {
	kind := string(u.cfg.Kind)
	if kind == "" {
		kind = string(domain.UnitKindZeusDirect)
	}
	return workunit.Brief{
		UnitID:      u.cfg.UnitID,
		Title:       u.cfg.Goal,
		Description: u.cfg.Goal,
		Allow:       map[string]string{"kind": kind},
	}
}

func (u *clientUnit) Run(ctx context.Context, in workunit.Input, progress workunit.ProgressSink) (workunit.Result, error) {
	if progress == nil {
		progress = workunit.NopProgress{}
	}
	progress.Report("run", 0, u.cfg.UnitID)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return workunit.Result{Error: err.Error()}, err
	}

	var res domain.UnitResult
	var err error
	if u.cfg.Kind == domain.UnitKindAgentTurn {
		res, err = u.host.AgentTurn(ctx, u.cfg, in.JobID, u.models)
	} else {
		res, err = u.host.ZeusDirect(ctx, u.cfg, in.JobID)
	}
	if err != nil {
		code := ""
		if de, ok := domain.AsError(err); ok {
			code = string(de.Code)
		}
		return workunit.Result{
			Error:   err.Error(),
			Payload: minUnitPayload(res, code),
		}, err
	}

	payload := minUnitPayload(res, res.ErrorCode)
	out := workunit.Result{
		Payload: payload,
		DeadEnd: res.DeadEnd || res.Status == domain.UnitStatusDeadEnd,
	}
	switch res.Status {
	case domain.UnitStatusCancelled:
		return out, context.Canceled
	case domain.UnitStatusTimeout:
		return out, context.DeadlineExceeded
	case domain.UnitStatusError, domain.UnitStatusDeadEnd:
		out.Error = res.ErrorCode
		if out.Error == "" {
			out.Error = string(res.Status)
		}
		// Engine treats (error, nil payload) OR a returned err as unit failure.
		// Keep payload for store; return err so status is not OK.
		return out, fmt.Errorf("%s", out.Error)
	default:
		return out, nil
	}
}

func minUnitPayload(res domain.UnitResult, errCode string) map[string]any {
	var code any
	if errCode != "" {
		code = errCode
	}
	out := map[string]any{
		"unit_id":    res.UnitID,
		"status":     string(res.Status),
		"error_code": code,
	}
	if res.Answer != "" {
		out["answer"] = res.Answer
	}
	if len(res.ReqIDs) > 0 {
		out["req_ids"] = append([]string(nil), res.ReqIDs...)
	}
	return out
}
