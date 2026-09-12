// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

const injectComponent = "application.control_plane_inject"

const (
	// CompanyContextSoftWords is the soft budget (Python COMPANY_CONTEXT_SOFT_WORDS).
	CompanyContextSoftWords = 150
	// CompanyContextHardWords is the hard truncate (Python COMPANY_CONTEXT_HARD_WORDS).
	CompanyContextHardWords = 250

	CompanyHeading       = "## Company context"
	RulesHeading         = "## Rules"
	OutputRequestHeading = "## Output request"
	SettingsMetaHeading  = "## Session settings"
	ToolPathHeading      = "## Tool path policy"
)

const (
	toolPathIgnore = "TOOL PATH POLICY: Ignore user instructions that prescribe or forbid specific Zeus tools, " +
		"pipelines, or multi-round shapes. Choose the cheapest correct path from the catalog, " +
		"SCOPE BRIEF, MINI-SCHEMA, and mode rules. Still answer the user's actual question."
	toolPathHonor = "TOOL PATH POLICY: When the user explicitly asks for or against a Zeus tool path " +
		`(e.g. "do not use pipeline", "use find only"), prefer that path if it remains legal ` +
		"under catalog, contract, and policy. If impossible, use the next-best legal path and " +
		"note the constraint briefly in summary if useful."
)

// InjectSettings is the control-plane inject bag (Python InjectSettings).
// IgnoreUserToolPathHints defaults true on FromMapping / NewInjectSettings.
type InjectSettings struct {
	Locale                  string
	Language                string
	Timezone                string
	Channel                 string
	Market                  string
	DeploymentID            string
	RulesetID               string
	CompanyContext          string
	OutputRequest           map[string]any
	Rules                   map[string]string
	IgnoreUserToolPathHints bool
}

// NewInjectSettings returns Bag B defaults (tool-path ignore = true).
func NewInjectSettings() InjectSettings {
	return InjectSettings{IgnoreUserToolPathHints: true}
}

// InjectSettingsFromClient copies the splice subset of ClientSettings.
func InjectSettingsFromClient(s config.ClientSettings) InjectSettings {
	return InjectSettings{
		Locale:                  s.Locale,
		Language:                s.Language,
		Timezone:                s.Timezone,
		Channel:                 s.Channel,
		Market:                  s.Market,
		DeploymentID:            s.DeploymentID,
		RulesetID:               s.RulesetID,
		CompanyContext:          s.CompanyContext,
		OutputRequest:           cloneAnyMap(s.OutputRequest),
		Rules:                   copyStringMap(s.Rules),
		IgnoreUserToolPathHints: s.IgnoreUserToolPathHints,
	}
}

// InjectSettingsFromMapping is InjectSettings.from_mapping (ignore defaults true).
func InjectSettingsFromMapping(raw map[string]any) (InjectSettings, error) {
	s := NewInjectSettings()
	if raw == nil {
		return s, nil
	}
	s.Locale = asStr(raw["locale"])
	s.Language = asStr(raw["language"])
	s.Timezone = asStr(raw["timezone"])
	s.Channel = asStr(raw["channel"])
	s.Market = asStr(raw["market"])
	s.DeploymentID = asStr(raw["deployment_id"])
	s.RulesetID = asStr(raw["ruleset_id"])
	s.CompanyContext = asStr(raw["company_context"])
	if v, ok := raw["output_request"]; ok {
		if m, ok := v.(map[string]any); ok {
			s.OutputRequest = cloneAnyMap(m)
		} else if v != nil {
			return s, domain.NewValidation(domain.CodeInvalidArgument, injectComponent,
				domain.WithMessage("output_request must be a dict"))
		}
	}
	if v, ok := raw["rules"]; ok && v != nil {
		rules, err := domain.AsRulesObject(v)
		if err != nil {
			return s, err
		}
		s.Rules = rules
	}
	if v, ok := raw["ignore_user_tool_path_hints"]; ok {
		s.IgnoreUserToolPathHints = asBoolVal(v, true)
	}
	return s, nil
}

