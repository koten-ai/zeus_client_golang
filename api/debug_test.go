// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain/journal"
)

func TestDebugAPIExportJournal(t *testing.T) {
	j := journal.NewInMemoryJournal(nil)
	j.Append(journal.JournalEvent{Type: journal.EventTurnStarted, TurnID: "t1"})
	api := NewDebugAPIWith("host", DebugOptions{Journal: j})
	exp := api.ExportJournal("t1", true)
	if len(exp.Events) != 1 {
		t.Fatalf("%+v", exp.Events)
	}
	if types := api.EventTypes("t1"); len(types) != 1 || types[0] != journal.EventTurnStarted {
		t.Fatalf("%v", types)
	}
	tree := api.Spans("t1")
	if tree.TurnID != "t1" {
		t.Fatalf("%+v", tree)
	}
}
