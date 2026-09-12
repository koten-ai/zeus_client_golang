// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"

	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const agentAPIComponent = "api.agent"

// AgentOptions constructs AgentAPI (Python AgentAPI.__init__).
type AgentOptions struct {
	LLM     ports.LlmPort
	Zeus    ports.ZeusPort
	Journal journal.ExecutionJournal
	IDs     ports.IDFactory
	Config  config.RuntimeConfig
	Version string
	Clock   ports.Clock
	Catalog ports.CatalogStore
	Log     func(level, msg string, attrs map[string]any)
}

// AgentAPI is the Mode 1 agent-plane facade (Python AgentAPI).
// The handle holds the Client without importing the root package.
type AgentAPI struct {
	host       any
	opts       AgentOptions
	middleware *application.MiddlewareChain
}

// NewAgentAPI binds a Client (or test fake) as the facade host.
func NewAgentAPI(host any) *AgentAPI {
	if host == nil {
		return nil
	}
	return &AgentAPI{host: host, middleware: application.DefaultMiddlewareChain()}
}

// NewAgentAPIWith binds host plus ports/config (Client.Agent).
func NewAgentAPIWith(host any, opts AgentOptions) *AgentAPI {
	return &AgentAPI{host: host, opts: opts, middleware: application.DefaultMiddlewareChain()}
}

// Host is the bound Client. Nil-safe.
func (a *AgentAPI) Host() any {
	if a == nil {
		return nil
	}
	return a.host
}

// Middleware is the turn hook chain (default includes SecurityHooks).
func (a *AgentAPI) Middleware() *application.MiddlewareChain {
	if a == nil {
		return nil
	}
	return a.middleware
}

// RunTurnParams is AgentAPI.RunTurn kwargs (Python AgentAPI.run_turn).
type RunTurnParams struct {
	Target         *config.DataTarget
	Settings       *config.ClientSettings
	Session        *domain.SessionHandle
	PriorMessages  []map[string]any
	SystemPrompt   string
	Tools          []map[string]any
	ChatRequest    map[string]any
	ChatID         string
	Model          string
	EnableSessions *bool
	BaseID         string
	LLM            ports.LlmPort
	Zeus           ports.ZeusPort
	Rewind         *bool
}

// RunTurn is agent.run — one sequential Mode 1 turn.
func (a *AgentAPI) RunTurn(ctx context.Context, message string, params RunTurnParams) (application.TurnResult, error) {
	if a == nil {
		return application.TurnResult{}, domain.New(domain.CodeNotImplemented, agentAPIComponent,
			domain.WithMessage("Agent API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return application.TurnResult{}, de
		}
		return application.TurnResult{}, err
	}
	llm := params.LLM
	if llm == nil {
		llm = a.opts.LLM
	}
	if llm == nil {
		return application.TurnResult{}, domain.New(domain.CodeNotImplemented, agentAPIComponent,
			domain.WithMessage("LLM port not wired on runtime"))
	}
	zeus := params.Zeus
	if zeus == nil {
		zeus = a.opts.Zeus
	}
	cs := a.opts.Config.Settings
	if params.Settings != nil {
		cs = *params.Settings
	}
	tgt := a.opts.Config.Target
	if params.Target != nil {
		tgt = *params.Target
	}
	cr := params.ChatRequest
	var extra []string
	var pack map[string]any
	if cr == nil && a.opts.Catalog != nil {
		loaded, err := a.opts.Catalog.Load(ctx, ports.CatalogKey{
			Mode:   cs.Mode,
			Bucket: tgt.Bucket,
			Scope:  tgt.Scope,
			BaseID: params.BaseID,
		})
		if err == nil {
			cr = loaded.Body
			extra = append(extra, "chat_request: catalog")
		}
	}
	sessionsOn := cs.DurableSessions
	if params.EnableSessions != nil {
		sessionsOn = *params.EnableSessions
	}
	sys := params.SystemPrompt
	if sys == "" {
		sys = "You are a helpful Zeus data assistant."
	}
	model := params.Model
	if model == "" {
		model = a.opts.Config.LLM.Model
	}
	dbg := a.opts.Config.Debug
	if params.Rewind != nil {
		dbg.Rewind = *params.Rewind
	}
	req := application.TurnRequest{
		Message:        message,
		Target:         tgt,
		Settings:       cs,
		Session:        params.Session,
		PriorMessages:  params.PriorMessages,
		SystemPrompt:   sys,
		Tools:          params.Tools,
		ChatRequest:    cr,
		BaseID:         params.BaseID,
		PackSchema:     pack,
		ChatID:         params.ChatID,
		Model:          model,
		EnableSessions: sessionsOn,
	}
	result := application.RunAgentTurn(ctx, req, application.RunAgentTurnOpts{
		LLM:              llm,
		Zeus:             zeus,
		Journal:          a.opts.Journal,
		Middleware:       a.middleware,
		DefaultSettings:  a.opts.Config.Settings,
		DebugPolicy:      dbg,
		ZeusURL:          a.opts.Config.Zeus.URL,
		ClientFloor:      a.opts.Config.ClientFloor,
		ExtraNotes:       extra,
		IDs:              a.opts.IDs,
		ClientIP:         a.opts.Config.Client.IPAddress,
		Version:          a.opts.Version,
		Log:              a.opts.Log,
		ContextWindow:    a.opts.Config.LLM.ContextWindowTokens,
		ContextSoftLimit: a.opts.Config.LLM.ContextSoftLimit,
	})
	return result, nil
}
