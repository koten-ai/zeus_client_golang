// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// JobHandle is an accepted job (Python domain.jobs.JobHandle).
// Full domain types + ValidateUnitMap live in domain (ZCG-28).
type JobHandle struct {
	JobID  string
	Status string
	Seq    int
}

// JobEvent is one watch row (Python domain.jobs.JobEvent).
type JobEvent struct {
	Seq       int
	Type      string
	JobID     string
	TSMS      int64
	UnitID    string
	Wave      int
	PlanEpoch int
	Payload   map[string]any
}

// JobSnapshot is a point-in-time job view (Python domain.jobs.JobSnapshot).
type JobSnapshot struct {
	JobID         string
	Status        string
	Seq           int
	Partial       bool
	UnitSummaries []map[string]any
	Answer        string
}

// Jobs is the job-runtime port (Python JobRuntimePort).
// Pattern A host is adapters/jobsma (links koten_multi_agent_golang).
// Sequential L4 seed is adapters/jobsfake. Do not grow this port into an engine.
// Watch returns a channel that the adapter closes when the watch ends.
type Jobs interface {
	Run(ctx context.Context, request map[string]any) (JobHandle, error)
	Watch(ctx context.Context, jobID string, afterSeq int) (<-chan JobEvent, error)
	Get(ctx context.Context, jobID string) (JobSnapshot, error)
	Cancel(ctx context.Context, jobID string) (JobSnapshot, error)
}
