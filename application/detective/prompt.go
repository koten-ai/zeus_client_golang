// SPDX-License-Identifier: BUSL-1.1

package detective

import (
	"fmt"
	"strings"
)

func checklistItem(id, label, status, group, detail, fixHint string) map[string]any {
	d := map[string]any{
		"id":     id,
		"label":  label,
		"status": status,
		"group":  group,
	}
	if detail != "" {
		d["detail"] = detail
	}
	if fixHint != "" {
		d["fix_hint"] = fixHint
	}
	return d
}

// BuildPromptChecklist is Python build_prompt_checklist.
func BuildPromptChecklist(messages []map[string]any, systemPrompt string, catalog map[string]any, tools []map[string]any) map[string]any {
	system := SystemPromptOf(messages, systemPrompt, catalog)
	flags := CatalogFlagsOf(system, catalog)
	var items []map[string]any

	if strings.TrimSpace(system) == "" {
		items = append(items, checklistItem(
			"system_present", "System message present", "fail", "prompt",
			"No system message on turn", "Load catalog / inject system before agent turn",
		))
	} else {
		items = append(items, checklistItem(
			"system_present", "System message present", "pass", "prompt",
			fmt.Sprintf("chars=%d", len(system)), "",
		))
	}

	scopeSt := "fail"
	scopeDetail := "marker missing"
	scopeFix := "merge_scope_brief / borrow live brief"
	if asBool(flags["has_scope_brief"]) {
		scopeSt = "pass"
		scopeDetail = "marker ## SCOPE BRIEF"
		scopeFix = ""
	}
	items = append(items, checklistItem("scope_brief", "SCOPE BRIEF injected", scopeSt, "inject", scopeDetail, scopeFix))

	miniSt := "fail"
	miniDetail := "marker missing"
	miniFix := "ensure mini-schema in catalog brief"
	if asBool(flags["has_mini_schema"]) {
		miniSt = "pass"
		miniDetail = "marker ## MINI-SCHEMA"
		miniFix = ""
	}
	items = append(items, checklistItem("mini_schema", "MINI-SCHEMA injected", miniSt, "inject", miniDetail, miniFix))

	toolN := len(tools)
	if toolN == 0 && catalog != nil {
		if crTools, ok := catalog["tools"].([]any); ok && len(crTools) > 0 {
			toolN = len(crTools)
		} else if crVerbs, ok := catalog["verbs"].([]any); ok {
			toolN = len(crVerbs)
		}
	}
	toolSt := "warn"
	if toolN > 0 {
		toolSt = "pass"
	}
	items = append(items, checklistItem("tools_present", "Tools available to LLM", toolSt, "tools", fmt.Sprintf("count=%d", toolN), ""))

	verdict := "pass"
	for _, it := range items {
		switch asString(it["status"]) {
		case "fail":
			verdict = "fail"
		case "warn":
			if verdict != "fail" {
				verdict = "warn"
			}
		}
	}

	var summaryBits []string
	if asBool(flags["has_scope_brief"]) && asBool(flags["has_mini_schema"]) {
		summaryBits = append(summaryBits, "inject OK")
	} else {
		var missing []string
		if !asBool(flags["has_scope_brief"]) {
			missing = append(missing, "SCOPE BRIEF")
		}
		if !asBool(flags["has_mini_schema"]) {
			missing = append(missing, "MINI-SCHEMA")
		}
		summaryBits = append(summaryBits, "missing "+strings.Join(missing, ", "))
	}
	summaryBits = append(summaryBits, fmt.Sprintf("tools=%d", toolN))

	miniTypes, _ := flags["mini_entity_types"].([]string)
	if miniTypes == nil {
		miniTypes = []string{}
	}
	return map[string]any{
		"verdict": verdict,
		"summary": strings.Join(summaryBits, "; "),
		"items":   items,
		"inject": map[string]any{
			"has_scope_brief":   flags["has_scope_brief"],
			"has_mini_schema":   flags["has_mini_schema"],
			"mini_entity_types": miniTypes,
			"brief_sha12":       flags["brief_sha12"],
			"mini_sha12":        flags["mini_sha12"],
			"brief_preview":     flags["brief_preview"],
			"mini_preview":      flags["mini_preview"],
			"system_chars":      len(system),
		},
		"notes": []string{},
	}
}
