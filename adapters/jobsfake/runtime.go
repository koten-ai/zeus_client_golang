// SPDX-License-Identifier: BUSL-1.1

package jobsfake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// UnitRunner is AgentTurn / ZeusDirect (api.UnitsAPI / api.UnitHost).
// Defined here so this adapter does not import api (hexagon + test cycle).
type UnitRunner interface {
	AgentTurn(ctx context.Context, unit domain.UnitConfig, jobID string, jobModels map[string]any) (domain.UnitResult, error)
	ZeusDirect(ctx context.Context, unit domain.UnitConfig, jobID string) (domain.UnitResult, error)
}

const fakeJobsComponent = "adapters.jobs_fake"

// Runtime is sequential FakeJobRuntime (Python adapters.jobs_fake.FakeJobRuntime).
// Tests only. Calls UnitsAPI in input order; does not share bags across units.
type Runtime struct {
	units UnitRunner
	clock ports.Clock

	mu        sync.Mutex
	closed    bool
	snaps     map[string]ports.JobSnapshot
	events    map[string][]ports.JobEvent
	cancels   map[string]context.CancelFunc
	cancelled map[string]bool
}

var _ ports.Jobs = (*Runtime)(nil)
var _ ports.Closer = (*Runtime)(nil)

// New binds a UnitRunner host (Python FakeJobRuntime(rt) → UnitsAPI).
// Pass api.UnitHost{Units: client.Units()}. Clock defaults to SystemClock.
func New(units UnitRunner) *Runtime {
	return &Runtime{
		units:     units,
		clock:     ports.SystemClock{},
		snaps:     map[string]ports.JobSnapshot{},
		events:    map[string][]ports.JobEvent{},
		cancels:   map[string]context.CancelFunc{},
		cancelled: map[string]bool{},
	}
}

func (r *Runtime) nowMS(ctx context.Context) int64 {
	if r == nil || r.clock == nil {
		return ports.SystemClock{}.NowMS(ctx)
	}
	return r.clock.NowMS(ctx)
}

func newJobID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "job_" + hex.EncodeToString(b[:])
}

