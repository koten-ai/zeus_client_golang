// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const unitsAgentComponent = "application.units_agent"

func hasRequiredInject(chatRequest map[string]any) bool {
	if domain.ExtractScopeBrief(chatRequest) != "" {
		return true
	}
	text := chatRequestText(chatRequest)
	return strings.Contains(text, "## MINI-SCHEMA")
}

func chatRequestText(cr map[string]any) string {
	if cr == nil {
		return ""
	}
	var b strings.Builder
	switch msgs := cr["messages"].(type) {
	case []any:
		if len(msgs) > 0 {
			if m, ok := msgs[0].(map[string]any); ok {
				b.WriteString(asString(m["content"]))
			}
		}
	case []map[string]any:
		if len(msgs) > 0 {
			b.WriteString(asString(msgs[0]["content"]))
		}
	}
	if instr, ok := cr["instructions"].(map[string]any); ok && instr != nil {
		b.WriteString(asString(instr["system_prompt"]))
	}
	return b.String()
}

func loadUnitCatalog(ctx context.Context, unit domain.UnitConfig, opts UnitRunOpts) (map[string]any, error) {
	if opts.Catalog == nil {
		return nil, domain.NewJob(domain.CodeUnitsCatalogMissing, unitsAgentComponent)
	}
	mode := unit.CatalogMode
	if mode == "" {
		mode = opts.Config.Settings.Mode
	}
	doc, err := opts.Catalog.Load(ctx, ports.CatalogKey{
		Mode:   mode,
		Bucket: unit.Bucket,
		Scope:  unit.Scope,
		BaseID: unit.BaseID,
	})
	if err != nil {
		return nil, domain.NewJob(domain.CodeUnitsCatalogMissing, unitsAgentComponent, domain.WithCause(err))
	}
	return cloneAnyMap(doc.Body), nil
}

