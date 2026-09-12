// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"errors"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// UnitRunOpts is shared kwargs for AgentTurn / ZeusDirect (Python run_*_unit).
// Cancel is ctx.Done() (Python cancel_event). Durable sessions stay off.
type UnitRunOpts struct {
	JobID            string
	Wave             int
	PlanEpoch        int
	JobModels        map[string]any
	Journal          journal.ExecutionJournal
	Clock            ports.Clock
	IDs              ports.IDFactory
	Config           config.RuntimeConfig
	LLM              ports.LlmPort
	Zeus             ports.ZeusPort
	Catalog          ports.CatalogStore
	Version          string
	Log              func(level, msg string, attrs map[string]any)
	LLMForSlice      func(config.ResolvedLlmSlice) ports.LlmPort
	FetchChatRequest ChatRequestFetch
	Middleware       *MiddlewareChain
}

func unitClockMS(ctx context.Context, clk ports.Clock) int64 {
	if clk == nil {
		return ports.SystemClock{}.NowMS(ctx)
	}
	return clk.NowMS(ctx)
}

func appendUnitEvent(j journal.ExecutionJournal, clk ports.Clock, typ, component, jobID, unitID string, data map[string]any) {
	if j == nil {
		return
	}
	payload := cloneAnyMap(data)
	payload["job_id"] = jobID
	payload["unit_id"] = unitID
	j.Append(journal.JournalEvent{
		EventID:   "evt_" + domain.NewZeusReqID()[:16],
		TsMs:      unitClockMS(context.Background(), clk),
		Type:      typ,
		Component: component,
		TurnID:    unitID,
		Data:      payload,
	})
}

func unitStatusFromCtx(ctx context.Context) (domain.UnitStatus, bool) {
	if ctx == nil {
		return "", false
	}
	err := ctx.Err()
	if err == nil {
		return "", false
	}
	switch {
	case errors.Is(err, context.Canceled):
		return domain.UnitStatusCancelled, true
	case errors.Is(err, context.DeadlineExceeded):
		return domain.UnitStatusTimeout, true
	default:
		if de := domain.FromContext(err); de != nil {
			if de.Code == domain.CodeCancelled {
				return domain.UnitStatusCancelled, true
			}
			if de.Code == domain.CodeContextDeadline {
				return domain.UnitStatusTimeout, true
			}
		}
		return domain.UnitStatusError, true
	}
}

func unitStatusFromErr(err error) (domain.UnitStatus, string, bool) {
	if err == nil {
		return "", "", false
	}
	if de := domain.FromContext(err); de != nil {
		switch de.Code {
		case domain.CodeCancelled:
			return domain.UnitStatusCancelled, string(de.Code), true
		case domain.CodeContextDeadline:
			return domain.UnitStatusTimeout, string(de.Code), true
		}
	}
	if de, ok := domain.AsError(err); ok {
		switch de.Code {
		case domain.CodeCancelled:
			return domain.UnitStatusCancelled, string(de.Code), true
		case domain.CodeContextDeadline:
			return domain.UnitStatusTimeout, string(de.Code), true
		}
		return domain.UnitStatusError, string(de.Code), true
	}
	return "", "", false
}

func cancelledUnit(unit domain.UnitConfig, epoch int) domain.UnitResult {
	return domain.UnitResult{UnitID: unit.UnitID, Status: domain.UnitStatusCancelled, PlanEpoch: epoch}
}

func recoverUnit(unit domain.UnitConfig, epoch int, rec any) domain.UnitResult {
	return domain.UnitResult{
		UnitID:    unit.UnitID,
		Status:    domain.UnitStatusError,
		ErrorCode: string(domain.CodeInternalBug),
		PlanEpoch: epoch,
		Artifacts: map[string]any{"panic": fmtPanic(rec)},
	}
}

func fmtPanic(rec any) string {
	if rec == nil {
		return "panic"
	}
	if e, ok := rec.(error); ok {
		return e.Error()
	}
	return "panic"
}
