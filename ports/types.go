// SPDX-License-Identifier: BUSL-1.1

package ports

import "github.com/koten-ai/zeus_client_golang/config"

// AuthContext is minted auth for a Zeus hop (Python AuthContext).
// Mode empty means "none". Headers are names/values only — no secrets.
type AuthContext struct {
	Headers map[string]string
	Mode    string
}

// VerbRequest is one Zeus data-plane hop (Python VerbRequest).
// Secrets never live here — only env names. AllowPipeline defaults false
// (Direct public surface must keep it false).
type VerbRequest struct {
	Verb       string
	Body       map[string]any
	Target     config.DataTarget
	ModeHeader string // Python default "analytics" when adapters fill it
	Headers    map[string]string
	// Agent path may dispatch pipeline; Direct public surface must keep false.
	AllowPipeline bool
	// Default omit X-Zeus-Req-Id (Zeus mints). Tests may pre-mint UUID v4.
	PreMintReqID bool
	// Optional per-call Zeus host / auth names (unit overrides).
	BaseURL     string
	AuthMode    string
	PasswordEnv string
	TokenEnv    string
	Username    string
	// Query ?rewind=true (never a verb JSON field). Default off.
	Rewind bool
}

// VerbHopResult is the transport-level verb outcome (Python VerbHopResult).
type VerbHopResult struct {
	OK         bool
	StatusCode int
	ReqID      string
	Body       map[string]any
	Error      string
	URL        string
}

// LlmRequest is one completion call (Python LlmRequest).
type LlmRequest struct {
	Messages    []map[string]any
	Model       string
	Tools       []map[string]any
	Temperature *float64
	MaxTokens   *int
	ConvID      string // cache routing key (xAI conv / OpenAI prompt_cache_key)
}

// LlmResponse is the completion result (Python LlmResponse).
type LlmResponse struct {
	Content   string
	ToolCalls []map[string]any
	Raw       map[string]any
	Usage     map[string]any
}

// CatalogKey identifies a catalog document (Python CatalogKey).
type CatalogKey struct {
	Mode   string
	Bucket string
	Scope  string
	BaseID string
}

// CatalogDocument is a loaded/saved catalog body (Python CatalogDocument).
type CatalogDocument struct {
	Body         map[string]any
	Path         string
	ContractHash string
}
