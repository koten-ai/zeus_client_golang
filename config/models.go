// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"fmt"
	"sort"
)

// AuthMode is zeus.auth_mode (Python AuthMode).
type AuthMode string

const (
	AuthNone        AuthMode = "none"
	AuthBasic       AuthMode = "basic"
	AuthBearer      AuthMode = "bearer"
	AuthSession     AuthMode = "session"
	AuthCertificate AuthMode = "certificate"
)

func validAuthMode(m AuthMode) bool {
	switch m {
	case AuthNone, AuthBasic, AuthBearer, AuthSession, AuthCertificate:
		return true
	default:
		return false
	}
}

// ZeusEndpointConfig is the data-plane connection (env names, not secrets).
type ZeusEndpointConfig struct {
	URL              string
	AuthMode         AuthMode
	Username         string
	PasswordEnv      string
	TokenEnv         string
	TimeoutS         float64
	TLSVerify        bool
	ScopeCredentials map[string]map[string]string
	CertFile         string
	KeyFile          string
	KeyFileEnv       string
}

func (z ZeusEndpointConfig) String() string {
	return fmt.Sprintf(
		"ZeusEndpointConfig(url=%q, auth_mode=%q, username=%q, password_env=%q, token_env=%q, timeout_s=%v, tls_verify=%v, cert_file=%q, key_file_env=%q)",
		z.URL, z.AuthMode, z.Username, z.PasswordEnv, z.TokenEnv, z.TimeoutS, z.TLSVerify, z.CertFile, z.KeyFileEnv,
	)
}

// DataTarget is bucket / scope / collection.
type DataTarget struct {
	Bucket     string
	Scope      string
	Collection string
}

// LlmRoleConfig is one config.llm.roles.* slice (api_key_env name only).
type LlmRoleConfig struct {
	Model       string
	APIKeyEnv   string
	BaseURL     string
	Provider    string
	Temperature *float64
}

func (r LlmRoleConfig) ToPublicDict() map[string]any {
	var temp any
	if r.Temperature != nil {
		temp = *r.Temperature
	}
	return map[string]any{
		"model":       nilIfEmpty(r.Model),
		"api_key_env": nilIfEmpty(r.APIKeyEnv),
		"base_url":    nilIfEmpty(r.BaseURL),
		"provider":    nilIfEmpty(r.Provider),
		"temperature": temp,
	}
}

func (r LlmRoleConfig) String() string {
	return fmt.Sprintf(
		"LlmRoleConfig(model=%q, api_key_env=%q, base_url=%q, provider=%q, temperature=%v)",
		r.Model, r.APIKeyEnv, r.BaseURL, r.Provider, r.Temperature,
	)
}

// JobsConfig is optional Pattern A host pin (unused until later tickets).
type JobsConfig struct {
	HostURL        string
	WatchTransport string
	Models         map[string]any
}

// LlmProviderConfig is config.llm (api_key_env name only).
type LlmProviderConfig struct {
	Provider            string
	BaseURL             string
	Model               string
	APIKeyEnv           string
	ContextWindowTokens int
	ContextSoftLimit    float64
	TimeoutS            float64
	Roles               map[string]LlmRoleConfig
}

func (l LlmProviderConfig) String() string {
	return fmt.Sprintf(
		"LlmProviderConfig(provider=%q, base_url=%q, model=%q, api_key_env=%q, context_window_tokens=%d, context_soft_limit=%v, timeout_s=%v, roles=%v)",
		l.Provider, l.BaseURL, l.Model, l.APIKeyEnv, l.ContextWindowTokens, l.ContextSoftLimit, l.TimeoutS, l.Roles,
	)
}

