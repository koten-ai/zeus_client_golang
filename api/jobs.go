// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const jobsAPIComponent = "api.jobs"

// JobsOptions constructs JobsAPI (Python JobsAPI).
type JobsOptions struct {
	Jobs    ports.Jobs
	Journal journal.ExecutionJournal
	Clock   ports.Clock
}

// JobsRunParams is JobsAPI.Run kwargs (Python jobs.run).
// Units wins over ScopeMap when both are set (Python `units or scope_map`).
type JobsRunParams struct {
	Pack     string
	Budgets  *domain.JobBudgets
	Units    []domain.UnitConfig
	ScopeMap []domain.UnitConfig
	Models   map[string]any
}

// UnitHost adapts UnitsAPI for a job runtime (adapters/jobsfake.UnitRunner).
// FakeJobRuntime must not import this package.
type UnitHost struct {
	Units *UnitsAPI
}

// AgentTurn forwards one isolated agent unit (job_models later-wins).
func (h UnitHost) AgentTurn(ctx context.Context, unit domain.UnitConfig, jobID string, jobModels map[string]any) (domain.UnitResult, error) {
	if h.Units == nil {
		return domain.UnitResult{}, domain.New(domain.CodeNotImplemented, unitsAPIComponent,
			domain.WithMessage("Units API is nil"))
	}
	return h.Units.AgentTurn(ctx, unit, UnitCallParams{JobID: jobID, JobModels: jobModels})
}

// ZeusDirect forwards one isolated direct unit.
func (h UnitHost) ZeusDirect(ctx context.Context, unit domain.UnitConfig, jobID string) (domain.UnitResult, error) {
	if h.Units == nil {
		return domain.UnitResult{}, domain.New(domain.CodeNotImplemented, unitsAPIComponent,
			domain.WithMessage("Units API is nil"))
	}
	return h.Units.ZeusDirect(ctx, unit, UnitCallParams{JobID: jobID})
}

// JobsAPI is the Mode 3 jobs facade (Python JobsAPI). Fail-closed without a
// host (130001). Everyday Q&A stays Mode 1 — jobs are never auto-promoted.
type JobsAPI struct {
	host any
	opts JobsOptions
}

// NewJobsAPI binds a Client (or test fake) as the facade host.
func NewJobsAPI(host any) *JobsAPI {
	if host == nil {
		return nil
	}
	return &JobsAPI{host: host}
}

// NewJobsAPIWith binds host plus the jobs port (Client.Jobs).
func NewJobsAPIWith(host any, opts JobsOptions) *JobsAPI {
	return &JobsAPI{host: host, opts: opts}
}

// Host is the bound Client. Nil-safe.
func (j *JobsAPI) Host() any {
	if j == nil {
		return nil
	}
	return j.host
}

func unavailableJobs() *domain.Error {
	return domain.NewJob(domain.CodeJobsUnavailable, jobsAPIComponent)
}

func (j *JobsAPI) port() (ports.Jobs, error) {
	if j == nil || j.opts.Jobs == nil {
		return nil, unavailableJobs()
	}
	return j.opts.Jobs, nil
}

func (j *JobsAPI) clockMS(ctx context.Context) int64 {
	if j == nil || j.opts.Clock == nil {
		return ports.SystemClock{}.NowMS(ctx)
	}
	return j.opts.Clock.NowMS(ctx)
}

func coalesceUnits(params JobsRunParams) []domain.UnitConfig {
	src := params.Units
	if len(src) == 0 {
		src = params.ScopeMap
	}
	if len(src) == 0 {
		return nil
	}
	out := make([]domain.UnitConfig, len(src))
	copy(out, src)
	return out
}

func cloneJobsMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func domainJobHandle(h ports.JobHandle) domain.JobHandle {
	return domain.JobHandle{JobID: h.JobID, Status: h.Status, Seq: h.Seq}
}

func domainJobEvent(e ports.JobEvent) domain.JobEvent {
	return domain.JobEvent{
		Seq:       e.Seq,
		Type:      e.Type,
		JobID:     e.JobID,
		TSMS:      e.TSMS,
		UnitID:    e.UnitID,
		Wave:      e.Wave,
		PlanEpoch: e.PlanEpoch,
		Payload:   cloneJobsMap(e.Payload),
	}
}

