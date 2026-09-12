// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/security"
)

// SpanNode is one span.started/ended node (Python SpanNode).
type SpanNode struct {
	SpanID       string
	Name         string
	ParentSpanID string
	TurnID       string
	StartedTsMs  *int64
	EndedTsMs    *int64
	Status       string
	Children     []SpanNode
}

// ToMap is Python SpanNode.to_dict.
func (n SpanNode) ToMap() map[string]any {
	var parent any
	if n.ParentSpanID != "" {
		parent = n.ParentSpanID
	}
	kids := make([]any, len(n.Children))
	for i, c := range n.Children {
		kids[i] = c.ToMap()
	}
	return map[string]any{
		"span_id":        n.SpanID,
		"name":           n.Name,
		"parent_span_id": parent,
		"turn_id":        n.TurnID,
		"started_ts_ms":  n.StartedTsMs,
		"ended_ts_ms":    n.EndedTsMs,
		"status":         nilIfEmpty(n.Status),
		"children":       kids,
	}
}

// SpanTree is Python SpanTree.
type SpanTree struct {
	Roots  []SpanNode
	TurnID string
}

// ToMap is Python SpanTree.to_dict.
func (t SpanTree) ToMap() map[string]any {
	roots := make([]any, len(t.Roots))
	for i, r := range t.Roots {
		roots[i] = r.ToMap()
	}
	var tid any
	if t.TurnID != "" {
		tid = t.TurnID
	}
	return map[string]any{"turn_id": tid, "roots": roots}
}

// ExportJournalRedacted is Python export_journal_redacted.
func ExportJournalRedacted(j journal.ExecutionJournal, turnID string, red security.Redactor) journal.JournalExport {
	exp := journal.ExportRedacted(j, red)
	return journal.FilterByTurn(exp, turnID)
}

// EventTypeSequence is Python event_type_sequence.
func EventTypeSequence(exp journal.JournalExport, turnID string) []string {
	filtered := journal.FilterByTurn(exp, turnID)
	var out []string
	for _, e := range filtered.Events {
		if e.Type != "" {
			out = append(out, e.Type)
		}
	}
	return out
}

// BuildSpanTree is Python build_span_tree.
func BuildSpanTree(j journal.ExecutionJournal, turnID string) SpanTree {
	exp := journal.Export(j)
	return spanTreeFromExport(exp, turnID)
}

func spanTreeFromExport(exp journal.JournalExport, turnID string) SpanTree {
	exp = journal.FilterByTurn(exp, turnID)
	started := map[string]journal.EventDump{}
	ended := map[string]journal.EventDump{}
	for _, ev := range exp.Events {
		if ev.SpanID == nil || *ev.SpanID == "" {
			continue
		}
		sid := *ev.SpanID
		switch ev.Type {
		case journal.EventSpanStarted:
			started[sid] = ev
		case journal.EventSpanEnded:
			ended[sid] = ev
		}
	}
	nodes := map[string]SpanNode{}
	for sid, sev := range started {
		eev := ended[sid]
		name := sid
		if sev.Data != nil {
			if n, ok := sev.Data["name"].(string); ok && n != "" {
				name = n
			}
		}
		if name == sid && eev.Data != nil {
			if n, ok := eev.Data["name"].(string); ok && n != "" {
				name = n
			}
		}
		parent := ""
		if sev.ParentSpanID != nil {
			parent = *sev.ParentSpanID
		}
		var startedMs, endedMs *int64
		if sev.TsMs != 0 {
			v := sev.TsMs
			startedMs = &v
		}
		if eev.TsMs != 0 {
			v := eev.TsMs
			endedMs = &v
		}
		st := ""
		if eev.Data != nil {
			if s, ok := eev.Data["status"].(string); ok {
				st = s
			}
		}
		nodes[sid] = SpanNode{
			SpanID:       sid,
			Name:         name,
			ParentSpanID: parent,
			TurnID:       sev.TurnID,
			StartedTsMs:  startedMs,
			EndedTsMs:    endedMs,
			Status:       st,
		}
	}
	children := map[string][]SpanNode{}
	var roots []SpanNode
	known := map[string]struct{}{}
	for sid := range nodes {
		known[sid] = struct{}{}
	}
	for _, n := range nodes {
		if n.ParentSpanID == "" || func() bool { _, ok := known[n.ParentSpanID]; return !ok }() {
			roots = append(roots, n)
			continue
		}
		children[n.ParentSpanID] = append(children[n.ParentSpanID], n)
	}
	var withKids func(SpanNode) SpanNode
	withKids = func(n SpanNode) SpanNode {
		kids := children[n.SpanID]
		outKids := make([]SpanNode, 0, len(kids))
		for _, c := range kids {
			outKids = append(outKids, withKids(c))
		}
		n.Children = outKids
		return n
	}
	outRoots := make([]SpanNode, 0, len(roots))
	for _, r := range roots {
		outRoots = append(outRoots, withKids(r))
	}
	return SpanTree{Roots: outRoots, TurnID: turnID}
}

// MermaidTimeline is Python mermaid_timeline.
func MermaidTimeline(j journal.ExecutionJournal, turnID string) string {
	tree := BuildSpanTree(j, turnID)
	lines := []string{"flowchart TD"}
	if len(tree.Roots) == 0 {
		lines = append(lines, "  empty[no spans]")
		return strings.Join(lines, "\n") + "\n"
	}
	var emit func(n SpanNode, parent string)
	emit = func(n SpanNode, parent string) {
		nid := strings.ReplaceAll(n.SpanID, "-", "_")
		label := strings.ReplaceAll(n.Name, `"`, "'")
		st := ""
		if n.Status != "" {
			st = " (" + n.Status + ")"
		}
		lines = append(lines, "  "+nid+`["`+label+st+`"]`)
		if parent != "" {
			lines = append(lines, "  "+parent+" --> "+nid)
		}
		for _, c := range n.Children {
			emit(c, nid)
		}
	}
	for _, r := range tree.Roots {
		emit(r, "")
	}
	return strings.Join(lines, "\n") + "\n"
}