// ClientSettings is product loop settings.
type ClientSettings struct {
	AIProcessResult         bool
	MaxRounds               int
	ForceTrace              bool
	Mode                    string
	DurableSessions         bool
	StickyFlags             map[string]bool
	Messages                map[string]string
	SoftRequirePolicyAction bool
	AppOutputOnError        string
	OutputRequest           map[string]any
	Rules                   map[string]string
	TenantRules             map[string]string
	OverrideDefaults        bool
	CompanyContext          string
	Locale                  string
	Language                string
	Timezone                string
	Channel                 string
	Market                  string
	DeploymentID            string
	RulesetID               string
	ForceReturnRoundsLeft   int
	IgnoreUserToolPathHints bool
	ToolTrailEnabled        bool
	ToolTrailInject         bool
	ToolTrailMaxEntries     int
}

func (s ClientSettings) withAIProcessResult(v bool) ClientSettings {
	s.AIProcessResult = v
	return s
}

// Semantic cache nested knobs (L0 session.semantic_cache). Master default off.
type SemanticCacheRecallConfig struct {
	Enabled       bool
	TopK          int
	MinScore      float64
	Types         []string
	MinQueryChars int
	TimeoutMS     int
	FailClosed    bool
}

type SemanticCacheInjectConfig struct {
	Bag           string
	Key           string
	MaxChars      int
	MaxBlocks     int
	IncludeFields []string
	Order         string
}

type SemanticCacheWriteConfig struct {
	Enabled               bool
	OnTurnCommit          bool
	WriteExplicitOnly     bool
	WriteUserMessage      bool
	WriteAssistantSummary bool
	DefaultType           string
	TypesAllowed          []string
	MinChars              int
	MaxCharsPerBlock      int
	MaxBlocksPerTurn      int
	TTLSeconds            int
	AsyncWrite            bool
}

type SemanticCacheEmbedConfig struct {
	PreferSummaryForWrite bool
}

type SemanticCachePrivacyConfig struct {
	RedactBeforeWrite bool
	DenyRegex         []string
}

type SemanticCacheConfig struct {
	Enabled      bool
	ApplyToModes []string
	Recall       SemanticCacheRecallConfig
	Inject       SemanticCacheInjectConfig
	Write        SemanticCacheWriteConfig
	Embed        SemanticCacheEmbedConfig
	Privacy      SemanticCachePrivacyConfig
	DevUserID    string
}

func (s SemanticCacheConfig) withEnabled(v bool) SemanticCacheConfig {
	s.Enabled = v
	return s
}

func (s SemanticCacheConfig) ToPublicDict() map[string]any {
	return map[string]any{
		"enabled":        s.Enabled,
		"apply_to_modes": copyStrings(s.ApplyToModes),
		"recall": map[string]any{
			"enabled":         s.Recall.Enabled,
			"top_k":           s.Recall.TopK,
			"min_score":       s.Recall.MinScore,
			"types":           copyStrings(s.Recall.Types),
			"min_query_chars": s.Recall.MinQueryChars,
			"timeout_ms":      s.Recall.TimeoutMS,
			"fail_closed":     s.Recall.FailClosed,
		},
		"inject": map[string]any{
			"bag":            s.Inject.Bag,
			"key":            s.Inject.Key,
			"max_chars":      s.Inject.MaxChars,
			"max_blocks":     s.Inject.MaxBlocks,
			"include_fields": copyStrings(s.Inject.IncludeFields),
			"order":          s.Inject.Order,
		},
		"write": map[string]any{
			"enabled":                 s.Write.Enabled,
			"on_turn_commit":          s.Write.OnTurnCommit,
			"write_explicit_only":     s.Write.WriteExplicitOnly,
			"write_user_message":      s.Write.WriteUserMessage,
			"write_assistant_summary": s.Write.WriteAssistantSummary,
			"default_type":            s.Write.DefaultType,
			"min_chars":               s.Write.MinChars,
			"max_chars_per_block":     s.Write.MaxCharsPerBlock,
			"max_blocks_per_turn":     s.Write.MaxBlocksPerTurn,
			"ttl_seconds":             s.Write.TTLSeconds,
			"async":                   s.Write.AsyncWrite,
		},
		"embed": map[string]any{
			"prefer_summary_for_write": s.Embed.PreferSummaryForWrite,
		},
		"privacy": map[string]any{
			"redact_before_write": s.Privacy.RedactBeforeWrite,
			"deny_regex_count":    len(s.Privacy.DenyRegex),
		},
		"has_dev_user_id": s.DevUserID != "",
	}
}