func domainJobSnapshot(s ports.JobSnapshot) domain.JobSnapshot {
	sums := make([]map[string]any, len(s.UnitSummaries))
	for i, m := range s.UnitSummaries {
		sums[i] = cloneJobsMap(m)
	}
	if s.UnitSummaries == nil {
		sums = nil
	}
	return domain.JobSnapshot{
		JobID:         s.JobID,
		Status:        s.Status,
		Seq:           s.Seq,
		Partial:       s.Partial,
		UnitSummaries: sums,
		Answer:        s.Answer,
	}
}

func snapshotWatch(src <-chan ports.JobEvent) <-chan domain.JobEvent {
	if src == nil {
		ch := make(chan domain.JobEvent)
		close(ch)
		return ch
	}
	var buf []domain.JobEvent
	for ev := range src {
		buf = append(buf, domainJobEvent(ev))
	}
	out := make(chan domain.JobEvent, len(buf))
	for _, ev := range buf {
		out <- ev
	}
	close(out)
	return out
}

func (j *JobsAPI) journalStarted(ctx context.Context, handle domain.JobHandle, goal string, unitCount int) {
	if j == nil || j.opts.Journal == nil {
		return
	}
	j.opts.Journal.Append(journal.JournalEvent{
		EventID:   "job_" + handle.JobID + "_start",
		TsMs:      j.clockMS(ctx),
		Type:      journal.EventJobStarted,
		Component: jobsAPIComponent,
		TurnID:    handle.JobID,
		Data: map[string]any{
			"job_id":     handle.JobID,
			"goal":       goal,
			"unit_count": unitCount,
		},
	})
}

// Run validates budgets + unit map, then starts a job on the host.
// Missing host → 130001. Invalid budget → 130003. Invalid map → 130002.
func (j *JobsAPI) Run(ctx context.Context, goal string, params JobsRunParams) (domain.JobHandle, error) {
	if j == nil {
		return domain.JobHandle{}, unavailableJobs()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	parsed := coalesceUnits(params)
	bag := domain.DefaultJobBudgets()
	if params.Budgets != nil {
		bag = *params.Budgets
	}
	if err := bag.Validate(); err != nil {
		return domain.JobHandle{}, err
	}
	if err := domain.ValidateUnitMap(parsed); err != nil {
		return domain.JobHandle{}, err
	}
	port, err := j.port()
	if err != nil {
		return domain.JobHandle{}, err
	}
	handle, err := port.Run(ctx, map[string]any{
		"goal":    goal,
		"pack":    params.Pack,
		"budgets": bag,
		"units":   parsed,
		"models":  cloneJobsMap(params.Models),
	})
	if err != nil {
		return domain.JobHandle{}, err
	}
	out := domainJobHandle(handle)
	j.journalStarted(ctx, out, goal, len(parsed))
	return out, nil
}

// Watch is a snapshot of JobEvents with seq > afterSeq. The channel is closed
// by the adapter (fake: historical rows after Run returns).
func (j *JobsAPI) Watch(ctx context.Context, jobID string, afterSeq int) (<-chan domain.JobEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	port, err := j.port()
	if err != nil {
		return nil, err
	}
	src, err := port.Watch(ctx, jobID, afterSeq)
	if err != nil {
		return nil, err
	}
	return snapshotWatch(src), nil
}

// Get is the point-in-time snapshot (Python jobs.get).
func (j *JobsAPI) Get(ctx context.Context, jobID string) (domain.JobSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	port, err := j.port()
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	snap, err := port.Get(ctx, jobID)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	return domainJobSnapshot(snap), nil
}

// Cancel is best-effort (Python jobs.cancel). Unknown job → 130004.
func (j *JobsAPI) Cancel(ctx context.Context, jobID string) (domain.JobSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	port, err := j.port()
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	snap, err := port.Cancel(ctx, jobID)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	return domainJobSnapshot(snap), nil
}