// RunAgentUnit is one isolated Mode 1 unit — worker LLM, no shared session
// (Python run_agent_unit). Durable sessions forced off.
func RunAgentUnit(ctx context.Context, unit domain.UnitConfig, opts UnitRunOpts) (out domain.UnitResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out = recoverUnit(unit, opts.PlanEpoch, rec)
			err = nil
		}
	}()
	if err := domain.ValidateUnitMap([]domain.UnitConfig{unit}); err != nil {
		return domain.UnitResult{}, err
	}
	if unit.Kind != domain.UnitKindAgentTurn {
		return domain.UnitResult{}, domain.NewJob(domain.CodeJobsInvalidUnitMap, unitsAgentComponent)
	}
	if st, ok := unitStatusFromCtx(ctx); ok {
		return domain.UnitResult{UnitID: unit.UnitID, Status: st, PlanEpoch: opts.PlanEpoch}, nil
	}

	chatRequest := cloneAnyMap(unit.ChatRequest)
	if unit.ChatRequest == nil {
		loaded, loadErr := loadUnitCatalog(ctx, unit, opts)
		if loadErr != nil {
			return domain.UnitResult{}, loadErr
		}
		chatRequest = loaded
	}

	processURL := opts.Config.Zeus.URL
	if !hasRequiredInject(chatRequest) {
		if unit.ZeusURL != "" && !SameZeusHost(unit.ZeusURL, processURL) {
			return domain.UnitResult{}, domain.NewJob(domain.CodeUnitsInjectMissing, unitsAgentComponent)
		}
		mode := unit.CatalogMode
		if mode == "" {
			mode = opts.Config.Settings.Mode
		}
		merged := EnsureScopeBrief(ctx, chatRequest, opts.FetchChatRequest, unit.Bucket, unit.Scope, mode)
		chatRequest = merged.Body
		if !hasRequiredInject(chatRequest) {
			return domain.UnitResult{}, domain.NewJob(domain.CodeUnitsInjectMissing, unitsAgentComponent)
		}
	}

	jobs := opts.Config.Jobs
	slice := config.ResolveLlmSlice(opts.Config.LLM, config.ResolveLlmOpts{
		Role:      config.LlmRoleWorker,
		Jobs:      &jobs,
		JobModels: opts.JobModels,
		UnitLLM:   unit.LLM,
		UnitID:    unit.UnitID,
	})
	payload := map[string]any{
		"wave":        opts.Wave,
		"plan_epoch":  opts.PlanEpoch,
		"llm.role":    slice.Role,
		"llm.model":   slice.Model,
		"api_key_env": slice.APIKeyEnv,
	}
	appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitStarted, unitsAgentComponent, opts.JobID, unit.UnitID, payload)

	var session *domain.SessionHandle
	if unit.ShareSessionID != "" {
		session = &domain.SessionHandle{SessionID: unit.ShareSessionID, Enabled: true}
	}

	settings := opts.Config.Settings
	settings.DurableSessions = false
	if unit.CatalogMode != "" {
		settings.Mode = unit.CatalogMode
	}
	if unit.MaxRounds > 0 {
		settings.MaxRounds = unit.MaxRounds
	}

	zeus := opts.Zeus
	if zeus != nil {
		zeus = UnitScopedZeusPort{Inner: zeus, Unit: unit}
	}
	llm := opts.LLM
	if opts.LLMForSlice != nil {
		if next := opts.LLMForSlice(slice); next != nil {
			llm = next
		}
	}

	chatID := opts.JobID
	if chatID == "" {
		chatID = unit.UnitID
	}

	result := RunAgentTurn(ctx, TurnRequest{
		Message:        unit.Goal,
		Target:         unitDataTarget(unit),
		Settings:       settings,
		Session:        session,
		ChatRequest:    chatRequest,
		BaseID:         unit.BaseID,
		ChatID:         chatID,
		Model:          slice.Model,
		EnableSessions: false,
	}, RunAgentTurnOpts{
		LLM:              llm,
		Zeus:             zeus,
		Journal:          opts.Journal,
		Middleware:       opts.Middleware,
		DefaultSettings:  settings,
		DebugPolicy:      opts.Config.Debug,
		ZeusURL:          unit.ZeusURL,
		ClientFloor:      opts.Config.ClientFloor,
		IDs:              opts.IDs,
		Version:          opts.Version,
		Log:              opts.Log,
		ContextWindow:    opts.Config.LLM.ContextWindowTokens,
		ContextSoftLimit: opts.Config.LLM.ContextSoftLimit,
	})

	if result.Err != nil {
		st, code, mapped := unitStatusFromErr(result.Err)
		if !mapped {
			st = domain.UnitStatusError
			code = string(result.Err.Code)
			if code == "" {
				code = string(domain.CodeAgentTurnFailed)
			}
		}
		appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitFinished, unitsAgentComponent, opts.JobID, unit.UnitID, map[string]any{
			"wave": opts.Wave, "plan_epoch": opts.PlanEpoch, "status": string(st),
		})
		return domain.UnitResult{
			UnitID:    unit.UnitID,
			Status:    st,
			ErrorCode: code,
			PlanEpoch: opts.PlanEpoch,
		}, nil
	}

	reqIDs := result.Debug.ReqIDs
	if reqIDs == nil {
		reqIDs = []string{}
	}
	status := domain.UnitStatusOK
	errCode := ""
	artifacts := map[string]any{"session_id": nil}
	if result.Session != nil && result.Session.SessionID != "" {
		artifacts["session_id"] = result.Session.SessionID
	}
	usage := result.Debug.Tokens
	if usageHasBilling(usage) {
		copied := make(map[string]any, len(usage))
		for k, v := range usage {
			copied[k] = v
		}
		artifacts["usage"] = copied
	}
	lastReq := any(nil)
	if len(reqIDs) > 0 {
		lastReq = reqIDs[len(reqIDs)-1]
	}
	appendUnitEvent(opts.Journal, opts.Clock, journal.EventUnitFinished, unitsAgentComponent, opts.JobID, unit.UnitID, map[string]any{
		"wave": opts.Wave, "plan_epoch": opts.PlanEpoch, "status": string(status), "req_id": lastReq,
	})
	return domain.UnitResult{
		UnitID:    unit.UnitID,
		Status:    status,
		Answer:    result.Answer,
		ReqIDs:    reqIDs,
		Artifacts: artifacts,
		ErrorCode: errCode,
		PlanEpoch: opts.PlanEpoch,
	}, nil
}

func usageHasBilling(usage map[string]any) bool {
	if usage == nil {
		return false
	}
	if ok, _ := usage["ok"].(bool); ok {
		return true
	}
	return tokenAsInt(usage["prompt"]) != 0 || tokenAsInt(usage["completion"]) != 0 || tokenAsInt(usage["total"]) != 0
}