// RetryPolicy is client retry knobs.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelayMS int
	MaxDelayMS  int
	Jitter      bool
}

// RedactionPolicy is journal/log preview policy (LOGGING.md).
type RedactionPolicy struct {
	Enabled         bool
	PreviewMaxChars int
}

// DebugPolicy is detective / capture / rewind flags.
type DebugPolicy struct {
	DetectiveBriefing bool
	CaptureBodies     bool
	TransportReplay   bool
	HubBaseURL        string
	Rewind            bool
}

func (d DebugPolicy) withRewind(v bool) DebugPolicy {
	d.Rewind = v
	return d
}

// RateLimitPolicy is client-side typeahead limits.
type RateLimitPolicy struct {
	TypeaheadEnabled bool
	TypeaheadRPS     float64
	TypeaheadBurst   float64
}

// LoggingPolicy is the family logger surface (four levels).
type LoggingPolicy struct {
	Level          string
	Redact         bool
	OTELEnabled    bool
	OTELEndpoint   string
	ServiceName    string
	ServiceVersion string
}

// ClientIdentity is optional host identity. Never invent IP.
type ClientIdentity struct {
	IPAddress string
}

// RuntimeConfig is the immutable bind result (Python RuntimeConfig).
type RuntimeConfig struct {
	Profile              string
	User                 string // product stamp; default zeus_client
	Zeus                 ZeusEndpointConfig
	Target               DataTarget
	LLM                  LlmProviderConfig
	Settings             ClientSettings
	Retry                RetryPolicy
	Redaction            RedactionPolicy
	Debug                DebugPolicy
	RateLimit            RateLimitPolicy
	Logging              LoggingPolicy
	Client               ClientIdentity
	ChatRequestsDir      string
	Jobs                 JobsConfig
	ClientFloor          string
	AllowDegradedCatalog bool
	ScopeContracts       map[string]any
	ProductionBaseID     string
	SemanticCache        SemanticCacheConfig
}

func (c RuntimeConfig) String() string {
	return fmt.Sprintf(
		"RuntimeConfig(profile=%q, zeus=%s, target=%+v, llm=%s, settings=%+v, retry=%+v, redaction=%+v, debug=%+v, rate_limit=%+v, logging=%+v, client=%+v, chat_requests_dir=%q, jobs=%+v, semantic_cache.enabled=%v)",
		c.Profile, c.Zeus, c.Target, c.LLM, c.Settings, c.Retry, c.Redaction, c.Debug, c.RateLimit, c.Logging, c.Client, c.ChatRequestsDir, c.Jobs, c.SemanticCache.Enabled,
	)
}

