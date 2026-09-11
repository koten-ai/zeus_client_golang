// SPDX-License-Identifier: BUSL-1.1

package journal

// Event type strings — copy Python domain.journal.events exactly.
const (
	EventTurnStarted   = "turn.started"
	EventTurnCompleted = "turn.completed"
	EventSpanStarted   = "span.started"
	EventSpanEnded     = "span.ended"
	EventErrorRaised   = "error.raised"
	EventNote          = "note"
	EventZeusHop       = "zeus.hop"
	EventLLMRound      = "llm.round"
	EventJobStarted    = "job.started"
	EventJobFinished   = "job.finished"
	EventUnitStarted   = "unit.started"
	EventUnitFinished  = "unit.finished"
)

// JournalEvent is one immutable journal row (Python JournalEvent).
// Bodies belong in PayloadStore; this row keeps payload_refs.
//
// Empty SpanID / ParentSpanID mean none (Python None). Data and PayloadRefs
// are copied on Append and again on Events so callers cannot alias storage.
type JournalEvent struct {
	EventID      string
	TsMs         int64
	Type         string
	Component    string
	TurnID       string
	SpanID       string
	ParentSpanID string
	Data         map[string]any
	PayloadRefs  []string
}

func copyData(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyRefs(r []string) []string {
	out := make([]string, len(r))
	copy(out, r)
	return out
}

func freezeEvent(e JournalEvent) JournalEvent {
	e.Data = copyData(e.Data)
	e.PayloadRefs = copyRefs(e.PayloadRefs)
	return e
}
