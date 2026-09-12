// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"sync"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
	"github.com/koten-ai/zeus_client_golang/security"
)

const zeusAPIComponent = "api.zeus"

// ZeusAPI is the Mode 2 data-plane facade (Python DataAPI).
// Call / Search / Find / Get / Project are raw V2 verbs. SearchSuggest is
// typeahead wrapping search (Direct only — never agent.Run).
// The handle holds the Client without importing the root package.
type ZeusAPI struct {
	host any
	opts ZeusOptions

	mu   sync.Mutex
	lazy ports.ZeusPort
}

// ZeusOptions constructs ZeusAPI. Zeus nil → lazy zeushttp.Port from Config + HTTP.
// Runtime services.Zeus stays nil unless injected.
type ZeusOptions struct {
	Zeus     ports.ZeusPort
	HTTP     ports.HttpPort
	Secrets  ports.SecretStore
	Journal  journal.ExecutionJournal
	Config   config.RuntimeConfig
	Version  string
	Redactor security.Redactor
	Log      func(level, msg string, attrs map[string]any)
}

// CallOptions is per-call Direct kwargs (Python DataAPI.verb).
// Rewind / ForceTrace nil → RuntimeConfig. AllowPipeline is never set.
type CallOptions struct {
	Target        *config.DataTarget
	ModeHeader    string
	Headers       map[string]string
	ForceTrace    *bool
	Rewind        *bool
	ChatID        string
	TurnID        string
	ChatSessionID string
	BriefSha12    string
	MiniSha12     string
	BaseURL       string
	AuthMode      string
	PasswordEnv   string
	TokenEnv      string
	Username      string
}

// SuggestCallOptions is SearchSuggest kwargs (Python DataAPI.search).
type SuggestCallOptions struct {
	Target  *config.DataTarget
	Options *application.SuggestOptions
	Rewind  *bool
}

// NewZeusAPI binds a Client (or test fake) as the facade host.
func NewZeusAPI(host any) *ZeusAPI {
	if host == nil {
		return nil
	}
	return &ZeusAPI{host: host}
}

// NewZeusAPIWith binds host plus ports/config (Client.Zeus).
func NewZeusAPIWith(host any, opts ZeusOptions) *ZeusAPI {
	return &ZeusAPI{host: host, opts: opts}
}

// Host is the bound Client. Nil-safe.
func (z *ZeusAPI) Host() any {
	if z == nil {
		return nil
	}
	return z.host
}

// Call is zeus.call — generic allow-listed POST. pipeline → 060010.
func (z *ZeusAPI) Call(ctx context.Context, verb string, body map[string]any, opts CallOptions) (application.VerbResult, error) {
	return z.run(ctx, verb, body, opts)
}

// Search is the raw V2 search verb (G3.1). Typeahead is SearchSuggest.
func (z *ZeusAPI) Search(ctx context.Context, body map[string]any, opts CallOptions) (application.VerbResult, error) {
	return z.run(ctx, "search", body, opts)
}

// Find is zeus.find.
func (z *ZeusAPI) Find(ctx context.Context, body map[string]any, opts CallOptions) (application.VerbResult, error) {
	return z.run(ctx, "find", body, opts)
}

// Get is zeus.get.
func (z *ZeusAPI) Get(ctx context.Context, body map[string]any, opts CallOptions) (application.VerbResult, error) {
	return z.run(ctx, "get", body, opts)
}

// Project is zeus.project.
func (z *ZeusAPI) Project(ctx context.Context, body map[string]any, opts CallOptions) (application.VerbResult, error) {
	return z.run(ctx, "project", body, opts)
}

// SearchSuggest is product typeahead wrapping FTS search. Direct only.
func (z *ZeusAPI) SearchSuggest(ctx context.Context, query string, opts SuggestCallOptions) (application.SuggestResult, error) {
	if z == nil {
		return application.SuggestResult{}, domain.New(domain.CodeNotImplemented, zeusAPIComponent,
			domain.WithMessage("Zeus API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	port, err := z.port()
	if err != nil {
		return application.SuggestResult{}, err
	}
	target := z.opts.Config.Target
	if opts.Target != nil {
		target = *opts.Target
	}
	var so application.SuggestOptions
	if opts.Options != nil {
		so = *opts.Options
	}
	rewind := z.opts.Config.Debug.Rewind
	if opts.Rewind != nil {
		rewind = *opts.Rewind
	}
	return application.RunTypeaheadSearch(ctx, port, query, target, so, rewind)
}

func (z *ZeusAPI) run(ctx context.Context, verb string, body map[string]any, opts CallOptions) (application.VerbResult, error) {
	if z == nil {
		return application.VerbResult{}, domain.New(domain.CodeNotImplemented, zeusAPIComponent,
			domain.WithMessage("Zeus API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	port, err := z.port()
	if err != nil {
		return application.VerbResult{}, err
	}
	target := z.opts.Config.Target
	if opts.Target != nil {
		target = *opts.Target
	}
	mode := opts.ModeHeader
	if mode == "" {
		mode = z.opts.Config.Settings.Mode
	}
	force := z.opts.Config.Settings.ForceTrace
	if opts.ForceTrace != nil {
		force = *opts.ForceTrace
	}
	rewind := z.opts.Config.Debug.Rewind
	if opts.Rewind != nil {
		rewind = *opts.Rewind
	}
	result, err := application.RunDataVerb(ctx, port, verb, body, application.DataVerbOptions{
		Target:        target,
		ModeHeader:    mode,
		Headers:       opts.Headers,
		ForceTrace:    force,
		Rewind:        rewind,
		ChatID:        opts.ChatID,
		TurnID:        opts.TurnID,
		ChatSessionID: opts.ChatSessionID,
		BriefSha12:    opts.BriefSha12,
		MiniSha12:     opts.MiniSha12,
		BaseURL:       opts.BaseURL,
		AuthMode:      opts.AuthMode,
		PasswordEnv:   opts.PasswordEnv,
		TokenEnv:      opts.TokenEnv,
		Username:      opts.Username,
	})
	if err != nil {
		return result, err
	}
	if result.OK {
		return result, nil
	}
	return result, hopError(result)
}

func (z *ZeusAPI) port() (ports.ZeusPort, error) {
	if z.opts.Zeus != nil {
		return z.opts.Zeus, nil
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if z.lazy != nil {
		return z.lazy, nil
	}
	p := zeushttp.NewPort(zeushttp.PortOptions{
		Endpoint:  z.opts.Config.Zeus,
		Secrets:   z.opts.Secrets,
		HTTP:      z.opts.HTTP,
		Journal:   z.opts.Journal,
		Version:   z.opts.Version,
		StampUser: z.opts.Config.User,
		Redactor:  z.opts.Redactor,
		Log:       z.opts.Log,
	})
	z.lazy = p
	return p, nil
}

func hopError(result application.VerbResult) error {
	code := zeushttp.HopCode(result.StatusCode)
	details := map[string]any{"http.status_code": result.StatusCode}
	if result.ReqID != "" {
		details["req_id"] = result.ReqID
	}
	msg := result.Error
	opts := []domain.Option{domain.WithDetails(details)}
	if msg != "" {
		opts = append(opts, domain.WithMessage(msg))
	}
	return domain.NewZeusTransport(code, zeusAPIComponent, opts...)
}
