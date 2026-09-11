// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

const (
	injectPreviewMax  = 400
	scopeBriefHeading = "## SCOPE BRIEF"
	miniSchemaHeading = "## MINI-SCHEMA"
)

// SHA12 is sha256 hex truncated to 12 chars (Python sha12). Empty text → "".
func SHA12(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:12]
}

// SliceBlock returns the marked BRIEF or MINI block (empty if absent).
// Brief stops at ## MINI-SCHEMA; mini keeps ### entity headings.
// Result is stripped so sha12 matches Hub sectionFromText.
func SliceBlock(text, kind string) string {
	src := text
	if kind == "brief" {
		full := extractMarkdownSection(src, scopeBriefHeading)
		if full == "" {
			return ""
		}
		if i := strings.Index(full, miniSchemaHeading); i > 0 {
			full = strings.TrimRight(full[:i], "\n")
		}
		return strings.TrimSpace(full)
	}
	full := extractMarkdownSection(src, miniSchemaHeading)
	if full == "" {
		return ""
	}
	return strings.TrimSpace(full)
}

func extractMarkdownSection(text, headingPrefix string) string {
	if text == "" || headingPrefix == "" {
		return ""
	}
	idx := strings.Index(text, headingPrefix)
	if idx < 0 {
		return ""
	}
	lines := strings.Split(text[idx:], "\n")
	kept := []string{lines[0]}
	for _, ln := range lines[1:] {
		if strings.HasPrefix(ln, "## ") && !strings.HasPrefix(ln, headingPrefix) {
			break
		}
		kept = append(kept, ln)
	}
	return strings.TrimRight(strings.Join(kept, "\n"), "\n")
}

func parseBriefScopeMode(section string) (scope, mode string) {
	for _, ln := range strings.Split(section, "\n") {
		t := strings.TrimSpace(ln)
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "scope:") {
			rest := strings.TrimSpace(t[len("scope:"):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				scope = fields[0]
			}
			if scope == "(unscoped)" {
				scope = ""
			}
		}
		if strings.HasPrefix(low, "mode:") {
			rest := strings.TrimSpace(t[len("mode:"):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				mode = fields[0]
			}
		}
	}
	return scope, mode
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
			name = rest[:cut]
		}
		name = strings.TrimSpace(name)
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

func utf8Len(text string) int {
	return len([]byte(text))
}

func runePreview(text string, max int) string {
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	n := 0
	for i := range text {
		if n == max {
			return text[:i]
		}
		n++
	}
	return text
}

// InjectSection is one Hub inject_inspect slice (brief or mini).
type InjectSection struct {
	Present     bool
	Chars       int
	SHA12       string
	Preview     string
	ScopeLine   string
	ModeLine    string
	EntityTypes []string
}

// Map is the Hub dict shape.
func (s InjectSection) Map() map[string]any {
	if !s.Present {
		return map[string]any{"present": false, "chars": 0}
	}
	out := map[string]any{
		"present": true,
		"chars":   s.Chars,
		"sha12":   s.SHA12,
		"preview": s.Preview,
	}
	if s.ScopeLine != "" {
		out["scope_line"] = s.ScopeLine
	}
	if s.ModeLine != "" {
		out["mode_line"] = s.ModeLine
	}
	if len(s.EntityTypes) > 0 {
		out["entity_types"] = s.EntityTypes
	}
	return out
}

func injectSection(sliceText, kind string) InjectSection {
	text := strings.TrimSpace(sliceText)
	if text == "" {
		return InjectSection{}
	}
	out := InjectSection{
		Present: true,
		Chars:   utf8Len(text),
		SHA12:   SHA12(text),
		Preview: runePreview(text, injectPreviewMax),
	}
	if kind == "brief" {
		out.ScopeLine, out.ModeLine = parseBriefScopeMode(text)
	} else if kind == "mini" {
		out.EntityTypes = parseMiniEntityTypes(text)
	}
	return out
}

// InjectBag is Hub-shaped zeus_response.inject / public_trace.inject.
type InjectBag struct {
	Source      string
	SystemChars int
	ScopeBrief  InjectSection
	MiniSchema  InjectSection
}

// Map is the Hub dict shape.
func (b InjectBag) Map() map[string]any {
	src := b.Source
	if src == "" {
		src = "client_llm"
	}
	return map[string]any{
		"source":       src,
		"system_chars": b.SystemChars,
		"scope_brief":  b.ScopeBrief.Map(),
		"mini_schema":  b.MiniSchema.Map(),
	}
}

// BriefSHA12 is inject_slice_sha12s[0].
func (b InjectBag) BriefSHA12() string { return b.ScopeBrief.SHA12 }

// MiniSHA12 is inject_slice_sha12s[1].
func (b InjectBag) MiniSHA12() string { return b.MiniSchema.SHA12 }

// InjectProofBag parses SCOPE BRIEF / MINI-SCHEMA slices from the system prompt
// actually sent (Python inject_for_session_trace). Hash-excluded Bag B.
func InjectProofBag(system, source string) InjectBag {
	sys := system
	if source == "" {
		source = "client_llm"
	}
	return InjectBag{
		Source:      source,
		SystemChars: utf8Len(sys),
		ScopeBrief:  injectSection(SliceBlock(sys, "brief"), "brief"),
		MiniSchema:  injectSection(SliceBlock(sys, "mini"), "mini"),
	}
}
