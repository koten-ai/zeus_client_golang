// SPDX-License-Identifier: BUSL-1.1

package detective

import (
	"fmt"
	"strings"
)

func injectSHA(inj map[string]any, kind string) string {
	nestedKey := "mini_schema"
	if kind == "brief" {
		nestedKey = "scope_brief"
	}
	if nested := asMap(inj[nestedKey]); nested != nil {
		if s := asString(nested["sha12"]); s != "" {
			return s
		}
	}
	top := "mini_sha12"
	if kind == "brief" {
		top = "brief_sha12"
	}
	if s := asString(inj[top]); s != "" {
		return s
	}
	return "-"
}

func hopLine(h map[string]any) string {
	name := hopName(h)
	if name == "" {
		name = "?"
	}
	rid := asString(h["req_id"])
	if rid == "" {
		rid = "-"
	}
	bits := []string{
		fmt.Sprintf("`%s`", name),
		fmt.Sprintf("status=%v", h["status"]),
		fmt.Sprintf("ms=%v", h["ms"]),
		fmt.Sprintf("req=`%s`", rid),
	}
	if err := asString(h["error"]); err != "" {
		bits = append(bits, "error="+clipRunes(err, 200))
	}
	return "- " + strings.Join(bits, " · ")
}

// SupportArgs is Python build_support_pack kwargs.
type SupportArgs struct {
	Headline       string
	TurnID         string
	ChatID         string
	SessionID      string
	PreferredReqID string
	ReqIDs         []string
	Hops           []map[string]any
	Playbooks      []map[string]any
	PromptVerdict  string
	Notes          []string
	Status         string
	Target         map[string]any
	ZeusURL        string
	ClientVersion  string
	Catalog        map[string]any
	ContractStatus string
	Inject         map[string]any
	LayerA         map[string]any
	Tokens         map[string]any
	ExportRef      string
}

// BuildSupportPack is G2-safe ticket markdown (Python build_support_pack).
func BuildSupportPack(in SupportArgs) map[string]any {
	tgt := in.Target
	if tgt == nil {
		tgt = map[string]any{}
	}
	cat := in.Catalog
	if cat == nil {
		cat = map[string]any{}
	}
	inj := in.Inject
	if inj == nil {
		inj = map[string]any{}
	}
	la := in.LayerA
	if la == nil {
		la = map[string]any{}
	}
	tok := in.Tokens
	if tok == nil {
		tok = map[string]any{}
	}
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	pref := dash(in.PreferredReqID)
	var reqBits []string
	for _, r := range in.ReqIDs {
		reqBits = append(reqBits, "`"+r+"`")
	}
	reqLine := "-"
	if len(reqBits) > 0 {
		reqLine = strings.Join(reqBits, ", ")
	}

	lines := []string{
		"# Detective support pack",
		"",
		"**Headline:** " + in.Headline,
		"**Status:** `" + dash(in.Status) + "`",
		"**Prompt verdict:** `" + dash(in.PromptVerdict) + "`",
		"",
		"## 1. Ids",
		"- chat_id: `" + dash(in.ChatID) + "`",
		"- turn_id: `" + dash(in.TurnID) + "`",
		"- session_id: `" + dash(in.SessionID) + "`",
		"",
		"## 2. Hops",
		"- preferred_req_id: `" + pref + "`",
		"- req_ids: " + reqLine,
	}
	if len(in.Hops) > 0 {
		for _, h := range in.Hops {
			lines = append(lines, hopLine(h))
		}
	} else {
		lines = append(lines, "- (no hops)")
	}

	toolsCount := cat["tools_count"]
	if toolsCount == nil {
		toolsCount = "-"
	}
	lines = append(lines,
		"",
		"## 3. Target",
		"- zeus.url: `"+dash(in.ZeusURL)+"`",
		fmt.Sprintf("- bucket/scope/collection: `%v` / `%v` / `%v`", dash(asString(tgt["bucket"])), dash(asString(tgt["scope"])), dash(asString(tgt["collection"]))),
		"- mode: `"+dash(asString(tgt["mode"]))+"`",
		"- client_version: `"+dash(in.ClientVersion)+"`",
		"",
		"## 4. Catalog / contract",
		fmt.Sprintf("- has_scope_brief: `%v`", cat["has_scope_brief"]),
		fmt.Sprintf("- has_mini_schema: `%v`", cat["has_mini_schema"]),
		fmt.Sprintf("- tools_count: `%v`", toolsCount),
		"- contract_status: `"+dash(in.ContractStatus)+"`",
		"",
		"## 5. Inject proof",
		"- brief_sha12: `"+injectSHA(inj, "brief")+"`",
		"- mini_sha12: `"+injectSHA(inj, "mini")+"`",
		"",
		"## 6. Hop errors / rows",
	)
	var errs []map[string]any
	for _, h := range in.Hops {
		if hopFailed(h) {
			errs = append(errs, h)
		}
	}
	if len(errs) > 0 {
		for _, h := range errs {
			lines = append(lines, hopLine(h))
		}
	} else {
		lines = append(lines, "- (none)")
	}

	promptN, _ := asInt(tok["prompt"])
	compN, _ := asInt(tok["completion"])
	totalN, _ := asInt(tok["total"])
	lines = append(lines,
		"",
		"## 7. Layer A",
		"- via: `"+dash(asString(la["via"]))+"`",
		"- confidence: `"+dash(asString(la["confidence"]))+"`",
		"- policy_action: `"+dash(asString(la["policy_action"]))+"`",
		fmt.Sprintf("- synthetic: `%v`", la["synthetic"]),
		"",
		"## 8. Tokens",
		fmt.Sprintf("- prompt/completion/total: `%d` / `%d` / `%d`", promptN, compN, totalN),
		fmt.Sprintf("- ok: `%v`", tok["ok"]),
		"",
		"## 9. Journal export",
		"- export_ref: `"+dash(firstNonEmpty(in.ExportRef, in.TurnID))+"`",
		"",
		"## Playbooks",
	)
	if len(in.Playbooks) == 0 {
		lines = append(lines, "- (none fired)")
	} else {
		for _, p := range in.Playbooks {
			lines = append(lines, fmt.Sprintf("- **%s** (%s): %s", p["id"], p["severity"], p["summary"]))
		}
	}
	if len(in.Notes) > 0 {
		lines = append(lines, "", "## Notes (truncated)")
		max := 12
		if len(in.Notes) < max {
			max = len(in.Notes)
		}
		for _, n := range in.Notes[:max] {
			lines = append(lines, "- "+n)
		}
	}
	md := strings.Join(lines, "\n") + "\n"
	var pbIDs []string
	for _, p := range in.Playbooks {
		if id := asString(p["id"]); id != "" {
			pbIDs = append(pbIDs, id)
		}
	}
	var prefOut any
	if in.PreferredReqID != "" {
		prefOut = in.PreferredReqID
	}
	var exp any
	if ref := firstNonEmpty(in.ExportRef, in.TurnID); ref != "" {
		exp = ref
	}
	return map[string]any{
		"headline":         in.Headline,
		"markdown":         md,
		"preferred_req_id": prefOut,
		"playbook_ids":     pbIDs,
		"export_ref":       exp,
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
