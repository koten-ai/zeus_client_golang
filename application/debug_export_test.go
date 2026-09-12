// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain/journal"
)

func TestExportJournalRedactedFiltersTurn(t *testing.T) {
	j := journal.NewInMemoryJournal(nil)
	j.Append(journal.JournalEvent{Type: journal.EventTurnStarted, TurnID: "t1", Data: map[string]any{"k": "v"}})
	j.Append(journal.JournalEvent{Type: journal.EventNote, TurnID: "t2", Data: map[string]any{"k": "other"}})
	exp := ExportJournalRedacted(j, "t1", nil)
	if len(exp.Events) != 1 || exp.Events[0].Type != journal.EventTurnStarted {
		t.Fatalf("%+v", exp.Events)
	}
	types := EventTypeSequence(exp, "")
	if len(types) != 1 || types[0] != journal.EventTurnStarted {
		t.Fatalf("%v", types)
	}
}

func TestBuildSpanTreeAndMermaid(t *testing.T) {
	j := journal.NewInMemoryJournal(nil)
	j.Append(journal.JournalEvent{Type: journal.EventSpanStarted, SpanID: "s1", TurnID: "t1", Data: map[string]any{"name": "turn"}})
	j.Append(journal.JournalEvent{Type: journal.EventSpanEnded, SpanID: "s1", TurnID: "t1", Data: map[string]any{"name": "turn", "status": "ok"}})
	tree := BuildSpanTree(j, "t1")
	if len(tree.Roots) != 1 || tree.Roots[0].Name != "turn" {
		t.Fatalf("%+v", tree)
	}
	md := MermaidTimeline(j, "t1")
	if !strings.Contains(md, "flowchart TD") || !strings.Contains(md, "turn") {
		t.Fatalf("%s", md)
	}
}