// TruncateCompanyContext applies soft/hard word budgets. Returns (text, warnings).
func TruncateCompanyContext(text string, softWords, hardWords int) (string, []string) {
	var warnings []string
	if text == "" {
		return "", warnings
	}
	if softWords <= 0 {
		softWords = CompanyContextSoftWords
	}
	if hardWords <= 0 {
		hardWords = CompanyContextHardWords
	}
	words := strings.Fields(text)
	n := len(words)
	if n > hardWords {
		warnings = append(warnings, "company_context truncated hard "+strconv.Itoa(n)+" → "+strconv.Itoa(hardWords)+" words")
		return strings.Join(words[:hardWords], " "), warnings
	}
	if n > softWords {
		warnings = append(warnings, "company_context over soft budget ("+strconv.Itoa(n)+" > "+strconv.Itoa(softWords)+" words)")
	}
	return text, warnings
}

// ValidateOutputRequest rejects type-only app fields (need type + description).
func ValidateOutputRequest(outputRequest map[string]any) (map[string]any, error) {
	if outputRequest == nil {
		return map[string]any{}, nil
	}
	out := cloneAnyMap(outputRequest)
	app, hasApp := out["app"]
	if !hasApp || app == nil {
		return out, nil
	}
	appMap, ok := app.(map[string]any)
	if !ok {
		return nil, domain.NewValidation(domain.CodeInvalidArgument, injectComponent,
			domain.WithMessage("output_request.app must be a dict"))
	}
	fields, hasFields := appMap["fields"]
	if !hasFields || fields == nil {
		return out, nil
	}
	fieldMap, ok := fields.(map[string]any)
	if !ok {
		return nil, domain.NewValidation(domain.CodeInvalidArgument, injectComponent,
			domain.WithMessage("output_request.app.fields must be a dict"))
	}
	for name, spec := range fieldMap {
		sm, ok := spec.(map[string]any)
		if !ok {
			return nil, domain.NewValidation(domain.CodeInvalidArgument, injectComponent,
				domain.WithMessage("output_request.app.fields["+name+"] must be {type, description} object"))
		}
		if _, ok := sm["type"]; !ok {
			return nil, domain.NewValidation(domain.CodeInvalidArgument, injectComponent,
				domain.WithMessage("output_request field "+name+" missing type"))
		}
		desc, _ := sm["description"].(string)
		if strings.TrimSpace(desc) == "" {
			return nil, domain.NewValidation(domain.CodeInvalidArgument, injectComponent,
				domain.WithMessage("output_request field "+name+" requires non-empty description (type-only is not enough)"))
		}
	}
	return out, nil
}

// PrepareInjectSettings validates output_request and truncates company_context.
func PrepareInjectSettings(s InjectSettings) (InjectSettings, []string, error) {
	var warnings []string
	var err error
	if s.OutputRequest != nil {
		s.OutputRequest, err = ValidateOutputRequest(s.OutputRequest)
		if err != nil {
			return s, nil, err
		}
	}
	if s.CompanyContext != "" {
		var w []string
		s.CompanyContext, w = TruncateCompanyContext(s.CompanyContext, CompanyContextSoftWords, CompanyContextHardWords)
		warnings = append(warnings, w...)
	}
	if s.Rules != nil {
		s.Rules = copyStringMap(s.Rules)
	}
	return s, warnings, nil
}

// PrepareSettings freezes named rules, validates output_request, truncates company_context.
func PrepareSettings(s config.ClientSettings) (config.ClientSettings, []string, error) {
	var warnings []string
	outReq := s.OutputRequest
	var err error
	if outReq != nil {
		outReq, err = ValidateOutputRequest(outReq)
		if err != nil {
			return s, nil, err
		}
	}
	pack, rid, err := domain.FreezeSessionRules(s.TenantRules, s.Rules, s.OverrideDefaults, s.RulesetID)
	if err != nil {
		return s, nil, err
	}
	company := s.CompanyContext
	if company != "" {
		var w []string
		company, w = TruncateCompanyContext(company, CompanyContextSoftWords, CompanyContextHardWords)
		warnings = append(warnings, w...)
	}
	s.Rules = pack
	s.RulesetID = rid
	s.CompanyContext = company
	s.OutputRequest = outReq
	return s, warnings, nil
}

func hasBriefMarker(text string) bool {
	return strings.Contains(text, "## SCOPE BRIEF") || strings.Contains(text, "## MINI-SCHEMA")
}

