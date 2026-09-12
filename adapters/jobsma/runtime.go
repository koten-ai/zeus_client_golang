// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

package jobsma

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/koten-ai/koten_multi_agent_golang/backend/memory"
	"github.com/koten-ai/koten_multi_agent_golang/multiagent"
	"github.com/koten-ai/koten_multi_agent_golang/workunit"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const jobsMAComponent = "adapters.jobsma"

// Runtime is Pattern A ports.Jobs: Engine.RunJob + memory store + Client units.
type Runtime struct {
	host  UnitRunner
	store *memory.Store

	mu        sync.Mutex
	closed    bool
	snaps     map[string]ports.JobSnapshot
	events    map[string][]ports.JobEvent
	cancels   map[string]context.CancelFunc
	cancelled map[string]bool
	wg        sync.WaitGroup
}

var _ ports.Jobs = (*Runtime)(nil)
var _ ports.Closer = (*Runtime)(nil)

// New binds a UnitRunner (api.UnitHost{Units: client.Units()}).
func New(host UnitRunner) *Runtime {
	return &Runtime{
		host:      host,
		store:     memory.New(),
		snaps:     map[string]ports.JobSnapshot{},
		events:    map[string][]ports.JobEvent{},
		cancels:   map[string]context.CancelFunc{},
		cancelled: map[string]bool{},
	}
}

// Store is the in-memory result store (epoch fence). Tests assert stale publish.
func (r *Runtime) Store() *memory.Store {
	if r == nil {
		return nil
	}
	return r.store
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

func notFound() *domain.Error {
	return domain.NewJob(domain.CodeJobsNotFound, jobsMAComponent)
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

func budgetsFromRequest(req map[string]any) domain.JobBudgets {
	bag := domain.DefaultJobBudgets()
	if req == nil {
		return bag
	}
	if b, ok := req["budgets"].(domain.JobBudgets); ok {
		return b
	}
	return bag
}

func mapEvent(e multiagent.JobEvent) ports.JobEvent {
	payload := map[string]any{}
	if e.Summary != "" {
		payload["summary"] = e.Summary
	}
	if e.Status != "" {
		payload["status"] = e.Status
	}
	if e.DeadEnd {
		payload["dead_end"] = true
	}
	return ports.JobEvent{
		Seq:       int(e.Seq),
		Type:      string(e.Type),
		JobID:     e.JobID,
		TSMS:      e.TS.UnixMilli(),
		UnitID:    e.UnitID,
		PlanEpoch: e.PlanEpoch,
		Payload:   payload,
	}
}

func (r *Runtime) record(ev ports.JobEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[ev.JobID] = append(r.events[ev.JobID], ev)
}

func (r *Runtime) isCancelled(jobID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled[jobID]
}

func workUnits(cfgs []domain.UnitConfig, host UnitRunner, models map[string]any) []workunit.WorkUnit {
	out := make([]workunit.WorkUnit, 0, len(cfgs))
	for i := range cfgs {
		out = append(out, &clientUnit{cfg: cfgs[i], host: host, models: models})
	}
	return out
}

func summariesFrom(units []multiagent.UnitResult) []map[string]any {
	out := make([]map[string]any, 0, len(units))
	for _, u := range units {
		var errCode any
		if u.Payload != nil {
			if c := u.Payload["error_code"]; c != nil {
				errCode = c
			}
		}
		if errCode == nil && u.Error != "" {
			errCode = u.Error
		}
		out = append(out, map[string]any{
			"unit_id":    u.UnitID,
			"status":     string(u.Status),
			"error_code": errCode,
		})
	}
	return out
}

func (r *Runtime) finish(jobID string, res multiagent.JobResult, runErr error) {
	status := "ok"
	partial := res.Partial
	switch {
	case r.isCancelled(jobID):
		status = "cancelled"
		partial = false
	case runErr != nil && !res.OK:
		status = "error"
	case res.Partial:
		status = "partial"
	case !res.OK:
		status = "error"
	}
	seq := 0
	r.mu.Lock()
	if evs := r.events[jobID]; len(evs) > 0 {
		seq = evs[len(evs)-1].Seq
	}
	if r.cancelled[jobID] {
		status = "cancelled"
		partial = false
	}
	r.snaps[jobID] = ports.JobSnapshot{
		JobID:         jobID,
		Status:        status,
		Seq:           seq,
		Partial:       partial,
		UnitSummaries: summariesFrom(res.Units),
		Answer:        res.Summary,
	}
	r.mu.Unlock()
}

// Run starts Engine.RunJob in a goroutine and returns a handle immediately.
func (r *Runtime) Run(ctx context.Context, request map[string]any) (ports.JobHandle, error) {
	if r == nil {
		return ports.JobHandle{}, domain.NewJob(domain.CodeJobsUnavailable, jobsMAComponent)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if r.host == nil {
		return ports.JobHandle{}, domain.NewJob(domain.CodeJobsUnavailable, jobsMAComponent)
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ports.JobHandle{}, domain.NewJob(domain.CodeJobsUnavailable, jobsMAComponent)
	}
	r.mu.Unlock()

	jobID := newJobID()
	cfgs := unitsFromRequest(request)
	models := modelsFromRequest(request)
	bag := budgetsFromRequest(request)
	goal := goalFromRequest(request)

	jobCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancels[jobID] = cancel
	r.events[jobID] = nil
	r.cancelled[jobID] = false
	r.snaps[jobID] = ports.JobSnapshot{JobID: jobID, Status: "running", Seq: 0}
	r.mu.Unlock()

	units := workUnits(cfgs, r.host, models)
	eng := multiagent.Engine{
		Store: r.store,
		OnEvent: func(e multiagent.JobEvent) {
			r.record(mapEvent(e))
		},
	}
	req := multiagent.RunRequest{
		Spec: multiagent.JobSpec{
			ID:         jobID,
			Goal:       goal,
			Planner:    multiagent.PlannerStatic,
			MaxReplans: bag.MaxReplans,
			Models:     multiagent.ModelConfig{AdvisorMode: multiagent.AdvisorOff},
			Budget: multiagent.Budget{
				MaxWorkers:         bag.MaxWorkers,
				MaxWaves:           bag.MaxWaves,
				MaxWallTime:        time.Duration(bag.WallMS) * time.Millisecond,
				MaxEvidenceKeys:    bag.MaxEvidenceKeys,
				DisableEarlyCancel: bag.DisableEarlyCancel,
			},
		},
		Units: units,
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer cancel()
		res, err := eng.RunJob(jobCtx, req)
		r.finish(jobID, res, err)
	}()

	return ports.JobHandle{JobID: jobID, Status: "running", Seq: 1}, nil
}

// Watch snapshots events with seq > afterSeq and closes the channel.
func (r *Runtime) Watch(ctx context.Context, jobID string, afterSeq int) (<-chan ports.JobEvent, error) {
	if r == nil {
		return nil, domain.NewJob(domain.CodeJobsUnavailable, jobsMAComponent)
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
	sort.Slice(buf, func(i, j int) bool { return buf[i].Seq < buf[j].Seq })
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
		return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsUnavailable, jobsMAComponent)
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
		return ports.JobSnapshot{}, domain.NewJob(domain.CodeJobsUnavailable, jobsMAComponent)
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

// Close cancels in-flight jobs and waits for worker goroutines. Idempotent.
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
	r.wg.Wait()
	return nil
}
