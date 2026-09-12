// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
)

const unitsDirectComponent = "application.units_direct"

// RunDirectUnit is one isolated Mode 2 unit — no catalog, no LLM (Python run_direct_unit).
func RunDirectUnit(ctx context.Context, unit domain.UnitConfig, opts UnitRunOpts) (out domain.UnitResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out = recoverUnit(unit, opts.PlanEpoch, rec)
			err = nil
		}
	}()
	if err := domain.ValidateUnitMap([]domain.UnitConfig{unit}); err != nil {
		return domain.UnitResult{}, err
	}
	if unit.Kind != domain.UnitKindZeusDirect {
		return domain.UnitResult{}, domain.NewJob(domain.CodeJobsInvalidUnitMap, unitsDirectComponent)
	}
	if st, ok := unitStatusFromCtx(ctx); ok {
		return domain.UnitResult{UnitID: unit.UnitID, Status: st, PlanEpoch: opts.PlanEpoch}, nil
	}

	call := unit.Call
	verb := ""
	if call != nil {
		verb = strings.TrimSpace(asString(call["verb"]))
	}
	if verb == "" {
		return domain.UnitResult{}, domain.NewJob(domain.CodeJobsInvalidUnitMap, unitsDirectComponent)
	}
	var body map[string]any
	if call != nil && call["body"] != nil {
		m, ok := call["body"].(map[string]any)
		if !ok {
			return domain.UnitResult{}, domain.NewJob(domain.CodeJobsInvalidUnitMap, unitsDirectComponent)
		}
		body = m
	}
	if body == nil {
		body = map[string]any{}
	}

	payload := map[string]any{"wave": opts.Wave, "plan_epoch": opts.PlanEpoch, "verb": verb}
	chatID := opts.JobID
	if chatID == "" {
		chatID = unit.UnitID
	}
	appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitStarted, unitsDirectComponent, opts.JobID, unit.UnitID, payload)

	over := UnitVerbOverrides(unit)
	mode := strings.TrimSpace(opts.Config.Settings.Mode)
	result, hopErr := RunDataVerb(ctx, opts.Zeus, verb, body, DataVerbOptions{
		Target:      unitDataTarget(unit),
		ModeHeader:  mode,
		ChatID:      chatID,
		TurnID:      unit.UnitID,
		BaseURL:     over.BaseURL,
		AuthMode:    over.AuthMode,
		PasswordEnv: over.PasswordEnv,
		TokenEnv:    over.TokenEnv,
		Username:    over.Username,
		Rewind:      opts.Config.Debug.Rewind,
		ForceTrace:  opts.Config.Settings.ForceTrace,
	})
	if hopErr != nil {
		if st, code, ok := unitStatusFromErr(hopErr); ok && (st == domain.UnitStatusCancelled || st == domain.UnitStatusTimeout) {
			appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitFinished, unitsDirectComponent, opts.JobID, unit.UnitID, map[string]any{
				"wave": opts.Wave, "plan_epoch": opts.PlanEpoch, "verb": verb, "status": string(st),
			})
			return domain.UnitResult{UnitID: unit.UnitID, Status: st, ErrorCode: code, PlanEpoch: opts.PlanEpoch}, nil
		}
		code := string(domain.CodeZeusTransport)
		if de, ok := domain.AsError(hopErr); ok {
			code = string(de.Code)
		}
		appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitFinished, unitsDirectComponent, opts.JobID, unit.UnitID, map[string]any{
			"wave": opts.Wave, "plan_epoch": opts.PlanEpoch, "verb": verb, "status": "error",
		})
		return domain.UnitResult{
			UnitID:    unit.UnitID,
			Status:    domain.UnitStatusError,
			ErrorCode: code,
			PlanEpoch: opts.PlanEpoch,
		}, nil
	}

	reqIDs := result.ReqIDs
	if len(reqIDs) == 0 && result.ReqID != "" {
		reqIDs = []string{result.ReqID}
	}
	status := domain.UnitStatusOK
	errCode := ""
	if !result.OK {
		status = domain.UnitStatusError
		errCode = string(domain.CodeZeusHTTP4xx)
		if result.Error != "" {
			errCode = result.Error
		}
	}
	lastReq := any(nil)
	if len(reqIDs) > 0 {
		lastReq = reqIDs[len(reqIDs)-1]
	}
	appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitFinished, unitsDirectComponent, opts.JobID, unit.UnitID, map[string]any{
		"wave": opts.Wave, "plan_epoch": opts.PlanEpoch, "verb": verb, "status": string(status), "req_id": lastReq,
	})
	bodyOut := result.Body
	if bodyOut == nil {
		bodyOut = map[string]any{}
	}
	return domain.UnitResult{
		UnitID:    unit.UnitID,
		Status:    status,
		ReqIDs:    reqIDs,
		Artifacts: map[string]any{"body": cloneAnyMap(bodyOut)},
		ErrorCode: errCode,
		PlanEpoch: opts.PlanEpoch,
	}, nil
}
