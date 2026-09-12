// SPDX-License-Identifier: BUSL-1.1

package zeusclient

import (
	"strings"

	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/observability"
	"github.com/koten-ai/zeus_client_golang/ports"
	"github.com/koten-ai/zeus_client_golang/security"
)

// Options constructs a Client. Env nil uses the process environment; an empty
// map isolates tests from process env (same as config.Load). Config, when
// non-nil, is snapshotted (Load is skipped). StampUser overlays config.user
// (empty → zeus_client; Hub Debug Chat sets admin).
//
// Journal / Secrets / Clock / IDs / Redactor / HTTP default when nil.
// Zeus, LLM, Catalog stay nil unless injected. Jobs stay nil unless injected
// or config.jobs.host_url is set (jobshttp). ZeusAPI lazy-opens
// adapters/zeushttp.Port from Config when Services.Zeus is nil.
type Options struct {
	Profile    string
	ConfigPath string
	Env        map[string]string
	Config     *config.RuntimeConfig

	Journal  journal.ExecutionJournal
	Secrets  ports.SecretStore
	Clock    ports.Clock
	IDs      ports.IDFactory
	Redactor security.Redactor
	HTTP     ports.HttpPort
	Zeus     ports.ZeusPort
	LLM      ports.LlmPort
	Catalog  ports.CatalogStore
	Jobs     ports.Jobs
	Logger   *observability.FamilyLogger
	Metrics  observability.MetricsPort

	// StampUser is the session/trace stamp user (UNIFICATION U5).
	// Empty → zeus_client (product). Hub Debug Chat sets "admin".
	// Same agent path; no second loop. Unknown values fail-closed to
	// zeus_client (never invent).
	StampUser string
}

// Client is the public handle (Python ZeusRuntime).
type Client struct {
	rt *runtime
}

// New constructs a Client. Close the result. No process-global HTTP or auth.
// Two New calls return two runtimes (two journals).
func New(opts Options) (*Client, error) {
	cfg, err := resolveConfig(opts)
	if err != nil {
		return nil, err
	}
	return &Client{rt: newRuntime(cfg, opts)}, nil
}

func resolveConfig(opts Options) (config.RuntimeConfig, error) {
	var cfg config.RuntimeConfig
	if opts.Config != nil {
		cfg = opts.Config.Snapshot()
	} else {
		loaded, err := config.Load(opts.ConfigPath, opts.Profile, opts.Env)
		if err != nil {
			return config.RuntimeConfig{}, err
		}
		cfg = loaded.Snapshot()
	}
	applyStampUser(&cfg, opts.StampUser)
	return cfg, nil
}

func applyStampUser(cfg *config.RuntimeConfig, optsUser string) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(optsUser) != "" {
		cfg.User = domain.ResolveStampUser(optsUser)
		return
	}
	cfg.User = domain.ResolveStampUser(cfg.User)
}

// Close releases injected adapters (HttpPort.Close and any ports.Closer).
// Idempotent and nil-safe.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	return c.rt.close()
}

// Config returns a snapshot (maps copied). Mutating the result does not race
// the runtime.
func (c *Client) Config() config.Snapshot {
	if c == nil || c.rt == nil {
		return config.Snapshot{}
	}
	return c.rt.configSnapshot()
}

// Zeus is the Mode 2 data-plane facade (search / find / get / project / call).
func (c *Client) Zeus() *api.ZeusAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.zeusAPI != nil {
		return r.zeusAPI
	}
	r.zeusAPI = api.NewZeusAPIWith(c, api.ZeusOptions{
		Zeus:        r.zeus,
		HTTP:        r.http,
		Secrets:     r.secrets,
		Journal:     r.journal,
		Config:      r.cfg,
		Version:     Version,
		Redactor:    r.redactor,
		Log:         logFunc(r.logger),
		Metrics:     r.metrics,
		RateLimiter: r.rateLimiter,
	})
	return r.zeusAPI
}

// Catalog is the catalog facade (load / info / contract.hash / mini_schema).
// Runtime services.Catalog stays nil unless injected; the API may lazy-open
// FsCatalogStore from Config.ChatRequestsDir.
func (c *Client) Catalog() *api.CatalogAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.catalogAPI != nil {
		return r.catalogAPI
	}
	r.catalogAPI = api.NewCatalogAPI(api.CatalogOptions{
		Store:  r.catalog,
		Config: r.cfg,
	})
	return r.catalogAPI
}

