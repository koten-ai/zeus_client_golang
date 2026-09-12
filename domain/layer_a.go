// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Layer A required-four confidence ENUM (API_LAYER_A).
var layerAConfidence = map[string]struct{}{
	"high": {},
	"med":  {},
	"low":  {},
}

// G2 UI-forbidden keys — never in UIView / UserFacingAnswer.
var g2UIForbidden = map[string]struct{}{
	"wish_i_knew":             {},
	"jail_break_attempt":      {},
	"subject_confidence":      {},
	"data_gaps":               {},
	"business_rules_triggers": {},
	"app_output":              {},
	"hooks_jailbreak_score":   {},
}

// Meta keys that appear alongside summary in terminate / pipeline-arg dumps.
var layerADumpMeta = []string{
	"confidence",
	"query_decomposition",
	"decomposition",
	"policy_action",
	"subject_confidence",
	"jail_break_attempt",
	"wish_i_knew",
	"business_rules_triggers",
	"node_refs",
	"entity_refs",
	"data_gaps",
	"app_output",
	"provenance",
}

var (
	fenceWrapRE       = regexp.MustCompile(`(?is)^` + "```" + `(?:json|yaml|yml|text|html|xml|pipeline)?\s*\n([\s\S]*?)\n` + "```" + `\s*$`)
	summaryQuotedRE   = regexp.MustCompile(`(?m)^summary\s*:\s*("(?:\\.|[^"\\])*")\s*$`)
	summaryUnquotedRE = regexp.MustCompile(`(?m)^summary\s*:\s*(.+?)\s*$`)
	summaryYAMLRE     = regexp.MustCompile(`(?m)^summary\s*:`)
	pipelineBlockRE   = regexp.MustCompile(`(?is)<pipeline\b[^>]*>([\s\S]*?)</pipeline>`)
	pipelineOpenRE    = regexp.MustCompile(`(?i)<([a-zA-Z_][\w-]*)\b[^>]*>`)
	yamlDumpMetaREs   []*regexp.Regexp
)

func init() {
	yamlDumpMetaREs = make([]*regexp.Regexp, len(layerADumpMeta))
	for i, k := range layerADumpMeta {
		yamlDumpMetaREs[i] = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(k) + `\s*:`)
	}
}

// LayerA is the parsed terminate / return bag (Python LayerA).
type LayerA struct {
	Summary               string
	QueryDecomposition    map[string]any
	Decomposition         map[string]any
	Confidence            string
	PolicyAction          string
	BusinessRulesTriggers map[string]bool
	AppOutput             map[string]any // nil when absent
	JailBreakAttempt      *float64
	WishIKnew             []any
	DataGaps              []any
	EntityRefs            any
	NodeRefs              any
	Provenance            any
	Raw                   map[string]any
	Errors                []string
	Warnings              []string
}

// OK is required-four + trigger/app_output validation (no errors).
func (l LayerA) OK() bool { return len(l.Errors) == 0 }

// ParseLayerAOpts is parse_layer_a kwargs.
type ParseLayerAOpts struct {
	RuleIDs          []string
	OutputRequest    map[string]any
	AppOutputOnError string // default "strip"
}

// NormalizeTriggers is sparse object {id: bool}; missing ⇒ false.
// Arrays fail closed (no dual-read). ruleIDs is unused (Python oracle).
func NormalizeTriggers(raw any, ruleIDs []string) (map[string]bool, []string) {
	_ = ruleIDs
	var errors []string
	if raw == nil {
		return map[string]bool{}, errors
	}
	if m, ok := asObjectMap(raw); ok {
		out := make(map[string]bool, len(m))
		for k, v := range m {
			out[fmt.Sprint(k)] = pyBool(v)
		}
		return out, errors
	}
	if isArray(raw) {
		errors = append(errors, "business_rules_triggers must be an object {id: bool}; arrays rejected")
		return map[string]bool{}, errors
	}
	errors = append(errors, fmt.Sprintf(
		"business_rules_triggers must be an object {id: bool}; got %s", pyTypeName(raw)))
	return map[string]bool{}, errors
}

