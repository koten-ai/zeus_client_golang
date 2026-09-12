// SPDX-License-Identifier: BUSL-1.1

package domain

// Mode 3 job / unit types + isolation validation (Python domain.jobs, ZCG-28).
// Types only — no job engine, no goroutines, no Mode 1 auto-promote.
// Runtime port stays in ports.Jobs (Pattern A host is later).

const jobsComponent = "domain.jobs"

// UnitKind is the WorkUnit allowlist (API_UNITS).
type UnitKind string

const (
	UnitKindAgentTurn  UnitKind = "agent_turn"
	UnitKindZeusDirect UnitKind = "zeus_direct"
	UnitKindHTTPJSON   UnitKind = "http_json" // not implemented this train
)

// UnitStatus is one unit outcome (MULTI_AGENT_RUNTIME).
type UnitStatus string

const (
	UnitStatusOK        UnitStatus = "ok"
	UnitStatusError     UnitStatus = "error"
	UnitStatusTimeout   UnitStatus = "timeout"
	UnitStatusCancelled UnitStatus = "cancelled"
	UnitStatusDeadEnd   UnitStatus = "dead_end"
)

// JobBudgets is the job wall (Python JobBudgets). Zero values are invalid;
// use DefaultJobBudgets for the family defaults.
type JobBudgets struct {
	MaxWorkers         int
	WallMS             int
	MaxWaves           int
	MaxReplans         int
	MaxEvidenceKeys    int
	DisableEarlyCancel bool
}

// DefaultJobBudgets is the family default wall (Python JobBudgets defaults).
func DefaultJobBudgets() JobBudgets {
	return JobBudgets{
		MaxWorkers:      4,
		WallMS:          120_000,
		MaxWaves:        4,
		MaxReplans:      2,
		MaxEvidenceKeys: 32,
	}
}

// Validate is fail-closed budget law (130003).
func (b JobBudgets) Validate() error {
	if b.MaxWorkers < 1 || b.WallMS < 1 || b.MaxWaves < 1 {
		return NewJob(CodeJobsBudgetInvalid, jobsComponent)
	}
	return nil
}

// UnitConfig is one WorkUnit config slice (API_UNITS §3).
// Each unit owns zeus_url, scope triple, auth env *names*, catalog pin, bags.
// PasswordEnv / TokenEnv are secret *names* only — never values.
type UnitConfig struct {
	UnitID         string
	Kind           UnitKind
	Goal           string
	ZeusURL        string
	Bucket         string
	Scope          string
	Collection     string
	AuthMode       string
	PasswordEnv    string
	TokenEnv       string
	Username       string
	CatalogMode    string
	BaseID         string
	ChatRequest    map[string]any
	LLM            map[string]any
	Call           map[string]any
	ShareSessionID string
	MaxRounds      int
}

// UnitTarget is the per-unit scope triple. Domain-local — do not import config.DataTarget.
type UnitTarget struct {
	Bucket     string
	Scope      string
	Collection string
}

// Target is the unit scope triple (Python UnitConfig.target).
func (u UnitConfig) Target() UnitTarget {
	return UnitTarget{Bucket: u.Bucket, Scope: u.Scope, Collection: u.Collection}
}

var unitLLMPublicKeys = map[string]struct{}{
	"model": {}, "api_key_env": {}, "temperature": {}, "base_url": {}, "provider": {},
}

// ToPublicDict is the minified unit view. No password values, no llm.api_key.
// share_session_id is a bool (set or not) — never the raw session id.
func (u UnitConfig) ToPublicDict() map[string]any {
	llm := map[string]any{}
	for k, v := range u.LLM {
		if _, ok := unitLLMPublicKeys[k]; ok {
			llm[k] = v
		}
	}
	out := map[string]any{
		"unit_id":          u.UnitID,
		"kind":             string(u.Kind),
		"goal":             u.Goal,
		"zeus_url":         u.ZeusURL,
		"bucket":           u.Bucket,
		"scope":            u.Scope,
		"collection":       u.Collection,
		"auth_mode":        u.AuthMode,
		"catalog_mode":     u.CatalogMode,
		"base_id":          u.BaseID,
		"llm":              llm,
		"has_chat_request": u.ChatRequest != nil,
		"share_session_id": u.ShareSessionID != "",
	}
	if u.PasswordEnv != "" {
		out["password_env"] = u.PasswordEnv
	}
	if u.TokenEnv != "" {
		out["token_env"] = u.TokenEnv
	}
	return out
}

