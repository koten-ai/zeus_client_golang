// SPDX-License-Identifier: BUSL-1.1

package zeusclient

import (
	"context"
	"io"
	"sync"

	"github.com/koten-ai/zeus_client_golang/adapters/secretsenv"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
	"github.com/koten-ai/zeus_client_golang/ports"
	"github.com/koten-ai/zeus_client_golang/security"
)

// Services is the internal dependency bundle (Python runtime.Services).
// Network ports stay nil unless the caller injects fakes / adapters.
type Services struct {
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

// runtime is the hexagonal wiring bundle (Python ZeusRuntime.Services).
// No process-global HTTP. Adapters are instance fields, not package vars.
type runtime struct {
	mu     sync.Mutex
	closed bool

	cfg      config.RuntimeConfig
	journal  journal.ExecutionJournal
	secrets  ports.SecretStore
	clock    ports.Clock
	ids      ports.IDFactory
	redactor security.Redactor
	http     ports.HttpPort
	zeus     ports.ZeusPort
	llm      ports.LlmPort
	catalog  ports.CatalogStore
	jobs     ports.Jobs
}

func newRuntime(cfg config.RuntimeConfig, opts Options) *runtime {
	j := opts.Journal
	if j == nil {
		j = journal.NewInMemoryJournal(nil)
	}
	sec := opts.Secrets
	if sec == nil {
		sec = secretsenv.New()
	}
	clk := opts.Clock
	if clk == nil {
		clk = ports.SystemClock{}
	}
	ids := opts.IDs
	if ids == nil {
		ids = ports.UUIDFactory{}
	}
	red := opts.Redactor
	if red == nil {
		red = security.New()
	}
	return &runtime{
		cfg:      cfg,
		journal:  j,
		secrets:  sec,
		clock:    clk,
		ids:      ids,
		redactor: red,
		http:     opts.HTTP,
		zeus:     opts.Zeus,
		llm:      opts.LLM,
		catalog:  opts.Catalog,
		jobs:     opts.Jobs,
	}
}

func (r *runtime) services() Services {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Services{
		Journal:  r.journal,
		Secrets:  r.secrets,
		Clock:    r.clock,
		IDs:      r.ids,
		Redactor: r.redactor,
		HTTP:     r.http,
		Zeus:     r.zeus,
		LLM:      r.llm,
		Catalog:  r.catalog,
		Jobs:     r.jobs,
	}
}

func (r *runtime) configSnapshot() config.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Snapshot()
}

func (r *runtime) close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	httpPort := r.http
	deps := []any{r.zeus, r.llm, r.catalog, r.jobs}
	r.mu.Unlock()

	ctx := context.Background()
	var first error
	if httpPort != nil {
		if err := httpPort.Close(ctx); err != nil {
			first = err
		}
	}
	for _, dep := range deps {
		if dep == nil || sameAdapter(dep, httpPort) {
			continue
		}
		if err := closeAdapter(ctx, dep); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func sameAdapter(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	return a == b
}

func closeAdapter(ctx context.Context, dep any) error {
	switch c := dep.(type) {
	case ports.Closer:
		return c.Close(ctx)
	case io.Closer:
		return c.Close()
	default:
		return nil
	}
}
