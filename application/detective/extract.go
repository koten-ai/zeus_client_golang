// SPDX-License-Identifier: BUSL-1.1

package detective

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/koten-ai/zeus_client_golang/application/projectors"
	"github.com/koten-ai/zeus_client_golang/domain"
)

// InjectSectionMaxBytes caps rewind inject text (~96 KiB; Hub inject_inspect).
const InjectSectionMaxBytes = 96 << 10

const previewMax = 400

var (
	reScopeBrief = regexp.MustCompile(`(?m)^##\s+SCOPE\s+BRIEF\b`)
	reMiniSchema = regexp.MustCompile(`(?m)^##\s+MINI-SCHEMA\b`)
)

// SHA12 is domain.SHA12 (empty → "").
func SHA12(text string) string { return domain.SHA12(text) }

// SliceBlock is domain.SliceBlock.
func SliceBlock(text, kind string) string { return domain.SliceBlock(text, kind) }

func utf8Len(text string) int { return len([]byte(text)) }

func parseBriefScopeMode(section string) (scopeLine, modeLine string) {
	for _, ln := range strings.Split(section, "\n") {
		t := strings.TrimSpace(ln)
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "scope:") {
			rest := strings.TrimSpace(t[len("scope:"):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				scopeLine = fields[0]
			}
			if scopeLine == "(unscoped)" {
				scopeLine = ""
			}
		}
		if strings.HasPrefix(low, "mode:") {
			rest := strings.TrimSpace(t[len("mode:"):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				modeLine = fields[0]
			}
		}
	}
	return scopeLine, modeLine
}

func parseMiniEntityTypes(section string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, ln := range strings.Split(section, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "### ") {
			continue
		}
		rest := strings.TrimSpace(t[4:])
		name := rest
		cut := -1
		for i, ch := range rest {
			if ch == ' ' || ch == '\t' || ch == '(' {
				cut = i
				break
			}
		}
		if cut > 0 {
			name = strings.TrimSpace(rest[:cut])
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func capSectionText(text string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		maxBytes = InjectSectionMaxBytes
	}
	raw := []byte(text)
	if len(raw) <= maxBytes {
		return text, false
	}
	clipped := string(raw[:maxBytes])
	if !utf8.ValidString(clipped) {
		clipped = strings.ToValidUTF8(clipped, "")
	}
	return clipped + "\n…[truncated]", true
}

func injectSection(sliceText, kind string, includeText bool) map[string]any {
	text := strings.TrimSpace(sliceText)
	if text == "" {
		return map[string]any{"present": false, "chars": 0}
	}
	out := map[string]any{
		"present": true,
		"chars":   utf8Len(text),
		"sha12":   SHA12(text),
		"preview": clipRunes(text, previewMax),
	}
	switch kind {
	case "brief":
		scopeLine, modeLine := parseBriefScopeMode(text)
		if scopeLine != "" {
			out["scope_line"] = scopeLine
		}
		if modeLine != "" {
			out["mode_line"] = modeLine
		}
	case "mini":
		if types := parseMiniEntityTypes(text); len(types) > 0 {
			out["entity_types"] = types
		}
	}
	if includeText {
		capped, truncated := capSectionText(text, InjectSectionMaxBytes)
		out["text"] = capped
		if truncated {
			out["truncated"] = true
		}
	}
	return out
}

func clipRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// CollectReqIDs is unique hop req_id order (Python collect_req_ids).
func CollectReqIDs(hops []map[string]any) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, h := range hops {
		rid := asString(h["req_id"])
		if rid == "" {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		out = append(out, rid)
	}
	return out
}

// PreferredReqID is the Detective join hop (Python preferred_req_id).
func PreferredReqID(hops []map[string]any) string {
	anyHops := make([]any, len(hops))
	for i, h := range hops {
		anyHops[i] = h
	}
	if id := projectors.SelectPrimaryReqID(anyHops); id != "" {
		return id
	}
	ids := CollectReqIDs(hops)
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
}

// CountToolErrors counts ok=false / HTTP >=400 hops plus llm_error steps.
func CountToolErrors(hops []map[string]any, steps []map[string]any) int {
	n := 0
	for _, h := range hops {
		if hopFailed(h) {
			n++
		}
	}
	for _, s := range steps {
		if asString(s["type"]) == "llm_error" {
			n++
		}
	}
	return n
}

func hopFailed(h map[string]any) bool {
	if h == nil {
		return false
	}
	if ok, isBool := h["ok"].(bool); isBool && !ok {
		return true
	}
	st, ok := asInt(h["status"])
	return ok && st >= 400
}

// CountRowsSignal is best-effort non-empty hop count (Python count_rows_signal).
func CountRowsSignal(hops []map[string]any) int {
	n := 0
	for _, h := range hops {
		if hopFailed(h) {
			continue
		}
		snip := asString(h["snippet"])
		if strings.Contains(snip, `"rows"`) || strings.Contains(snip, `"items"`) ||
			strings.Contains(snip, `"node_ids"`) || strings.Contains(snip, `"src_keys"`) {
			n++
			continue
		}
		if ok, isBool := h["ok"].(bool); isBool && ok {
			trim := strings.TrimSpace(snip)
			if trim != "" && trim != "{}" && trim != "null" {
				n++
			}
		}
	}
	return n
}

// SystemPromptOf picks system text from kwargs / catalog / messages.
func SystemPromptOf(messages []map[string]any, systemPrompt string, catalog map[string]any) string {
	if strings.TrimSpace(systemPrompt) != "" {
		return systemPrompt
	}
	if catalog != nil {
		if sm, ok := catalog["system_message"].(string); ok && strings.TrimSpace(sm) != "" {
			return sm
		}
	}
	for _, m := range messages {
		if asString(m["role"]) == "system" {
			if c, ok := m["content"].(string); ok && strings.TrimSpace(c) != "" {
				return c
			}
		}
	}
	return ""
}

// CatalogFlagsOf is Python catalog_flags_of.
func CatalogFlagsOf(system string, catalog map[string]any) map[string]any {
	cat := catalog
	if cat == nil {
		cat = map[string]any{}
	}
	hasScope := asBool(cat["has_scope_brief"])
	hasMini := asBool(cat["has_mini_schema"])
	if system != "" {
		if !hasScope && reScopeBrief.MatchString(system) {
			hasScope = true
		}
		if !hasMini && reMiniSchema.MatchString(system) {
			hasMini = true
		}
	}
	var miniTypes []string
	switch raw := cat["mini_entity_types"].(type) {
	case []string:
		miniTypes = append([]string{}, raw...)
	case []any:
		for _, x := range raw {
			miniTypes = append(miniTypes, asString(x))
		}
	}
	briefSlice := SliceBlock(system, "brief")
	miniSlice := SliceBlock(system, "mini")
	var briefSha, miniSha, briefPrev, miniPrev any
	if hasScope && briefSlice != "" {
		briefSha = SHA12(briefSlice)
		briefPrev = clipRunes(briefSlice, previewMax)
	}
	if hasMini && miniSlice != "" {
		miniSha = SHA12(miniSlice)
		miniPrev = clipRunes(miniSlice, previewMax)
	}
	return map[string]any{
		"has_scope_brief":   hasScope,
		"has_mini_schema":   hasMini,
		"mini_entity_types": miniTypes,
		"brief_sha12":       briefSha,
		"mini_sha12":        miniSha,
		"brief_preview":     briefPrev,
		"mini_preview":      miniPrev,
	}
}

// InjectForSessionTrace is Hub-shaped zeus_response.inject / public_trace.inject.
func InjectForSessionTrace(system string, source string, rewind bool) map[string]any {
	if source == "" {
		source = "client_llm"
	}
	sys := system
	return map[string]any{
		"source":       source,
		"system_chars": utf8Len(sys),
		"scope_brief":  injectSection(SliceBlock(sys, "brief"), "brief", rewind),
		"mini_schema":  injectSection(SliceBlock(sys, "mini"), "mini", rewind),
	}
}

// NotesBlob joins notes with newlines.
func NotesBlob(notes []string) string {
	return strings.Join(notes, "\n")
}

// HopErrorBlob concatenates failing hop snippets.
func HopErrorBlob(hops []map[string]any) string {
	var parts []string
	for _, h := range hops {
		if hopFailed(h) {
			s := asString(h["snippet"])
			if s == "" {
				s = asString(h["error"])
			}
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(v)
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(x)
		return n, err == nil
	default:
		return 0, false
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func hopName(h map[string]any) string {
	n := asString(h["name"])
	if n == "" {
		n = asString(h["path_class"])
	}
	return n
}
