// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"encoding/json"
	"fmt"

	"github.com/koten-ai/zeus_client_golang/security"
)

const (
	clientTerminateReturn       = "return"
	clientTerminateReturnResult = "return_result"
	untrustedToolPayloadError   = "untrusted_tool_payload"
)

// MiddlewareContext is the mutable per-turn bag visible to middleware (Python MiddlewareContext).
type MiddlewareContext struct {
	TurnID  string
	UserMsg string
	Round   int
	Notes   []string
	Data    map[string]any
}

func newMiddlewareContext(turnID, userMsg string) *MiddlewareContext {
	return &MiddlewareContext{
		TurnID:  turnID,
		UserMsg: userMsg,
		Data:    map[string]any{},
	}
}

func (c *MiddlewareContext) takeNotes() []string {
	if c == nil || len(c.Notes) == 0 {
		return nil
	}
	out := append([]string{}, c.Notes...)
	c.Notes = c.Notes[:0]
	return out
}

func (c *MiddlewareContext) boolData(key string) bool {
	if c == nil || c.Data == nil {
		return false
	}
	v, ok := c.Data[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (c *MiddlewareContext) floatData(key string) float64 {
	if c == nil || c.Data == nil {
		return 0
	}
	f, _ := asFloat(c.Data[key])
	return f
}

// Middleware is one agent-turn hook (Python Middleware protocol).
type Middleware interface {
	Name() string
	Critical() bool
	OnTurnStart(ctx *MiddlewareContext)
	BeforeLLM(ctx *MiddlewareContext, messages []map[string]any)
	AfterLLM(ctx *MiddlewareContext, response map[string]any)
	BeforeZeus(ctx *MiddlewareContext, name string, args map[string]any) map[string]any
	AfterZeus(ctx *MiddlewareContext, name string, status int, body any)
	OnTurnEnd(ctx *MiddlewareContext, answer string)
}

// NoopMiddleware is the default hook (Python NoopMiddleware).
type NoopMiddleware struct {
	HookName string
}

func (n NoopMiddleware) Name() string {
	if n.HookName == "" {
		return "noop"
	}
	return n.HookName
}
func (NoopMiddleware) Critical() bool { return false }
func (NoopMiddleware) OnTurnStart(*MiddlewareContext) {
}
func (NoopMiddleware) BeforeLLM(*MiddlewareContext, []map[string]any) {
}
func (NoopMiddleware) AfterLLM(*MiddlewareContext, map[string]any) {
}
func (NoopMiddleware) BeforeZeus(_ *MiddlewareContext, _ string, args map[string]any) map[string]any {
	return args
}
func (NoopMiddleware) AfterZeus(*MiddlewareContext, string, int, any) {}
func (NoopMiddleware) OnTurnEnd(*MiddlewareContext, string)           {}

// MiddlewareChain is an ordered list with isolated non-critical failures (Python MiddlewareChain).
type MiddlewareChain struct {
	Items []Middleware
}

func (c *MiddlewareChain) Add(mw Middleware) {
	if c == nil || mw == nil {
		return
	}
	c.Items = append(c.Items, mw)
}

func (c *MiddlewareChain) items() []Middleware {
	if c == nil {
		return nil
	}
	return c.Items
}

func (c *MiddlewareChain) isolate(mwCtx *MiddlewareContext, mw Middleware, method string, fn func()) {
	critical := mw.Critical()
	name := mw.Name()
	defer func() {
		if rec := recover(); rec != nil {
			note := fmt.Sprintf("middleware.%s.%s_failed: %v", name, method, rec)
			if mwCtx != nil {
				mwCtx.Notes = append(mwCtx.Notes, note)
			}
			if critical {
				panic(rec)
			}
		}
	}()
	fn()
}

func (c *MiddlewareChain) OnTurnStart(mwCtx *MiddlewareContext) {
	for _, mw := range c.items() {
		c.isolate(mwCtx, mw, "on_turn_start", func() { mw.OnTurnStart(mwCtx) })
	}
}

func (c *MiddlewareChain) BeforeLLM(mwCtx *MiddlewareContext, messages []map[string]any) {
	for _, mw := range c.items() {
		c.isolate(mwCtx, mw, "before_llm", func() { mw.BeforeLLM(mwCtx, messages) })
	}
}

func (c *MiddlewareChain) AfterLLM(mwCtx *MiddlewareContext, response map[string]any) {
	for _, mw := range c.items() {
		c.isolate(mwCtx, mw, "after_llm", func() { mw.AfterLLM(mwCtx, response) })
	}
}

func (c *MiddlewareChain) BeforeZeus(mwCtx *MiddlewareContext, name string, args map[string]any) map[string]any {
	out := args
	for _, mw := range c.items() {
		next := out
		c.isolate(mwCtx, mw, "before_zeus", func() {
			if got := mw.BeforeZeus(mwCtx, name, next); got != nil {
				next = got
			}
		})
		out = next
	}
	if out == nil {
		return args
	}
	return out
}

func (c *MiddlewareChain) AfterZeus(mwCtx *MiddlewareContext, name string, status int, body any) {
	for _, mw := range c.items() {
		c.isolate(mwCtx, mw, "after_zeus", func() { mw.AfterZeus(mwCtx, name, status, body) })
	}
}

func (c *MiddlewareChain) OnTurnEnd(mwCtx *MiddlewareContext, answer string) {
	for _, mw := range c.items() {
		c.isolate(mwCtx, mw, "on_turn_end", func() { mw.OnTurnEnd(mwCtx, answer) })
	}
}

// DefaultMiddlewareChain is Python MiddlewareChain(items=[SecurityHooks()]).
func DefaultMiddlewareChain() *MiddlewareChain {
	return &MiddlewareChain{Items: []Middleware{SecurityHooks{}}}
}

func foldAssessment(ctx *MiddlewareContext, assessment security.JailbreakAssessment) {
	if ctx == nil {
		return
	}
	if ctx.Data == nil {
		ctx.Data = map[string]any{}
	}
	prev := ctx.floatData("hooks_jailbreak_score")
	score := prev
	if assessment.Score > score {
		score = assessment.Score
	}
	if score > 1 {
		score = 1
	}
	ctx.Data["hooks_jailbreak_score"] = score
	if assessment.MustRefuse || score >= security.HardRefuseScore {
		ctx.Data["hooks_must_refuse"] = true
	}
	hits := asStringSlice(ctx.Data["jailbreak_hits"])
	seen := make(map[string]struct{}, len(hits))
	for _, id := range hits {
		seen[id] = struct{}{}
	}
	for _, h := range assessment.Hits {
		if _, ok := seen[h.AttemptID]; ok {
			continue
		}
		seen[h.AttemptID] = struct{}{}
		hits = append(hits, h.AttemptID)
	}
	ctx.Data["jailbreak_hits"] = hits
}

// InspectJailbreak scores a payload and folds it into ctx.Data (Python inspect_jailbreak).
func InspectJailbreak(ctx *MiddlewareContext, payload any, surface string) security.JailbreakAssessment {
	assessment := security.AssessPayload(payload, surface)
	foldAssessment(ctx, assessment)
	return assessment
}

func denyVerb(ctx *MiddlewareContext, name string) {
	if ctx == nil {
		return
	}
	if ctx.Data == nil {
		ctx.Data = map[string]any{}
	}
	denied := asStringSlice(ctx.Data["denied_verbs"])
	for _, d := range denied {
		if d == name {
			prev := ctx.floatData("hooks_jailbreak_score")
			if prev < security.DeniedVerbScore {
				ctx.Data["hooks_jailbreak_score"] = security.DeniedVerbScore
			}
			return
		}
	}
	ctx.Data["denied_verbs"] = append(denied, name)
	prev := ctx.floatData("hooks_jailbreak_score")
	if prev < security.DeniedVerbScore {
		ctx.Data["hooks_jailbreak_score"] = security.DeniedVerbScore
	}
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return append([]string{}, x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func asNameSet(v any) map[string]bool {
	switch x := v.(type) {
	case map[string]bool:
		return x
	case map[string]struct{}:
		out := make(map[string]bool, len(x))
		for k := range x {
			out[k] = true
		}
		return out
	default:
		return map[string]bool{}
	}
}

func asToolCallMaps(v any) []map[string]any {
	switch x := v.(type) {
	case []map[string]any:
		return x
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func priorTextsFromData(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// SecurityHooks is the baseline AgentHooks: prompt-dump / secrets / denied verbs
// (CHECKLIST C / Python SecurityHooks). Dual scores stay separate from Layer A.
type SecurityHooks struct {
	HookName    string
	IsCritical  bool
	DeniedVerbs []string
}

func (s SecurityHooks) Name() string {
	if s.HookName == "" {
		return "security"
	}
	return s.HookName
}

func (s SecurityHooks) Critical() bool { return s.IsCritical }

func (s SecurityHooks) ScoreJailbreak(ctx *MiddlewareContext) float64 {
	if ctx == nil {
		return 0
	}
	if ctx.Data == nil {
		ctx.Data = map[string]any{}
	}
	prior := priorTextsFromData(ctx.Data["prior_user_texts"])
	assessment := security.AssessTurn(ctx.UserMsg, prior)
	foldAssessment(ctx, assessment)
	if len(asStringSlice(ctx.Data["denied_verbs"])) > 0 {
		prev := ctx.floatData("hooks_jailbreak_score")
		if prev < security.DeniedVerbScore {
			ctx.Data["hooks_jailbreak_score"] = security.DeniedVerbScore
		}
	}
	score := ctx.floatData("hooks_jailbreak_score")
	if score > 1 {
		score = 1
	}
	return score
}

func (s SecurityHooks) MustRefuse(ctx *MiddlewareContext) bool {
	return s.ScoreJailbreak(ctx) >= security.HardRefuseScore
}

func (s SecurityHooks) OnTurnStart(ctx *MiddlewareContext) {
	if ctx == nil {
		return
	}
	if ctx.Data == nil {
		ctx.Data = map[string]any{}
	}
	score := s.ScoreJailbreak(ctx)
	ctx.Data["hooks_jailbreak_score"] = score
	ctx.Data["hooks_must_refuse"] = ctx.boolData("hooks_must_refuse") || score >= security.HardRefuseScore
}

func (SecurityHooks) BeforeLLM(*MiddlewareContext, []map[string]any) {}

func (s SecurityHooks) AfterLLM(ctx *MiddlewareContext, response map[string]any) {
	var content any
	var calls any
	if response != nil {
		content = response["content"]
		calls = response["tool_calls"]
	}
	InspectJailbreak(ctx, content, "llm")
	for _, tc := range asToolCallMaps(calls) {
		fn, _ := tc["function"].(map[string]any)
		var args any
		if fn != nil {
			args = fn["arguments"]
		}
		InspectJailbreak(ctx, args, "tool_args")
	}
}

func (s SecurityHooks) BeforeZeus(ctx *MiddlewareContext, name string, args map[string]any) map[string]any {
	if ctx != nil {
		if s.verbDenied(ctx, name) {
			denyVerb(ctx, name)
		}
		InspectJailbreak(ctx, args, "tool_args")
	}
	return args
}

func (s SecurityHooks) verbDenied(ctx *MiddlewareContext, name string) bool {
	for _, d := range s.DeniedVerbs {
		if d == name {
			return true
		}
	}
	allowed := asNameSet(nil)
	if ctx != nil && ctx.Data != nil {
		allowed = asNameSet(ctx.Data["catalog_tool_names"])
	}
	if len(allowed) == 0 {
		return false
	}
	if allowed[name] {
		return false
	}
	if name == clientTerminateReturn || name == clientTerminateReturnResult {
		return false
	}
	return true
}

func (SecurityHooks) AfterZeus(ctx *MiddlewareContext, _ string, _ int, body any) {
	assessment := InspectJailbreak(ctx, body, "tool_body")
	if !assessment.MustRefuse || ctx == nil {
		return
	}
	if ctx.Data == nil {
		ctx.Data = map[string]any{}
	}
	raw, err := json.Marshal(map[string]string{"error": untrustedToolPayloadError})
	if err != nil {
		ctx.Data["tool_body_override"] = `{"error":"untrusted_tool_payload"}`
		return
	}
	ctx.Data["tool_body_override"] = string(raw)
}

func (SecurityHooks) OnTurnEnd(ctx *MiddlewareContext, answer string) {
	InspectJailbreak(ctx, answer, "answer")
}
