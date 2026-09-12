// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/security"
)

// DebugOptions constructs DebugAPI (Python DebugAPI.__init__).
type DebugOptions struct {
	Journal  journal.ExecutionJournal
	Redactor security.Redactor
}

// DebugAPI is rt.debug — journal export / spans (Python DebugAPI).
type DebugAPI struct {
	host any
	opts DebugOptions
}

// NewDebugAPI binds a Client (or test fake) as the facade host.
func NewDebugAPI(host any) *DebugAPI {
	if host == nil {
		return nil
	}
	return &DebugAPI{host: host}
}

// NewDebugAPIWith binds host plus journal/redactor (Client.Debug).
func NewDebugAPIWith(host any, opts DebugOptions) *DebugAPI {
	return &DebugAPI{host: host, opts: opts}
}

// Host is the bound Client. Nil-safe.
func (d *DebugAPI) Host() any {
	if d == nil {
		return nil
	}
	return d.host
}

// ExportJournal is rt.debug.export_journal (default redacted).
func (d *DebugAPI) ExportJournal(turnID string, redact bool) journal.JournalExport {
	if d == nil {
		return journal.JournalExport{JournalSchema: journal.JournalSchemaVersion, Events: []journal.EventDump{}, Payloads: map[string]journal.PayloadMeta{}}
	}
	if redact {
		return application.ExportJournalRedacted(d.opts.Journal, turnID, d.opts.Redactor)
	}
	return journal.FilterByTurn(journal.Export(d.opts.Journal), turnID)
}

// Spans is rt.debug.spans.
func (d *DebugAPI) Spans(turnID string) application.SpanTree {
	if d == nil {
		return application.SpanTree{}
	}
	return application.BuildSpanTree(d.opts.Journal, turnID)
}

// MermaidTimeline is rt.debug.mermaid_timeline.
func (d *DebugAPI) MermaidTimeline(turnID string) string {
	if d == nil {
		return "flowchart TD\n  empty[no spans]\n"
	}
	return application.MermaidTimeline(d.opts.Journal, turnID)
}

// EventTypes is rt.debug.event_types.
func (d *DebugAPI) EventTypes(turnID string) []string {
	if d == nil {
		return nil
	}
	exp := d.ExportJournal(turnID, true)
	return application.EventTypeSequence(exp, "")
}