// Units is the Mode 3 WorkUnit facade (ZCG-31). Isolated AgentTurn / ZeusDirect.
// Everyday Q&A stays Mode 1 — this is never auto-promoted from chat.
func (c *Client) Units() *api.UnitsAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unitsAPI != nil {
		return r.unitsAPI
	}
	r.unitsAPI = api.NewUnitsAPIWith(c, api.UnitsOptions{
		LLM:     r.llm,
		Zeus:    r.zeus,
		Journal: r.journal,
		IDs:     r.ids,
		Config:  r.cfg,
		Version: Version,
		Clock:   r.clock,
		Catalog: r.catalog,
		Log:     logFunc(r.logger),
		Metrics: r.metrics,
	})
	return r.unitsAPI
}

// Jobs is the Mode 3 jobs facade (ZCG-39). Fail-closed when Options.Jobs is
// nil (130001) unless config.jobs.host_url is set (Pattern B jobshttp).
// Inject adapters/jobsfake (L4 seed), adapters/jobsma (Pattern A), or
// adapters/jobshttp (SSE WatchJob). BindJobs after New so Pattern A can do
// jobsma.New(api.UnitHost{Units: client.Units()}).
// Everyday Q&A stays Mode 1 — jobs are never auto-promoted from chat.
func (c *Client) Jobs() *api.JobsAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.jobsAPI != nil {
		return r.jobsAPI
	}
	r.jobsAPI = api.NewJobsAPIWith(c, api.JobsOptions{
		Jobs:    r.jobs,
		Journal: r.journal,
		Clock:   r.clock,
	})
	return r.jobsAPI
}

// BindJobs sets the jobs port after New. Pattern A demos construct
// jobsma.New(api.UnitHost{Units: c.Units()}) then BindJobs so Jobs().Run
// is available. Nil-safe. Replaces any previous Jobs port; Close closes it.
// Closed / nil Client → 130001.
func (c *Client) BindJobs(j ports.Jobs) error {
	if c == nil || c.rt == nil {
		return domain.NewJob(domain.CodeJobsUnavailable, "zeusclient")
	}
	c.rt.mu.Lock()
	defer c.rt.mu.Unlock()
	if c.rt.closed {
		return domain.NewJob(domain.CodeJobsUnavailable, "zeusclient")
	}
	c.rt.jobs = j
	c.rt.jobsAPI = nil
	return nil
}

// Agent is the Mode 1 agent-plane facade (ZCG-24 run_turn).
func (c *Client) Agent() *api.AgentAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agentAPI != nil {
		return r.agentAPI
	}
	r.agentAPI = api.NewAgentAPIWith(c, api.AgentOptions{
		LLM:     r.llm,
		Zeus:    r.zeus,
		Journal: r.journal,
		IDs:     r.ids,
		Config:  r.cfg,
		Version: Version,
		Clock:   r.clock,
		Catalog: r.catalog,
		Log:     logFunc(r.logger),
		Metrics: r.metrics,
	})
	return r.agentAPI
}

func logFunc(lg *observability.FamilyLogger) func(level, msg string, attrs map[string]any) {
	if lg == nil {
		return nil
	}
	return lg.Func()
}

// Debug is the journal export / span facade (ZCG-27). Integrators read
// TurnResult.Debug after Agent.RunTurn; ExportJournal(turn_id=debug.ExportRef)
// is gather field 9.
func (c *Client) Debug() *api.DebugAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.debugAPI != nil {
		return r.debugAPI
	}
	r.debugAPI = api.NewDebugAPIWith(c, api.DebugOptions{
		Journal:  r.journal,
		Redactor: r.redactor,
	})
	return r.debugAPI
}

// Session is the durable-session facade (create / continue / rehydrate / trace).
// Trace joins Detective on the dispatch hop req_id (ZCG-16). Runtime has no
// dedicated session port — the API lazy-opens adapters/zeushttp.SessionClient
// from Config + HTTP.
func (c *Client) Session() *api.SessionAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	r := c.rt
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionAPI != nil {
		return r.sessionAPI
	}
	r.sessionAPI = api.NewSessionAPIWith(c, api.SessionOptions{
		HTTP:     r.http,
		Secrets:  r.secrets,
		Config:   r.cfg,
		Version:  Version,
		Identity: r.cfg.Client,
		Journal:  r.journal,
	})
	return r.sessionAPI
}

// Services is the injected port bundle (Python ZeusRuntime.services).
func (c *Client) Services() Services {
	if c == nil || c.rt == nil {
		return Services{}
	}
	return c.rt.services()
}

// Journal is the runtime journal (Python ZeusRuntime.journal).
func (c *Client) Journal() journal.ExecutionJournal {
	if c == nil || c.rt == nil {
		return nil
	}
	return c.rt.journal
}