// Default is Python RuntimeConfig() — profile name set, profile policies not applied.
func Default() RuntimeConfig {
	return RuntimeConfig{
		Profile: "development",
		User:    "zeus_client",
		Zeus: ZeusEndpointConfig{
			URL:              "http://127.0.0.1:8080",
			AuthMode:         AuthNone,
			TimeoutS:         30,
			TLSVerify:        true,
			ScopeCredentials: map[string]map[string]string{},
		},
		Target: DataTarget{Bucket: "yelp-data", Scope: "_default", Collection: "_default"},
		LLM: LlmProviderConfig{
			Provider:            "xai",
			BaseURL:             "https://api.x.ai/v1",
			Model:               "grok-4-1-non-reasoning",
			APIKeyEnv:           "XAI_API_KEY",
			ContextWindowTokens: 128000,
			ContextSoftLimit:    0.8,
			TimeoutS:            120,
			Roles:               map[string]LlmRoleConfig{},
		},
		Settings: ClientSettings{
			AIProcessResult:         false,
			MaxRounds:               8,
			Mode:                    "analytics",
			DurableSessions:         true,
			StickyFlags:             map[string]bool{},
			Messages:                map[string]string{},
			SoftRequirePolicyAction: true,
			AppOutputOnError:        "strip",
			ForceReturnRoundsLeft:   1,
			IgnoreUserToolPathHints: true,
			ToolTrailEnabled:        true,
			ToolTrailInject:         true,
			ToolTrailMaxEntries:     16,
		},
		Retry:          RetryPolicy{MaxAttempts: 3, BaseDelayMS: 200, MaxDelayMS: 5000, Jitter: true},
		Redaction:      RedactionPolicy{Enabled: true, PreviewMaxChars: 2048},
		Debug:          DebugPolicy{DetectiveBriefing: true, CaptureBodies: false, TransportReplay: true, Rewind: false},
		RateLimit:      RateLimitPolicy{TypeaheadEnabled: true, TypeaheadRPS: 10, TypeaheadBurst: 20},
		Logging:        LoggingPolicy{Level: "info", Redact: true, ServiceName: "zeus_client"},
		Jobs:           JobsConfig{WatchTransport: "sse", Models: map[string]any{}},
		ClientFloor:    "client-floor-5",
		ScopeContracts: map[string]any{},
		SemanticCache:  defaultSemanticCache(),
	}
}

func defaultSemanticCache() SemanticCacheConfig {
	return SemanticCacheConfig{
		Enabled:      false,
		ApplyToModes: []string{"agent"},
		Recall: SemanticCacheRecallConfig{
			Enabled:       true,
			TopK:          5,
			Types:         []string{"profile", "semantic", "conversational"},
			MinQueryChars: 12,
			TimeoutMS:     150,
		},
		Inject: SemanticCacheInjectConfig{
			Bag:           "B",
			Key:           "semantic_memory",
			MaxChars:      4000,
			MaxBlocks:     5,
			IncludeFields: []string{"summary", "text"},
			Order:         "score_desc",
		},
		Write: SemanticCacheWriteConfig{
			Enabled:           true,
			OnTurnCommit:      true,
			WriteExplicitOnly: true,
			DefaultType:       "conversational",
			TypesAllowed:      []string{"conversational", "profile", "semantic"},
			MinChars:          24,
			MaxCharsPerBlock:  2000,
			MaxBlocksPerTurn:  3,
			TTLSeconds:        604800,
			AsyncWrite:        true,
		},
		Embed: SemanticCacheEmbedConfig{PreferSummaryForWrite: true},
	}
}