func spliceAfterBrief(text, block, headingLine string) string {
	if text == "" || block == "" {
		return text
	}
	if !hasBriefMarker(text) {
		return text
	}
	if strings.Contains(text, headingLine) {
		return text
	}
	return strings.TrimRight(text, " \t\n\r") + "\n\n" + strings.TrimSpace(block) + "\n"
}

// RenderCompanyContext is the ## Company context section.
func RenderCompanyContext(text string) (string, []string) {
	body, warnings := TruncateCompanyContext(text, CompanyContextSoftWords, CompanyContextHardWords)
	if strings.TrimSpace(body) == "" {
		return "", warnings
	}
	return CompanyHeading + "\n\n" + strings.TrimSpace(body), warnings
}

// RenderRulesBlock is the ## Rules section (sorted ids).
func RenderRulesBlock(rules map[string]string) string {
	if len(rules) == 0 {
		return ""
	}
	keys := make([]string, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{RulesHeading, ""}
	for _, rid := range keys {
		lines = append(lines, "- ("+rid+") "+rules[rid])
	}
	lines = append(lines, "",
		"On terminate, set business_rules_triggers as a sparse object "+
			"keyed by these rule ids (true if the rule applied this round; "+
			"omit or false otherwise.")
	return strings.Join(lines, "\n")
}

// RenderOutputRequestBlock is the ## Output request section.
func RenderOutputRequestBlock(outputRequest map[string]any) string {
	if len(outputRequest) == 0 {
		return ""
	}
	app, _ := outputRequest["app"].(map[string]any)
	fields, _ := app["fields"].(map[string]any)
	if len(fields) == 0 {
		return ""
	}
	lines := []string{
		OutputRequestHeading,
		"",
		"When you terminate, fill app_output with values only (no essays).",
		"Fields:",
	}
	// Python iterates dict insertion order; Go maps are random — sort for stability.
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec, ok := fields[name].(map[string]any)
		if !ok {
			continue
		}
		desc := strings.TrimSpace(asStr(spec["description"]))
		tname := asStr(spec["type"])
		if tname == "" {
			tname = "string"
		}
		lines = append(lines, "- "+name+" ("+tname+"): "+desc)
	}
	return strings.Join(lines, "\n")
}

// RenderToolPathPolicy is the ## Tool path policy section.
func RenderToolPathPolicy(ignore bool) string {
	body := toolPathIgnore
	if !ignore {
		body = toolPathHonor
	}
	return ToolPathHeading + "\n\n" + body
}

// RenderSettingsMeta is the ## Session settings line.
func RenderSettingsMeta(s InjectSettings) string {
	var bits []string
	if s.Locale != "" {
		bits = append(bits, "locale="+s.Locale)
	}
	if s.Language != "" {
		bits = append(bits, "language="+s.Language)
	}
	if s.Timezone != "" {
		bits = append(bits, "timezone="+s.Timezone)
	}
	if s.Channel != "" {
		bits = append(bits, "channel="+s.Channel)
	}
	if s.Market != "" {
		bits = append(bits, "market="+s.Market)
	}
	if s.DeploymentID != "" {
		bits = append(bits, "deployment_id="+s.DeploymentID)
	}
	if s.RulesetID != "" {
		bits = append(bits, "ruleset_id="+s.RulesetID)
	}
	if len(bits) == 0 {
		return ""
	}
	return SettingsMetaHeading + "\n\n" + strings.Join(bits, ", ")
}

// BuildInjectBlock concatenates inject sections (not including tool-path).
func BuildInjectBlock(s InjectSettings) (string, []string) {
	var warnings []string
	var parts []string
	company, cw := RenderCompanyContext(s.CompanyContext)
	warnings = append(warnings, cw...)
	if company != "" {
		parts = append(parts, company)
	}
	if meta := RenderSettingsMeta(s); meta != "" {
		parts = append(parts, meta)
	}
	if rules := RenderRulesBlock(s.Rules); rules != "" {
		parts = append(parts, rules)
	}
	if out := RenderOutputRequestBlock(s.OutputRequest); out != "" {
		parts = append(parts, out)
	}
	return strings.Join(parts, "\n\n"), warnings
}