// ValidateAppOutput type-checks app_output values against output_request.app.fields.
// onError: "strip" (default) omits bad fields; "fail" returns nil map on first mismatch.
func ValidateAppOutput(appOutput any, fieldsSpec map[string]any, onError string) (map[string]any, []string) {
	var errors []string
	if onError == "" {
		onError = "strip"
	}
	if fieldsSpec == nil {
		if appOutput == nil {
			return nil, errors
		}
		if m, ok := asObjectMap(appOutput); ok && len(m) == 0 {
			return map[string]any{}, errors
		}
		if m, ok := asObjectMap(appOutput); ok {
			return copyMapAny(m), errors
		}
		errors = append(errors, "app_output present but no output_request.app.fields")
		if onError == "strip" {
			return map[string]any{}, errors
		}
		return nil, errors
	}
	if appOutput == nil {
		return nil, errors
	}
	m, ok := asObjectMap(appOutput)
	if !ok {
		errors = append(errors, fmt.Sprintf("app_output must be object, got %s", pyTypeName(appOutput)))
		if onError == "strip" {
			return map[string]any{}, errors
		}
		return nil, errors
	}
	out := map[string]any{}
	for name, specAny := range fieldsSpec {
		spec, ok := asObjectMap(specAny)
		if !ok {
			continue
		}
		val, present := m[name]
		if !present {
			continue
		}
		tname := asString(spec["type"])
		if tname == "" {
			tname = "string"
		}
		if valueMatchesType(val, tname) {
			out[name] = val
			continue
		}
		msg := fmt.Sprintf("app_output.%s type mismatch: expected %s, got %s", name, tname, pyTypeName(val))
		errors = append(errors, msg)
		if onError != "strip" {
			return nil, errors
		}
	}
	return out, errors
}

// ParseLayerA parses and validates a terminate payload (required four + G3).
func ParseLayerA(returnArgs any, opts ParseLayerAOpts) LayerA {
	layer := LayerA{
		BusinessRulesTriggers: map[string]bool{},
		Raw:                   map[string]any{},
	}
	args, ok := asObjectMap(returnArgs)
	if !ok {
		layer.Errors = append(layer.Errors, "return payload is not an object")
		return layer
	}
	layer.Raw = copyMapAny(args)

	summary, _ := args["summary"].(string)
	if strings.TrimSpace(summary) == "" {
		layer.Errors = append(layer.Errors, "required field summary missing or empty")
	} else {
		layer.Summary = strings.TrimSpace(summary)
	}

	qd, ok := asObjectMap(args["query_decomposition"])
	if !ok {
		layer.Errors = append(layer.Errors, "required field query_decomposition must be an object")
	} else {
		layer.QueryDecomposition = qd
	}

	decomp, ok := asObjectMap(args["decomposition"])
	if !ok {
		decomp, ok = asObjectMap(args["query_understanding"])
	}
	if !ok {
		layer.Errors = append(layer.Errors, "required field decomposition must be an object")
	} else {
		layer.Decomposition = decomp
	}

	conf, _ := args["confidence"].(string)
	if _, known := layerAConfidence[conf]; !known {
		layer.Errors = append(layer.Errors, "required field confidence must be one of high|med|low")
	} else {
		layer.Confidence = conf
	}

	if pa, ok := args["policy_action"].(string); ok && strings.TrimSpace(pa) != "" {
		layer.PolicyAction = strings.TrimSpace(pa)
	}

	triggers, terr := NormalizeTriggers(args["business_rules_triggers"], opts.RuleIDs)
	layer.BusinessRulesTriggers = triggers
	layer.Errors = append(layer.Errors, terr...)

	if f, ok := asFloat64(args["jail_break_attempt"]); ok {
		layer.JailBreakAttempt = &f
	}

	if wik, ok := args["wish_i_knew"]; ok {
		if arr, isArr := asArray(wik); isArr {
			layer.WishIKnew = arr
		} else if wik != nil {
			layer.Warnings = append(layer.Warnings, "wish_i_knew ignored (expected array of objects)")
		}
	}

	if dg, ok := args["data_gaps"]; ok {
		if arr, isArr := asArray(dg); isArr {
			layer.DataGaps = arr
		}
	}

	if v, ok := args["entity_refs"]; ok {
		layer.EntityRefs = v
	}
	if v, ok := args["node_refs"]; ok {
		layer.NodeRefs = v
	}
	if v, ok := args["provenance"]; ok {
		layer.Provenance = v
	}

	onError := opts.AppOutputOnError
	if onError == "" {
		onError = "strip"
	}
	var fieldsSpec map[string]any
	if app, ok := asObjectMap(opts.OutputRequest["app"]); ok {
		if fields, ok := asObjectMap(app["fields"]); ok {
			fieldsSpec = fields
		}
	}
	appOut, appErrs := ValidateAppOutput(args["app_output"], fieldsSpec, onError)
	layer.AppOutput = appOut
	if len(appErrs) > 0 {
		if onError == "fail" {
			layer.Errors = append(layer.Errors, appErrs...)
		} else {
			layer.Warnings = append(layer.Warnings, appErrs...)
		}
	}
	return layer
}