// ToPublicDict is a log/support snapshot (no secret values).
func (c RuntimeConfig) ToPublicDict() map[string]any {
	roles := map[string]any{}
	for name, role := range c.LLM.Roles {
		roles[name] = role.ToPublicDict()
	}
	stickyKeys := make([]string, 0, len(c.Settings.StickyFlags))
	for k := range c.Settings.StickyFlags {
		stickyKeys = append(stickyKeys, k)
	}
	sort.Strings(stickyKeys)
	msgKeys := make([]string, 0, len(c.Settings.Messages))
	for k := range c.Settings.Messages {
		msgKeys = append(msgKeys, k)
	}
	sort.Strings(msgKeys)
	scopeKeys := make([]string, 0, len(c.ScopeContracts))
	for k := range c.ScopeContracts {
		scopeKeys = append(scopeKeys, k)
	}
	sort.Strings(scopeKeys)
	jobsModels := map[string]any{}
	for k, v := range c.Jobs.Models {
		jobsModels[k] = v
	}
	return map[string]any{
		"profile": c.Profile,
		"user":    c.User,
		"zeus": map[string]any{
			"url":          c.Zeus.URL,
			"auth_mode":    string(c.Zeus.AuthMode),
			"username":     nilIfEmpty(c.Zeus.Username),
			"password_env": nilIfEmpty(c.Zeus.PasswordEnv),
			"token_env":    nilIfEmpty(c.Zeus.TokenEnv),
			"timeout_s":    c.Zeus.TimeoutS,
			"tls_verify":   c.Zeus.TLSVerify,
			"cert_file":    nilIfEmpty(c.Zeus.CertFile),
			"key_file_env": nilIfEmpty(c.Zeus.KeyFileEnv),
		},
		"target": map[string]any{
			"bucket":     c.Target.Bucket,
			"scope":      c.Target.Scope,
			"collection": c.Target.Collection,
		},
		"llm": map[string]any{
			"provider":              c.LLM.Provider,
			"base_url":              c.LLM.BaseURL,
			"model":                 c.LLM.Model,
			"api_key_env":           c.LLM.APIKeyEnv,
			"context_window_tokens": c.LLM.ContextWindowTokens,
			"context_soft_limit":    c.LLM.ContextSoftLimit,
			"timeout_s":             c.LLM.TimeoutS,
			"roles":                 roles,
		},
		"settings": map[string]any{
			"ai_process_result":           c.Settings.AIProcessResult,
			"max_rounds":                  c.Settings.MaxRounds,
			"force_trace":                 c.Settings.ForceTrace,
			"mode":                        c.Settings.Mode,
			"durable_sessions":            c.Settings.DurableSessions,
			"soft_require_policy_action":  c.Settings.SoftRequirePolicyAction,
			"app_output_on_error":         c.Settings.AppOutputOnError,
			"sticky_flag_keys":            stickyKeys,
			"message_keys":                msgKeys,
			"has_output_request":          c.Settings.OutputRequest != nil,
			"ruleset_id":                  nilIfEmpty(c.Settings.RulesetID),
			"has_rules":                   len(c.Settings.Rules) > 0,
			"ignore_user_tool_path_hints": c.Settings.IgnoreUserToolPathHints,
			"force_return_rounds_left":    c.Settings.ForceReturnRoundsLeft,
			"tool_trail_enabled":          c.Settings.ToolTrailEnabled,
		},
		"retry": map[string]any{
			"max_attempts":  c.Retry.MaxAttempts,
			"base_delay_ms": c.Retry.BaseDelayMS,
			"max_delay_ms":  c.Retry.MaxDelayMS,
			"jitter":        c.Retry.Jitter,
		},
		"redaction": map[string]any{
			"enabled":           c.Redaction.Enabled,
			"preview_max_chars": c.Redaction.PreviewMaxChars,
		},
		"debug": map[string]any{
			"detective_briefing": c.Debug.DetectiveBriefing,
			"capture_bodies":     c.Debug.CaptureBodies,
			"transport_replay":   c.Debug.TransportReplay,
			"rewind":             c.Debug.Rewind,
		},
		"rate_limit": map[string]any{
			"typeahead_enabled": c.RateLimit.TypeaheadEnabled,
			"typeahead_rps":     c.RateLimit.TypeaheadRPS,
			"typeahead_burst":   c.RateLimit.TypeaheadBurst,
		},
		"logging": map[string]any{
			"level":           c.Logging.Level,
			"redact":          c.Logging.Redact,
			"otel_enabled":    c.Logging.OTELEnabled,
			"otel_endpoint":   nilIfEmpty(c.Logging.OTELEndpoint),
			"service_name":    c.Logging.ServiceName,
			"service_version": nilIfEmpty(c.Logging.ServiceVersion),
		},
		"client": map[string]any{
			"ip_address": nilIfEmpty(c.Client.IPAddress),
		},
		"chat_requests_dir": nilIfEmpty(c.ChatRequestsDir),
		"jobs": map[string]any{
			"host_url":        nilIfEmpty(c.Jobs.HostURL),
			"watch_transport": c.Jobs.WatchTransport,
			"models":          jobsModels,
		},
		"client_floor":           c.ClientFloor,
		"allow_degraded_catalog": c.AllowDegradedCatalog,
		"scope_contract_keys":    scopeKeys,
		"production_base_id":     nilIfEmpty(c.ProductionBaseID),
		"semantic_cache":         c.SemanticCache.ToPublicDict(),
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func copyStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
