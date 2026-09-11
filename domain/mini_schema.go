// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	// MissingMiniSchema is classify_mini_schema error_class when empty.
	MissingMiniSchema = "missing_mini_schema"
)

var (
	miniHeaderRe = regexp.MustCompile(`^###\s+(?P<ent>.+?)\s+\(fields:\s*(?P<n>\d+)\)\s*$`)
	miniFieldRe  = regexp.MustCompile(
		`^\s+-\s+(?P<path>\S+)\s+(?P<kind>\S+)\s+\[(?P<idx>[^\]]+)\]` +
			`(?:\s+(?P<trailer>.*\S))?\s*$`,
	)
	miniInverseRe = regexp.MustCompile(
		`^\s+\x{2190}\s+(?P<token>\S+)\s+\((?P<kind>[^)]+)\)\s+--\s+(?P<recipe>.*\S)?\s*$`,
	)
	fkToRe        = regexp.MustCompile(`fk_to=(\S+)`)
	viaRe         = regexp.MustCompile(`via=(\S+)`)
	exRe          = regexp.MustCompile(`ex:\s*(.+)$`)
	scopeLineRe   = regexp.MustCompile(`(?m)^scope:\s+(\S+)`)
	modeLineRe    = regexp.MustCompile(`(?m)^mode:\s+(\S+)`)
	nonFilterable = map[string]struct{}{
		"display":  {},
		"text_fts": {},
	}
)

func namedMatch(re *regexp.Regexp, s string) map[string]string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for i, name := range re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		out[name] = m[i]
	}
	return out
}

// SystemPromptText prefers messages[0].content; falls back to instructions.system_prompt.
func SystemPromptText(chatReq map[string]any) string {
	if chatReq == nil {
		return ""
	}
	content := ""
	if messages, ok := chatReq["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			content = asString(msg["content"])
		}
	}
	if !strings.Contains(content, "## MINI-SCHEMA") && !strings.Contains(content, "## SCOPE BRIEF") {
		if instr, ok := chatReq["instructions"].(map[string]any); ok && instr != nil {
			sp := asString(instr["system_prompt"])
			if strings.Contains(sp, "## MINI-SCHEMA") || strings.Contains(sp, "## SCOPE BRIEF") {
				return sp
			}
		}
	}
	return content
}

func emptyMiniSchema() map[string]any {
	return map[string]any{
		"scope":        "",
		"mode":         "",
		"entity_types": map[string]any{},
	}
}

// miniSchemaBlock is Python rest split at the next H2 (`\n##\s+` not MINI-SCHEMA).
// `###` entity headers must stay inside the block (RE2 has no lookahead).
func miniSchemaBlock(content string) string {
	start := strings.Index(content, "## MINI-SCHEMA")
	if start < 0 {
		return ""
	}
	rest := content[start:]
	from := 1
	for {
		i := strings.Index(rest[from:], "\n##")
		if i < 0 {
			return rest
		}
		abs := from + i
		afterHashes := rest[abs+len("\n"):]
		if !strings.HasPrefix(afterHashes, "##") {
			from = abs + 1
			continue
		}
		after := afterHashes[2:]
		if after == "" || (after[0] != ' ' && after[0] != '\t') {
			from = abs + 1
			continue
		}
		title := strings.TrimLeft(after, " \t")
		if strings.HasPrefix(title, "MINI-SCHEMA") {
			from = abs + 1
			continue
		}
		return rest[:abs]
	}
}

// GetMiniSchema is the public structured MINI-SCHEMA parse.
// Empty entity_types when the brief is absent — do not invent.
func GetMiniSchema(chatReq map[string]any, values bool) map[string]any {
	out := emptyMiniSchema()
	if chatReq == nil {
		return out
	}
	content := SystemPromptText(chatReq)
	if content == "" {
		return out
	}
	if m := scopeLineRe.FindStringSubmatch(content); m != nil {
		out["scope"] = m[1]
	}
	if m := modeLineRe.FindStringSubmatch(content); m != nil {
		out["mode"] = m[1]
	}
	block := miniSchemaBlock(content)
	if block == "" {
		return out
	}

	entityTypes := out["entity_types"].(map[string]any)
	var cur string
	inInverse := false
	for _, line := range strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n") {
		if hm := namedMatch(miniHeaderRe, line); hm != nil {
			cur = hm["ent"]
			n, _ := strconv.Atoi(hm["n"])
			entityTypes[cur] = map[string]any{
				"field_count": n,
				"fields":      map[string]any{},
				"inverse_fks": []any{},
			}
			inInverse = false
			continue
		}
		if cur == "" {
			continue
		}
		if strings.TrimSpace(line) == "inverse_fks:" {
			inInverse = true
			continue
		}
		ent, _ := entityTypes[cur].(map[string]any)
		if inInverse {
			if im := namedMatch(miniInverseRe, line); im != nil {
				fromEnt, fromPath, _ := strings.Cut(im["token"], ".")
				inv, _ := ent["inverse_fks"].([]any)
				ent["inverse_fks"] = append(inv, map[string]any{
					"from_entity": fromEnt,
					"from_path":   fromPath,
					"kind":        im["kind"],
				})
			}
			continue
		}
		fm := namedMatch(miniFieldRe, line)
		if fm == nil {
			continue
		}
		kind := fm["kind"]
		idx := fm["idx"]
		_, nonFilt := nonFilterable[kind]
		field := map[string]any{
			"kind":       kind,
			"indexed":    idx,
			"filterable": !nonFilt && idx != "none" && idx != "",
		}
		trailer := fm["trailer"]
		if fk := fkToRe.FindStringSubmatch(trailer); fk != nil {
			field["fk_to"] = fk[1]
		}
		if via := viaRe.FindStringSubmatch(trailer); via != nil {
			field["via"] = via[1]
		}
		trim := strings.TrimLeft(trailer, " \t")
		if strings.HasPrefix(trim, "--") {
			field["note"] = strings.TrimSpace(trim[2:])
		}
		if values {
			if ex := exRe.FindStringSubmatch(trailer); ex != nil {
				var examples []string
				for _, e := range strings.Split(ex[1], ",") {
					e = strings.TrimSpace(e)
					if e != "" {
						examples = append(examples, e)
					}
				}
				field["examples"] = examples
			}
		}
		fields, _ := ent["fields"].(map[string]any)
		fields[fm["path"]] = field
	}
	return out
}

// ClassifyMiniSchema is the stable error_class for missing MINI-SCHEMA.
func ClassifyMiniSchema(parsed map[string]any) map[string]any {
	out := emptyMiniSchema()
	if parsed != nil {
		if cloned, ok := cloneJSON(parsed).(map[string]any); ok {
			out = cloned
		}
	}
	entities, _ := out["entity_types"].(map[string]any)
	present := len(entities) > 0
	out["result"] = present
	if present {
		out["error_class"] = nil
	} else {
		out["error_class"] = MissingMiniSchema
	}
	return out
}