// ApplyPackSchema adds errors when terminate misses pack response_output_schema required four.
func ApplyPackSchema(layer LayerA, schema map[string]any) LayerA {
	if schema == nil {
		return layer
	}
	required := []string{"summary", "query_decomposition", "decomposition", "confidence"}
	if raw, ok := schema["required"].([]any); ok {
		required = make([]string, 0, len(raw))
		for _, v := range raw {
			required = append(required, fmt.Sprint(v))
		}
	}
	have := map[string]bool{
		"summary":             layer.Summary != "",
		"query_decomposition": layer.QueryDecomposition != nil,
		"decomposition":       layer.Decomposition != nil,
		"confidence":          layer.Confidence != "",
	}
	for _, key := range required {
		if ok, tracked := have[key]; tracked && !ok {
			msg := fmt.Sprintf("pack schema required field %s missing", key)
			if !containsString(layer.Errors, msg) {
				layer.Errors = append(layer.Errors, msg)
			}
		}
	}
	return layer
}

// ParsePipelineEnvelope parses a fenced/XML <pipeline> dump into pipeline tool args.
// Returns nil unless steps is a non-empty list.
func ParsePipelineEnvelope(text string) map[string]any {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	body := stripCodeFence(text)
	match := pipelineBlockRE.FindStringSubmatch(body)
	if match == nil {
		return nil
	}
	args := parsePipelineChildren(match[1])
	steps, ok := asArray(args["steps"])
	if !ok || len(steps) == 0 {
		return nil
	}
	return args
}

// LooksLikeLayerADump is true when text is a Layer A / pipeline-args envelope, not chat prose.
func LooksLikeLayerADump(text string) bool {
	if text == "" {
		return false
	}
	body := stripCodeFence(text)
	if body == "" {
		return false
	}
	head := body
	if len(head) > 800 {
		head = head[:800]
	}
	if strings.HasPrefix(body, "{") && strings.Contains(head, `"summary"`) {
		hits := 0
		for _, k := range layerADumpMeta {
			if strings.Contains(body, `"`+k+`"`) {
				hits++
			}
		}
		return hits >= 2
	}
	if !summaryYAMLRE.MatchString(body) {
		return false
	}
	hits := 0
	for _, re := range yamlDumpMetaREs {
		if re.MatchString(body) {
			hits++
		}
	}
	return hits >= 2
}

