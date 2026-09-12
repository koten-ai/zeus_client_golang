// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"sync"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/application"
	"github.com/koten-ai/zeus_client_golang/application/projectors"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const sessionAPIComponent = "api.session"

// SessionAPI is the Mode 1 session-plane facade (create / continue / rehydrate / trace).
// Trace joins Detective on the dispatch hop req_id (ZCG-16). Semantic cache is later.
// The handle holds the Client without importing the root package.
type SessionAPI struct {
	host any
	opts SessionOptions

	mu   sync.Mutex
	lazy *zeushttp.SessionClient
}

// SessionOptions constructs SessionAPI. Client nil → lazy SessionClient from Config + HTTP.
type SessionOptions struct {
	Client   *zeushttp.SessionClient
	HTTP     ports.HttpPort
	Secrets  ports.SecretStore
	Config   config.RuntimeConfig
	Version  string
	Identity config.ClientIdentity
	Journal  journal.ExecutionJournal
	Log      func(level, msg string, attrs map[string]any)
}

// NewSessionAPI binds a Client (or test fake) as the facade host.
func NewSessionAPI(host any) *SessionAPI {
	if host == nil {
		return nil
	}
	return &SessionAPI{host: host}
}

// NewSessionAPIWith binds host plus ports/config (Client.Session).
func NewSessionAPIWith(host any, opts SessionOptions) *SessionAPI {
	return &SessionAPI{host: host, opts: opts}
}

// Host is the bound Client. Nil-safe.
func (s *SessionAPI) Host() any {
	if s == nil {
		return nil
	}
	return s.host
}

// Create is session.create — new durable session. Server mints session.id.
func (s *SessionAPI) Create(ctx context.Context, opts application.SetupOptions) (domain.SessionHandle, error) {
	opts.Prior = nil
	opts.EnableSessions = s.opts.Config.Settings.DurableSessions
	return s.Setup(ctx, opts)
}

// Rehydrate is session.rehydrate — resume with prior id. Dead sid recreates.
func (s *SessionAPI) Rehydrate(ctx context.Context, prior domain.SessionHandle, opts application.SetupOptions) (domain.SessionHandle, error) {
	opts.Prior = &prior
	opts.EnableSessions = s.opts.Config.Settings.DurableSessions
	return s.Setup(ctx, opts)
}

// Continue is session.continue — POST /v2/session/{id}/turn.
func (s *SessionAPI) Continue(ctx context.Context, handle domain.SessionHandle, opts application.CommitOptions) (application.CommitResult, error) {
	return s.Commit(ctx, handle, opts)
}

// Setup is the unified create/rehydrate path (Python SessionLifecycle.setup).
func (s *SessionAPI) Setup(ctx context.Context, opts application.SetupOptions) (domain.SessionHandle, error) {
	if s == nil {
		return domain.SessionHandle{}, domain.NewSession(domain.CodeSessionCreateFailed, sessionAPIComponent,
			domain.WithMessage("session API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	life, err := s.lifecycle()
	if err != nil {
		return domain.SessionHandle{}, err
	}
	if opts.Mode == "" {
		opts.Mode = s.opts.Config.Settings.Mode
	}
	return life.Setup(ctx, opts)
}

// Commit is session.continue at the lifecycle layer.
func (s *SessionAPI) Commit(ctx context.Context, handle domain.SessionHandle, opts application.CommitOptions) (application.CommitResult, error) {
	if s == nil {
		return application.CommitResult{Handle: handle}, domain.NewSession(domain.CodeSessionCommitFailed, sessionAPIComponent,
			domain.WithMessage("session API is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	life, err := s.lifecycle()
	if err != nil {
		return application.CommitResult{Handle: handle}, err
	}
	if opts.Mode == "" {
		opts.Mode = s.opts.Config.Settings.Mode
	}
	return life.Commit(ctx, handle, opts)
}

// Trace POSTs /v2/session/trace via the session-trace projector (ZCG-16).
// Soft-fail: never returns a raising error that would abort a successful turn.
func (s *SessionAPI) Trace(ctx context.Context, opts projectors.ProjectOptions) projectors.SessionTraceProjectResult {
	if s == nil {
		return projectors.SessionTraceProjectResult{OK: false, Errors: []string{"session API is nil"}}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Journal == nil {
		opts.Journal = s.opts.Journal
	}
	if opts.Log == nil {
		opts.Log = s.opts.Log
	}
	if opts.Mode == "" {
		opts.Mode = s.opts.Config.Settings.Mode
	}
	if opts.Target == (config.DataTarget{}) {
		opts.Target = s.opts.Config.Target
	}
	cli, err := s.client()
	if err != nil {
		return projectors.ProjectSessionTrace(ctx, nil, opts)
	}
	return projectors.ProjectSessionTrace(ctx, cli, opts)
}

func (s *SessionAPI) lifecycle() (*application.SessionLifecycle, error) {
	cli, err := s.client()
	if err != nil {
		return nil, err
	}
	return &application.SessionLifecycle{
		Client: cli,
		Target: s.opts.Config.Target,
		Log:    s.opts.Log,
	}, nil
}

func (s *SessionAPI) client() (*zeushttp.SessionClient, error) {
	if s.opts.Client != nil {
		return s.opts.Client, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lazy != nil {
		return s.lazy, nil
	}
	p := zeushttp.NewSessionClient(zeushttp.SessionClientOptions{
		Endpoint:  s.opts.Config.Zeus,
		Secrets:   s.opts.Secrets,
		HTTP:      s.opts.HTTP,
		Version:   s.opts.Version,
		Identity:  s.opts.Identity,
		StampUser: s.opts.Config.User,
	})
	s.lazy = p
	return p, nil
}
