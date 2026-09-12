// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"fmt"
)

// MiddlewareContext is the mutable per-turn bag visible to middleware (Python MiddlewareContext).
// Jailbreak scoring lands in ZCG-25 (SecurityHooks). This ticket wires the chain only.
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