// PeelLayerASummary returns the summary prose when text is a Layer A dump; else ("", false).
func PeelLayerASummary(text string) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	if !LooksLikeLayerADump(text) {
		return "", false
	}
	body := stripCodeFence(text)

	if strings.HasPrefix(body, "{") {
		var obj any
		if json.Unmarshal([]byte(body), &obj) == nil {
			if m, ok := obj.(map[string]any); ok {
				for _, key := range []string{"summary", "answer", "content"} {
					if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s), true
					}
				}
			}
		}
	}

	if m := summaryQuotedRE.FindStringSubmatch(body); m != nil {
		var val any
		if json.Unmarshal([]byte(m[1]), &val) == nil {
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s), true
			}
		} else {
			raw := m[1][1 : len(m[1])-1]
			raw = strings.ReplaceAll(raw, `\n`, "\n")
			raw = strings.ReplaceAll(raw, `\t`, "\t")
			raw = strings.ReplaceAll(raw, `\"`, `"`)
			raw = strings.ReplaceAll(raw, `\\`, `\`)
			if strings.TrimSpace(raw) != "" {
				return strings.TrimSpace(raw), true
			}
		}
	}

	if m := summaryUnquotedRE.FindStringSubmatch(body); m != nil {
		val := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		if val != "" && !strings.HasPrefix(val, "{") {
			return strings.TrimSpace(strings.ReplaceAll(val, `\n`, "\n")), true
		}
	}
	return "", false
}

// UserFacingAnswer normalizes model/tool answer for chat UIs.
// Preference when raw answer is a Layer A envelope: uiText, then layerSummary, then peeled summary.
func UserFacingAnswer(answer, uiText, layerSummary string) string {
	raw := answer
	preferred := ""
	if strings.TrimSpace(uiText) != "" {
		preferred = strings.TrimSpace(uiText)
	} else if strings.TrimSpace(layerSummary) != "" {
		preferred = strings.TrimSpace(layerSummary)
	}

	if LooksLikeLayerADump(raw) {
		if preferred != "" && !LooksLikeLayerADump(preferred) {
			return preferred
		}
		if peeled, ok := PeelLayerASummary(raw); ok {
			return peeled
		}
		if preferred != "" {
			return preferred
		}
	}
	if pipe := ParsePipelineEnvelope(raw); pipe != nil {
		preferredIsProse := preferred != "" &&
			!LooksLikeLayerADump(preferred) &&
			ParsePipelineEnvelope(preferred) == nil
		if preferredIsProse {
			return preferred
		}
		if s, ok := pipe["summary"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		if preferred != "" {
			return preferred
		}
	}
	return raw
}

// UIView is the G1 user-facing view — never G2 admin scores / wish_i_knew.
// uiText nil → layer.summary (Python ui_text is None).
func UIView(layer LayerA, uiText *string) map[string]any {
	text := layer.Summary
	if uiText != nil {
		text = *uiText
	}
	out := map[string]any{
		"summary":       text,
		"confidence":    nilIfEmpty(layer.Confidence),
		"policy_action": nilIfEmpty(layer.PolicyAction),
	}
	for k := range g2UIForbidden {
		delete(out, k)
	}
	return out
}

// ArtifactsView is full Layer A + G2/G3 for artifacts / metrics — not chat UI.
// Dual jailbreak scores stay separate fields.
func ArtifactsView(layer LayerA, hooksJailbreakScore any, policy string, flags map[string]bool) map[string]any {
	flagCopy := map[string]bool{}
	for k, v := range flags {
		flagCopy[k] = v
	}
	errs := layer.Errors
	if errs == nil {
		errs = []string{}
	}
	warns := layer.Warnings
	if warns == nil {
		warns = []string{}
	}
	var jba any
	if layer.JailBreakAttempt != nil {
		jba = *layer.JailBreakAttempt
	}
	var pol any
	if policy != "" {
		pol = policy
	}
	return map[string]any{
		"summary":                 layer.Summary,
		"query_decomposition":     layer.QueryDecomposition,
		"decomposition":           layer.Decomposition,
		"confidence":              nilIfEmpty(layer.Confidence),
		"policy_action":           nilIfEmpty(layer.PolicyAction),
		"business_rules_triggers": copyBoolMap(layer.BusinessRulesTriggers),
		"app_output":              layer.AppOutput,
		"jail_break_attempt":      jba,
		"hooks_jailbreak_score":   hooksJailbreakScore,
		"wish_i_knew":             layer.WishIKnew,
		"data_gaps":               layer.DataGaps,
		"entity_refs":             layer.EntityRefs,
		"node_refs":               layer.NodeRefs,
		"provenance":              layer.Provenance,
		"policy":                  pol,
		"flags":                   flagCopy,
		"errors":                  append([]string{}, errs...),
		"warnings":                append([]string{}, warns...),
		"raw":                     copyMapAny(layer.Raw),
	}
}

// CompactLayerA is a G2-free terminate bag for public_trace / session-trace.
func CompactLayerA(layer LayerA, via string) map[string]any {
	if via == "" {
		via = "client_terminate"
	}
	decomp := layer.Decomposition
	if decomp == nil {
		decomp = map[string]any{}
	}
	targets := decomp["targets"]
	emptyTargets := !pyBool(targets)
	synthetic := (layer.PolicyAction == "error" && emptyTargets) || LooksLikeLayerADump(layer.Summary)
	errs := layer.Errors
	if errs == nil {
		errs = []string{}
	}
	warns := layer.Warnings
	if warns == nil {
		warns = []string{}
	}
	return map[string]any{
		"ok":                  layer.OK(),
		"summary":             layer.Summary,
		"confidence":          nilIfEmpty(layer.Confidence),
		"policy_action":       nilIfEmpty(layer.PolicyAction),
		"query_decomposition": layer.QueryDecomposition,
		"decomposition":       layer.Decomposition,
		"synthetic":           synthetic,
		"via":                 via,
		"errors":              append([]string{}, errs...),
		"warnings":            append([]string{}, warns...),
	}
}

func stripCodeFence(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return ""
	}
	if m := fenceWrapRE.FindStringSubmatch(t); m != nil {
		return strings.TrimSpace(m[1])
	}
	if strings.HasPrefix(t, "```") {
		lines := strings.Split(t, "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == "```" {
			lines = lines[:n-1]
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return t
}

func parsePipelineChildren(inner string) map[string]any {
	args := map[string]any{}
	s := inner
	for {
		loc := pipelineOpenRE.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		tag := s[loc[2]:loc[3]]
		after := s[loc[1]:]
		if strings.EqualFold(tag, "pipeline") {
			s = after
			continue
		}
		closeTag := "</" + tag + ">"
		idx := indexFold(after, closeTag)
		if idx < 0 {
			s = after
			continue
		}
		if tag != "" {
			args[tag] = maybeJSONValue(after[:idx])
		}
		s = after[idx+len(closeTag):]
	}
	return args
}

func maybeJSONValue(raw string) any {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	if s[0] == '{' || s[0] == '[' || s == "true" || s == "false" || s == "null" {
		var v any
		if json.Unmarshal([]byte(s), &v) == nil {
			return v
		}
	}
	return s
}

func indexFold(s, substr string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(substr))
}

func valueMatchesType(val any, typeName string) bool {
	tn := strings.TrimSpace(strings.ToLower(typeName))
	if tn == "" {
		tn = "string"
	}
	switch tn {
	case "string", "str":
		_, ok := val.(string)
		return ok
	case "number", "float":
		return isNumberNotBool(val)
	case "integer", "int":
		return isIntNotBool(val)
	case "boolean", "bool":
		_, ok := val.(bool)
		return ok
	case "object", "dict":
		_, ok := asObjectMap(val)
		return ok
	case "array", "list":
		return isArray(val)
	case "null":
		return val == nil
	default:
		return true
	}
}

func asObjectMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil, false
	}
	return m, true
}

func asArray(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	default:
		return nil, false
	}
}

func isArray(v any) bool {
	_, ok := asArray(v)
	return ok
}

func asFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func isNumberNotBool(v any) bool {
	switch v.(type) {
	case bool:
		return false
	default:
		_, ok := asFloat64(v)
		return ok
	}
}

func isIntNotBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return false
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case json.Number:
		_, err := x.Int64()
		return err == nil
	default:
		return false
	}
}

func pyBool(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case map[string]bool:
		return len(x) > 0
	default:
		if f, ok := asFloat64(v); ok {
			return f != 0
		}
		return v != nil
	}
}

func pyTypeName(v any) string {
	if v == nil {
		return "NoneType"
	}
	switch v.(type) {
	case string:
		return "str"
	case bool:
		return "bool"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "int"
	case float32, float64:
		return "float"
	case map[string]any:
		return "dict"
	case []any:
		return "list"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func copyMapAny(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyBoolMap(m map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func incompleteObject(m map[string]any) bool {
	return len(m) == 0
}
