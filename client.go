// SPDX-License-Identifier: BUSL-1.1

package zeusclient

import (
	"github.com/koten-ai/zeus_client_golang/api"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
	"github.com/koten-ai/zeus_client_golang/security"
)

// Options constructs a Client. Env nil uses the process environment; an empty
// map isolates tests from process env (same as config.Load). Config, when
// non-nil, is snapshotted and used as-is (Load is skipped).
//
// Journal / Secrets / Clock / IDs / Redactor / HTTP default when nil.
// Zeus, LLM, Catalog, and Jobs stay nil unless injected. ZeusAPI lazy-opens
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
	if opts.Config != nil {
		return opts.Config.Snapshot(), nil
	}
	cfg, err := config.Load(opts.ConfigPath, opts.Profile, opts.Env)
	if err != nil {
		return config.RuntimeConfig{}, err
	}
	return cfg.Snapshot(), nil
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
	svc := c.Services()
	return api.NewZeusAPIWith(c, api.ZeusOptions{
		Zeus:     svc.Zeus,
		HTTP:     svc.HTTP,
		Secrets:  svc.Secrets,
		Journal:  c.Journal(),
		Config:   c.Config(),
		Version:  Version,
		Redactor: svc.Redactor,
	})
}

// Catalog is the catalog facade (load / info / contract.hash / mini_schema).
// Runtime services.Catalog stays nil unless injected; the API may lazy-open
// FsCatalogStore from Config.ChatRequestsDir.
func (c *Client) Catalog() *api.CatalogAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	return api.NewCatalogAPI(api.CatalogOptions{
		Store:  c.Services().Catalog,
		Config: c.Config(),
	})
}

// Agent is the Mode 1 agent-plane facade (ZCG-24 run_turn).
func (c *Client) Agent() *api.AgentAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	svc := c.Services()
	cfg := c.Config()
	return api.NewAgentAPIWith(c, api.AgentOptions{
		LLM:     svc.LLM,
		Zeus:    svc.Zeus,
		Journal: c.Journal(),
		IDs:     svc.IDs,
		Config:  cfg,
		Version: Version,
		Clock:   svc.Clock,
		Catalog: svc.Catalog,
	})
}

// Session is the durable-session facade (create / continue / rehydrate / trace).
// Trace joins Detective on the dispatch hop req_id (ZCG-16). Runtime has no
// dedicated session port — the API lazy-opens adapters/zeushttp.SessionClient
// from Config + HTTP.
func (c *Client) Session() *api.SessionAPI {
	if c == nil || c.rt == nil {
		return nil
	}
	svc := c.Services()
	cfg := c.Config()
	return api.NewSessionAPIWith(c, api.SessionOptions{
		HTTP:     svc.HTTP,
		Secrets:  svc.Secrets,
		Config:   cfg,
		Version:  Version,
		Identity: cfg.Client,
		Journal:  c.Journal(),
	})
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