// UnitResult is one finished unit (Python UnitResult).
type UnitResult struct {
	UnitID    string
	Status    UnitStatus
	Answer    string
	ReqIDs    []string
	Artifacts map[string]any
	ErrorCode string
	DeadEnd   bool
	PlanEpoch int
}

// JobHandle is an accepted job (Python JobHandle). Status defaults to "accepted".
type JobHandle struct {
	JobID  string
	Status string
	Seq    int
}

// JobStatusAccepted is the JobHandle default (Python status="accepted").
const JobStatusAccepted = "accepted"

// JobEvent is one minified watch row (Python JobEvent). Payload must not carry secrets.
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

// ToPublicDict is the minified event (no secret keys at this layer).
func (e JobEvent) ToPublicDict() map[string]any {
	payload := cloneMap(e.Payload)
	return map[string]any{
		"seq":        e.Seq,
		"type":       e.Type,
		"job_id":     e.JobID,
		"ts_ms":      e.TSMS,
		"unit_id":    nilIfEmpty(e.UnitID),
		"wave":       e.Wave,
		"plan_epoch": e.PlanEpoch,
		"payload":    payload,
	}
}

// JobSnapshot is a point-in-time job view (Python JobSnapshot).
type JobSnapshot struct {
	JobID         string
	Status        string
	Seq           int
	Partial       bool
	UnitSummaries []map[string]any
	Answer        string
}

// ValidateUnitMap is fail-closed isolation law. Call before any unit goroutine.
// Empty / duplicate / missing scope triple → 130002.
// http_json → 000002.
// agent_turn without catalog pin → 130011.
// Shared durable session id across parallel units → 130013.
func ValidateUnitMap(units []UnitConfig) error {
	if len(units) < 1 {
		return NewJob(CodeJobsInvalidUnitMap, jobsComponent)
	}
	seen := make(map[string]struct{}, len(units))
	sessions := map[string]string{}
	for _, u := range units {
		if u.UnitID == "" {
			return NewJob(CodeJobsInvalidUnitMap, jobsComponent)
		}
		if _, dup := seen[u.UnitID]; dup {
			return NewJob(CodeJobsInvalidUnitMap, jobsComponent)
		}
		seen[u.UnitID] = struct{}{}
		switch u.Kind {
		case UnitKindHTTPJSON:
			return NewJob(CodeNotImplemented, jobsComponent,
				WithMessage("http_json units are not implemented in this SDK train"))
		case UnitKindAgentTurn, UnitKindZeusDirect:
			// ok
		default:
			return NewJob(CodeJobsInvalidUnitMap, jobsComponent)
		}
		if u.ZeusURL == "" || u.Bucket == "" || u.Scope == "" || u.Collection == "" {
			return NewJob(CodeJobsInvalidUnitMap, jobsComponent)
		}
		if u.Kind == UnitKindAgentTurn {
			if u.BaseID == "" && u.CatalogMode == "" && u.ChatRequest == nil {
				return NewJob(CodeUnitsCatalogMissing, jobsComponent)
			}
		}
		if u.ShareSessionID != "" {
			if prev, ok := sessions[u.ShareSessionID]; ok && prev != u.UnitID {
				return NewJob(CodeUnitsIsolation, jobsComponent)
			}
			sessions[u.ShareSessionID] = u.UnitID
		}
	}
	return nil
}

func cloneMap(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}
