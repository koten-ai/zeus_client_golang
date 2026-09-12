// SPDX-License-Identifier: BUSL-1.1

package journal

import "github.com/koten-ai/zeus_client_golang/security"

// JournalSchemaVersion is export envelope version (Python JOURNAL_SCHEMA_VERSION).
const JournalSchemaVersion = 1

// EventDump is one event in a JournalExport (schema v1 keys).
type EventDump struct {
	EventID      string         `json:"event_id"`
	TsMs         int64          `json:"ts_ms"`
	Type         string         `json:"type"`
	Component    string         `json:"component"`
	TurnID       string         `json:"turn_id"`
	SpanID       *string        `json:"span_id"`
	ParentSpanID *string        `json:"parent_span_id"`
	Data         map[string]any `json:"data"`
	PayloadRefs  []string       `json:"payload_refs"`
}

// JournalExport is a JSON-serializable snapshot (Python JournalExport).
// Payloads is metadata only — bytes stay in PayloadStore.
type JournalExport struct {
	JournalSchema int                    `json:"journal_schema"`
	Events        []EventDump            `json:"events"`
	Payloads      map[string]PayloadMeta `json:"payloads"`
}

type payloadLister interface {
	PayloadItems() map[string]PayloadMeta
}

// Export is the unredacted snapshot (Python export_journal).
func Export(j ExecutionJournal) JournalExport {
	if j == nil {
		return emptyExport()
	}
	return j.Export()
}

// ExportRedacted is the public dump path (Python export_journal_redacted).
// A nil redactor uses security.New(). Payload bytes are never included.
func ExportRedacted(j ExecutionJournal, red security.Redactor) JournalExport {
	if red == nil {
		red = security.New()
	}
	return snapshotExport(j, red)
}

func snapshotExport(j ExecutionJournal, red security.Redactor) JournalExport {
	var evs []JournalEvent
	if j != nil {
		evs = j.Events()
	}
	dumps := make([]EventDump, 0, len(evs))
	for _, ev := range evs {
		dumps = append(dumps, dumpEvent(ev, red))
	}
	return JournalExport{
		JournalSchema: JournalSchemaVersion,
		Events:        dumps,
		Payloads:      payloadItemsOf(j),
	}
}

func payloadItemsOf(j ExecutionJournal) map[string]PayloadMeta {
	if l, ok := j.(payloadLister); ok {
		items := l.PayloadItems()
		out := make(map[string]PayloadMeta, len(items))
		for k, v := range items {
			out[k] = v
		}
		return out
	}
	return map[string]PayloadMeta{}
}

func dumpEvent(ev JournalEvent, red security.Redactor) EventDump {
	data := copyData(ev.Data)
	if red != nil {
		walked := red.JSONValue(data)
		m, ok := walked.(map[string]any)
		if !ok {
			m = map[string]any{"_": walked}
		}
		data = m
	}
	return EventDump{
		EventID:      ev.EventID,
		TsMs:         ev.TsMs,
		Type:         ev.Type,
		Component:    ev.Component,
		TurnID:       ev.TurnID,
		SpanID:       strPtr(ev.SpanID),
		ParentSpanID: strPtr(ev.ParentSpanID),
		Data:         data,
		PayloadRefs:  copyRefs(ev.PayloadRefs),
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	cp := s
	return &cp
}

func emptyExport() JournalExport {
	return JournalExport{
		JournalSchema: JournalSchemaVersion,
		Events:        []EventDump{},
		Payloads:      map[string]PayloadMeta{},
	}
}

// FilterByTurn keeps events (and referenced payload meta) for one turn_id.
// Empty turnID returns exp unchanged (Python filter_export_by_turn).
func FilterByTurn(exp JournalExport, turnID string) JournalExport {
	if turnID == "" {
		return exp
	}
	var events []EventDump
	refs := map[string]struct{}{}
	for _, ev := range exp.Events {
		if ev.TurnID != turnID {
			continue
		}
		events = append(events, ev)
		for _, r := range ev.PayloadRefs {
			refs[r] = struct{}{}
		}
	}
	payloads := map[string]PayloadMeta{}
	if len(refs) > 0 {
		for k, v := range exp.Payloads {
			if _, ok := refs[k]; ok {
				payloads[k] = v
			}
		}
	}
	if events == nil {
		events = []EventDump{}
	}
	return JournalExport{
		JournalSchema: exp.JournalSchema,
		Events:        events,
		Payloads:      payloads,
	}
}

// ToMap is Python JournalExport.to_dict (schema v1 keys).
func (e JournalExport) ToMap() map[string]any {
	events := make([]any, len(e.Events))
	for i, ev := range e.Events {
		events[i] = map[string]any{
			"event_id":       ev.EventID,
			"ts_ms":          ev.TsMs,
			"type":           ev.Type,
			"component":      ev.Component,
			"turn_id":        ev.TurnID,
			"span_id":        anyString(ev.SpanID),
			"parent_span_id": anyString(ev.ParentSpanID),
			"data":           ev.Data,
			"payload_refs":   ev.PayloadRefs,
		}
	}
	payloads := make(map[string]any, len(e.Payloads))
	for k, v := range e.Payloads {
		payloads[k] = map[string]any{
			"ref":          v.Ref,
			"content_type": v.ContentType,
			"kind":         v.Kind,
			"sha256":       v.SHA256,
			"size":         v.Size,
		}
	}
	return map[string]any{
		"journal_schema": e.JournalSchema,
		"events":         events,
		"payloads":       payloads,
	}
}

func anyString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
