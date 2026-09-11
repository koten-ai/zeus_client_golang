// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// TrailHeading is the Bag B tool-trail marker (Python TRAIL_HEADING).
const TrailHeading = "ZEUS_TOOL_TRAIL (this turn):"

// ErrorClassFor classifies a Zeus hop for later LLM rounds (Python error_class_for).
func ErrorClassFor(ok bool, status int, err string) string {
	if ok {
		return ""
	}
	if status == 409 {
		return "contract_mismatch"
	}
	low := strings.ToLower(err)
	if status == 429 || status == 503 || strings.Contains(low, "timeout") {
		return "retryable_later"
	}
	if status >= 400 && status < 500 {
		return "do_not_retry_same_args"
	}
	return "zeus_error"
}

func trailHint(ok bool, status int, empty bool, errorClass string) string {
	if ok && empty {
		return "empty"
	}
	if ok {
		return "success"
	}
	if errorClass == "contract_mismatch" || errorClass == "do_not_retry_same_args" {
		return "do_not_retry_same_args"
	}
	if errorClass == "retryable_later" {
		return "retryable_later"
	}
	return ""
}

// TrailEntryFromHop is one slim hop row (Python trail_entry_from_hop).
func TrailEntryFromHop(hop map[string]any, seq int, empty bool) map[string]any {
	if hop == nil {
		hop = map[string]any{}
	}
	ok := asBool(hop["ok"])
	status := asInt(hop["status"])
	if status == 0 {
		status = asInt(hop["status_code"])
	}
	err := ""
	if hop["error"] != nil {
		err = fmt.Sprint(hop["error"])
		if err == "<nil>" {
			err = ""
		}
	}
	klass := ErrorClassFor(ok, status, err)
	verb := strings.TrimSpace(asString(hop["name"]))
	if verb == "" {
		verb = strings.TrimSpace(asString(hop["verb"]))
	}
	entry := map[string]any{
		"seq": seq,
		"ok":  ok,
	}
	if verb != "" {
		entry["verb"] = verb
		entry["l0"] = "zeus." + verb
	}
	if status != 0 {
		entry["http_status"] = status
	}
	if rid := asString(hop["req_id"]); rid != "" {
		entry["req_id"] = rid
	}
	if klass != "" {
		entry["error_class"] = klass
	}
	if h := trailHint(ok, status, empty, klass); h != "" {
		entry["hint"] = h
	}
	path := asString(hop["url"])
	if path == "" {
		path = asString(hop["path"])
	}
	if path != "" {
		entry["path"] = path
	}
	return entry
}

// RenderTrailInject is the Bag B trail block (Python render_trail_inject).
func RenderTrailInject(entries []map[string]any, maxEntries int) string {
	if maxEntries < 0 {
		maxEntries = 0
	}
	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}
	if len(entries) == 0 {
		return ""
	}
	lines := []string{TrailHeading}
	for _, e := range entries {
		seq := "?"
		if e["seq"] != nil {
			seq = fmt.Sprint(e["seq"])
		}
		l0 := asString(e["l0"])
		if l0 == "" {
			l0 = asString(e["verb"])
		}
		if l0 == "" {
			l0 = "hop"
		}
		bits := []string{seq + ".", l0}
		if asBool(e["ok"]) {
			bits = append(bits, "ok")
		} else {
			bits = append(bits, "fail")
		}
		if rid := asString(e["req_id"]); rid != "" {
			bits = append(bits, "req_id="+rid)
		}
		if klass := asString(e["error_class"]); klass != "" {
			bits = append(bits, klass)
		}
		if h := asString(e["hint"]); h != "" {
			bits = append(bits, "hint="+h)
		}
		lines = append(lines, strings.Join(bits, " "))
	}
	return strings.Join(lines, "\n")
}

// UpsertTrailOnSystem splices trail into the system message (Bag B).
// Never touches user text.
func UpsertTrailOnSystem(messages []any, snippet string) {
	if snippet == "" || len(messages) == 0 {
		return
	}
	sys, ok := messages[0].(map[string]any)
	if !ok {
		return
	}
	if asString(sys["role"]) != "system" {
		return
	}
	content := asString(sys["content"])
	if strings.Contains(content, TrailHeading) {
		head, _, _ := strings.Cut(content, TrailHeading)
		sys["content"] = strings.TrimRight(head, trailingWS) + "\n\n" + snippet
		return
	}
	sys["content"] = strings.TrimRight(content, trailingWS) + "\n\n" + snippet
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		b, _ := strconv.ParseBool(x)
		return b
	default:
		return false
	}
}

func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}
