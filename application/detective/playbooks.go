// SPDX-License-Identifier: BUSL-1.1

package detective

import (
	"fmt"
	"regexp"
	"strings"
)

// PlaybookIDs is the v1 min set (Python PLAYBOOK_IDS).
var PlaybookIDs = []string{
	"boundary_collections",
	"missing_inject",
	"hybrid_empty_find_ok",
	"project_dotted_fk",
	"tool_errors",
	"hollow_answer",
	"contract_drift",
}

var (
	reBoundaryCollections = regexp.MustCompile(`(?i)unknown boundary:\s*["']?collections`)
	reDottedFK            = regexp.MustCompile(`(?i)unknown FK|attributes\.\w+|dotted`)
)

func playbook(id, title, severity, summary string, actions []string) map[string]any {
	if actions == nil {
		actions = []string{}
	}
	return map[string]any{
		"id":       id,
		"title":    title,
		"severity": severity,
		"summary":  summary,
		"actions":  actions,
	}
}

// RunPlaybooks is Python run_playbooks.
func RunPlaybooks(hops []map[string]any, notes []string, answer string, prompt map[string]any, contractStatus string, layerA map[string]any) []map[string]any {
	errBlob := HopErrorBlob(hops) + "\n" + NotesBlob(notes)
	var out []map[string]any

	if reBoundaryCollections.MatchString(errBlob) {
		out = append(out, playbook(
			"boundary_collections",
			"Boundary collections not registered",
			"high",
			"Tool error mentions unknown boundary collections — register N1QL/boundary then restart zeus-api.",
			[]string{
				"Check Zeus boundary/collections registration",
				"Never unregister prod boundary casually",
			},
		))
	}

	inj := asMap(prompt["inject"])
	if inj == nil {
		inj = map[string]any{}
	}
	if !asBool(inj["has_scope_brief"]) || !asBool(inj["has_mini_schema"]) {
		out = append(out, playbook(
			"missing_inject",
			"SCOPE BRIEF / MINI-SCHEMA missing on client path",
			"high",
			"Client system prompt lacks inject markers required for floor-5 control plane.",
			[]string{
				"Verify catalog load + merge_scope_brief",
				"Do not trust Hub tool-hop inject tiles for external agents",
			},
		))
	}

	var names []string
	for _, h := range hops {
		names = append(names, hopName(h))
	}
	hasSearch := false
	hasFind := false
	for _, n := range names {
		if n == "search" || n == "hybrid" {
			hasSearch = true
		}
		if n == "find" || n == "project" {
			hasFind = true
		}
	}
	if hasSearch && hasFind {
		searchEmpty := false
		findOK := false
		for _, h := range hops {
			n := hopName(h)
			snip := asString(h["snippet"])
			if n == "search" || n == "hybrid" {
				if strings.Contains(snip, "[]") || strings.Contains(strings.ToLower(snip), "empty") || hopFailed(h) {
					searchEmpty = true
				}
			}
			if n == "find" || n == "project" {
				okFalse, isBool := h["ok"].(bool)
				if (!isBool || okFalse) && strings.TrimSpace(snip) != "" {
					findOK = true
				}
			}
		}
		if searchEmpty && findOK {
			out = append(out, playbook(
				"hybrid_empty_find_ok",
				"Search empty but find/project returned data",
				"med",
				"FTS/hybrid empty while graph find later succeeded — common ID/strategy mismatch.",
				[]string{
					"Prefer FTS typeahead for dropdowns",
					"Do not treat empty hybrid as hard fail",
				},
			))
		}
	}

	if reDottedFK.MatchString(errBlob) {
		out = append(out, playbook(
			"project_dotted_fk",
			"Project dotted FK / attributes path",
			"med",
			"Tool error suggests dotted attribute or FK projection issue.",
			[]string{
				"Check project fields vs entity schema",
				"Avoid projecting FTS biz: keys as graph ids",
			},
		))
	}

	errN := CountToolErrors(hops, nil)
	hasBoundary := false
	for _, p := range out {
		if asString(p["id"]) == "boundary_collections" {
			hasBoundary = true
			break
		}
	}
	if errN > 0 && !hasBoundary {
		sev := "med"
		if errN >= 3 {
			sev = "high"
		}
		out = append(out, playbook(
			"tool_errors",
			fmt.Sprintf("%d tool hop error(s)", errN),
			sev,
			"One or more Zeus hops returned 4xx/5xx or ok=false.",
			[]string{"Open preferred_req_id in Hub Detective", "Inspect hop snippets on debug.hops"},
		))
	}

	rows := CountRowsSignal(hops)
	conf, _ := layerA["confidence"].(string)
	ans := strings.TrimSpace(answer)
	if ans != "" && rows == 0 && errN == 0 && len(hops) > 0 {
		sev := "med"
		if conf == "high" || conf == "med" {
			sev = "high"
		}
		out = append(out, playbook(
			"hollow_answer",
			"Prose answer without row evidence",
			sev,
			"Model produced an answer but hops show no clear row/items payload.",
			[]string{"Verify tool result unpacking", "Check cheap vs insight path"},
		))
	}

	cs := strings.ToLower(contractStatus)
	switch cs {
	case "drift", "mismatch", "fail", "failed":
		out = append(out, playbook(
			"contract_drift",
			"Contract drift / mismatch",
			"high",
			"contract_status="+contractStatus,
			[]string{
				"Align scope_contracts hash with Verify stamp",
				"Never invent production contract_hash",
			},
		))
	}

	if out == nil {
		out = []map[string]any{}
	}
	return out
}
