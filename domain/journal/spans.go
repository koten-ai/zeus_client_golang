// SPDX-License-Identifier: BUSL-1.1

package journal

import (
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

const defaultSpanComponent = "domain.journal.spans"

// Clock returns unix milliseconds. Nil uses time.Now (Python _clock_ms).
type Clock func() int64

// SpanHandle is an open span (Python SpanHandle).
type SpanHandle struct {
	SpanID       string
	ParentSpanID string
	Name         string
	TurnID       string
}

// SpanTracer writes span.started / span.ended with parent linkage.
// The stack is per-tracer and sequential (one turn). The journal is what
// concurrent units hammer — do not share one tracer across goroutines.
type SpanTracer struct {
	journal   ExecutionJournal
	turnID    string
	Component string
	clock     Clock
	stack     []string
}

// NewSpanTracer binds a tracer to a journal and turn. clock may be nil.
func NewSpanTracer(j ExecutionJournal, turnID string, clock Clock) *SpanTracer {
	return &SpanTracer{
		journal:   j,
		turnID:    turnID,
		Component: defaultSpanComponent,
		clock:     clock,
	}
}

func (t *SpanTracer) now() int64 {
	if t.clock != nil {
		return t.clock()
	}
	return time.Now().UnixMilli()
}

// NewSpanID returns span_<16 hex> (Python SpanTracer._new_span_id).
func NewSpanID() string {
	return "span_" + newHex16()
}

// NewEventID returns evt_<16 hex> (Python journal event_id mint).
func NewEventID() string {
	return "evt_" + newHex16()
}

func newHex16() string {
	u := uuid.New()
	return hex.EncodeToString(u[:])[:16]
}

// Start opens a span, linking to parent_span_id or the current stack top.
func (t *SpanTracer) Start(name, parentSpanID string, data map[string]any) SpanHandle {
	if parentSpanID == "" && len(t.stack) > 0 {
		parentSpanID = t.stack[len(t.stack)-1]
	}
	spanID := NewSpanID()
	handle := SpanHandle{
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Name:         name,
		TurnID:       t.turnID,
	}
	merged := map[string]any{"name": name}
	for k, v := range data {
		merged[k] = v
	}
	t.journal.Append(JournalEvent{
		EventID:      NewEventID(),
		TsMs:         t.now(),
		Type:         EventSpanStarted,
		Component:    t.Component,
		TurnID:       t.turnID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		Data:         merged,
	})
	t.stack = append(t.stack, spanID)
	return handle
}

// End closes a span. status defaults to "ok" when empty.
func (t *SpanTracer) End(handle SpanHandle, status string, data map[string]any) {
	if status == "" {
		status = "ok"
	}
	if n := len(t.stack); n > 0 && t.stack[n-1] == handle.SpanID {
		t.stack = t.stack[:n-1]
	} else {
		for i, id := range t.stack {
			if id == handle.SpanID {
				t.stack = append(t.stack[:i], t.stack[i+1:]...)
				break
			}
		}
	}
	merged := map[string]any{"name": handle.Name, "status": status}
	for k, v := range data {
		merged[k] = v
	}
	t.journal.Append(JournalEvent{
		EventID:      NewEventID(),
		TsMs:         t.now(),
		Type:         EventSpanEnded,
		Component:    t.Component,
		TurnID:       handle.TurnID,
		SpanID:       handle.SpanID,
		ParentSpanID: handle.ParentSpanID,
		Data:         merged,
	})
}