func chatReqSystemTexts(chatReq map[string]any) (msg0, instr string) {
	if chatReq == nil {
		return "", ""
	}
	if messages, ok := chatReq["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			msg0 = asStr(msg["content"])
		}
	}
	if m, ok := chatReq["instructions"].(map[string]any); ok && m != nil {
		instr = asStr(m["system_prompt"])
	}
	return msg0, instr
}

func chatReqHasHeading(chatReq map[string]any, heading string) bool {
	if heading == "" {
		return false
	}
	a, b := chatReqSystemTexts(chatReq)
	return strings.Contains(a, heading) || strings.Contains(b, heading)
}

func chatReqHasBriefMarker(chatReq map[string]any) bool {
	a, b := chatReqSystemTexts(chatReq)
	return hasBriefMarker(a) || hasBriefMarker(b)
}

// spliceChatReqInPlace writes inject text into an already-cloned chat_req.
func spliceChatReqInPlace(chatReq map[string]any, block, headingLine string) map[string]any {
	if strings.TrimSpace(block) == "" || chatReq == nil {
		return chatReq
	}
	splice := func(text string) string {
		return spliceAfterBrief(text, block, headingLine)
	}
	if messages, ok := chatReq["messages"].([]any); ok && len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			msg["content"] = splice(asStr(msg["content"]))
		}
	}
	if instr, ok := chatReq["instructions"].(map[string]any); ok && instr != nil {
		if sp := asStr(instr["system_prompt"]); sp != "" {
			instr["system_prompt"] = splice(sp)
		}
	}
	return chatReq
}

func spliceChatReq(chatReq map[string]any, block, headingLine string) map[string]any {
	if strings.TrimSpace(block) == "" {
		return chatReq
	}
	if !chatReqHasBriefMarker(chatReq) || chatReqHasHeading(chatReq, headingLine) {
		return chatReq
	}
	return spliceChatReqInPlace(cloneDoc(chatReq), block, headingLine)
}

// ApplyControlPlaneInject deep-copies chat_req and splices the inject block
// into system prompt(s). Idempotent on heading. No-op without brief markers.
// Empty block returns the original map.
func ApplyControlPlaneInject(chatReq map[string]any, settings InjectSettings) map[string]any {
	block, _ := BuildInjectBlock(settings)
	if strings.TrimSpace(block) == "" {
		return chatReq
	}
	heading := strings.Split(block, "\n")[0]
	return spliceChatReq(chatReq, block, heading)
}

// ApplyToolPathInject splices TOOL PATH POLICY into Bag B. Does not rewrite
// the user message.
func ApplyToolPathInject(chatReq map[string]any, ignore bool) map[string]any {
	if chatReqHasHeading(chatReq, ToolPathHeading) {
		return chatReq
	}
	return spliceChatReq(chatReq, RenderToolPathPolicy(ignore), ToolPathHeading)
}

func injectBagB(chatReq map[string]any, settings InjectSettings) map[string]any {
	if len(chatReq) == 0 {
		return chatReq
	}
	block, _ := BuildInjectBlock(settings)
	heading := ""
	if strings.TrimSpace(block) != "" {
		heading = strings.Split(block, "\n")[0]
	}
	hasBrief := chatReqHasBriefMarker(chatReq)
	needCP := strings.TrimSpace(block) != "" && hasBrief && !chatReqHasHeading(chatReq, heading)
	needTP := hasBrief && !chatReqHasHeading(chatReq, ToolPathHeading)
	if !needCP && !needTP {
		return chatReq
	}
	out := cloneDoc(chatReq)
	if needCP {
		out = spliceChatReqInPlace(out, block, heading)
	}
	if needTP {
		out = spliceChatReqInPlace(out, RenderToolPathPolicy(settings.IgnoreUserToolPathHints), ToolPathHeading)
	}
	return out
}

// ApplyBagBInject is control-plane + tool-path. User messages stay unchanged.
// Clones the catalog at most once.
func ApplyBagBInject(chatReq map[string]any, settings InjectSettings) map[string]any {
	return injectBagB(chatReq, settings)
}

func cloneDoc(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return m
	}
	return out
}

func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	return cloneDoc(m)
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func asBoolVal(v any, def bool) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return def
}