func clonePayload(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func unitsFromRequest(req map[string]any) []domain.UnitConfig {
	if req == nil {
		return nil
	}
	switch v := req["units"].(type) {
	case []domain.UnitConfig:
		out := make([]domain.UnitConfig, len(v))
		copy(out, v)
		return out
	default:
		return nil
	}
}

func modelsFromRequest(req map[string]any) map[string]any {
	if req == nil {
		return nil
	}
	m, ok := req["models"].(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	return clonePayload(m)
}

func goalFromRequest(req map[string]any) string {
	if req == nil {
		return ""
	}
	s, _ := req["goal"].(string)
	return s
}

func notFound() *domain.Error {
	return domain.NewJob(domain.CodeJobsNotFound, fakeJobsComponent)
}

func (r *Runtime) emit(ctx context.Context, jobID, typ string, payload map[string]any) ports.JobEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	seq := len(r.events[jobID]) + 1
	ev := ports.JobEvent{
		Seq:     seq,
		Type:    typ,
		JobID:   jobID,
		TSMS:    r.nowMS(ctx),
		Payload: clonePayload(payload),
	}
	r.events[jobID] = append(r.events[jobID], ev)
	return ev
}

func (r *Runtime) isCancelled(jobID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled[jobID]
}

// Run executes units sequentially via UnitsAPI. No planner LLM.
func (r *Runtime) Run(ctx context.Context, request map[string]any) (ports.JobHandle, error) {
	if r == nil {
		return ports.JobHandle{}, domain.NewJob(domain.CodeJobsUnavailable, fakeJobsComponent)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	jobID := newJobID()
	units := unitsFromRequest(request)
	models := modelsFromRequest(request)
	jobCtx, cancel := context.WithCancel(ctx)

	r.mu.Lock()
	r.cancels[jobID] = cancel
	r.events[jobID] = nil
	r.cancelled[jobID] = false
	r.snaps[jobID] = ports.JobSnapshot{JobID: jobID, Status: "running", Seq: 0}
	r.mu.Unlock()

	r.emit(jobCtx, jobID, journal.EventJobStarted, map[string]any{"goal": goalFromRequest(request)})

	if r.units == nil && len(units) > 0 {
		cancel()
		return ports.JobHandle{}, domain.NewJob(domain.CodeJobsUnavailable, fakeJobsComponent)
	}

	summaries := make([]map[string]any, 0, len(units))
	anyOK := false
	anyErr := false

	for _, unit := range units {
		if r.isCancelled(jobID) || jobCtx.Err() != nil {
			break
		}
		var result domain.UnitResult
		var err error
		if unit.Kind == domain.UnitKindAgentTurn {
			result, err = r.units.AgentTurn(jobCtx, unit, jobID, models)
		} else {
			result, err = r.units.ZeusDirect(jobCtx, unit, jobID)
		}
		if err != nil {
			cancel()
			return ports.JobHandle{}, err
		}
		if result.Status == domain.UnitStatusOK {
			anyOK = true
		} else {
			anyErr = true
		}
		var errCode any
		if result.ErrorCode != "" {
			errCode = result.ErrorCode
		}
		summaries = append(summaries, map[string]any{
			"unit_id":    result.UnitID,
			"status":     string(result.Status),
			"error_code": errCode,
		})
	}

	cancelled := r.isCancelled(jobID) || jobCtx.Err() != nil
	status := "ok"
	switch {
	case cancelled:
		status = "cancelled"
	case anyErr && anyOK:
		status = "partial"
	case anyErr:
		status = "error"
	}
	finished := r.emit(jobCtx, jobID, journal.EventJobFinished, map[string]any{"status": status})

	r.mu.Lock()
	if r.cancelled[jobID] {
		status = "cancelled"
	}
	r.snaps[jobID] = ports.JobSnapshot{
		JobID:         jobID,
		Status:        status,
		Seq:           finished.Seq,
		Partial:       status == "partial",
		UnitSummaries: summaries,
	}
	r.mu.Unlock()

	return ports.JobHandle{JobID: jobID, Status: "running", Seq: 1}, nil
}

// Watch snapshots events with seq > afterSeq and closes the channel.
func (r *Runtime) Watch(ctx context.Context, jobID string, afterSeq int) (<-chan ports.JobEvent, error) {
	if r == nil {
		return nil, domain.NewJob(domain.CodeJobsUnavailable, fakeJobsComponent)
	}
	r.mu.Lock()
	events, ok := r.events[jobID]
	if !ok {
		r.mu.Unlock()
		return nil, notFound()
	}
	var buf []ports.JobEvent
	for _, ev := range events {
		if ev.Seq > afterSeq {
			copied := ev
			copied.Payload = clonePayload(ev.Payload)
			buf = append(buf, copied)
		}
	}
	r.mu.Unlock()
	ch := make(chan ports.JobEvent, len(buf))
	for _, ev := range buf {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// Get is the current snapshot. Unknown job → 130004.
func (r *Runtime) Get(ctx context.Context, jobID string) (ports.JobSnapshot, error) {
	if r == nil {
		return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsUnavailable, fakeJobsComponent)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snap, ok := r.snaps[jobID]
	if !ok {
		return ports.JobSnapshot{}, notFound()
	}
	return cloneSnapshot(snap), nil
}

// Cancel is best-effort. Unknown job → 130004.
func (r *Runtime) Cancel(ctx context.Context, jobID string) (ports.JobSnapshot, error) {
	if r == nil {
		return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsUnavailable, fakeJobsComponent)
	}
	r.mu.Lock()
	cancel, ok := r.cancels[jobID]
	if !ok {
		r.mu.Unlock()
		return ports.JobSnapshot{}, notFound()
	}
	r.cancelled[jobID] = true
	prev := r.snaps[jobID]
	snap := ports.JobSnapshot{
		JobID:         jobID,
		Status:        "cancelled",
		Seq:           prev.Seq,
		Partial:       prev.Partial,
		UnitSummaries: prev.UnitSummaries,
		Answer:        prev.Answer,
	}
	r.snaps[jobID] = snap
	out := cloneSnapshot(snap)
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return out, nil
}

// Close cancels in-flight jobs. Idempotent.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	cancels := make([]context.CancelFunc, 0, len(r.cancels))
	for _, c := range r.cancels {
		if c != nil {
			cancels = append(cancels, c)
		}
	}
	r.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return nil
}

func cloneSnapshot(s ports.JobSnapshot) ports.JobSnapshot {
	sums := make([]map[string]any, len(s.UnitSummaries))
	for i, m := range s.UnitSummaries {
		sums[i] = clonePayload(m)
	}
	if s.UnitSummaries == nil {
		sums = nil
	}
	s.UnitSummaries = sums
	return s
}
