// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
)

// FromMapping builds RuntimeConfig from a plain mapping (file JSON shape) and applies profile.
func FromMapping(data map[string]any, profile string) (RuntimeConfig, error) {
	if data == nil {
		data = map[string]any{}
	}
	if isPinsMapping(data) {
		cfg := overlayPins(Default(), data)
		prof := profile
		if prof == "" {
			prof = asString(data["profile"], cfg.Profile)
		}
		return ApplyProfile(cfg, prof)
	}
	return fromRuntimeMapping(data, profile)
}

func fromRuntimeMapping(data map[string]any, profile string) (RuntimeConfig, error) {
	z := childMap(data, "zeus")
	t := childMap(data, "target")
	if len(t) == 0 {
		if samples := childMap(data, "samples"); len(samples) > 0 {
			t = map[string]any{
				"bucket":     samples["bucket"],
				"scope":      samples["scope"],
				"collection": samples["collection"],
			}
		}
	}
	llm := childMap(data, "llm")
	settings := childMap(data, "settings")
	retry := childMap(data, "retry")
	redaction := childMap(data, "redaction")
	debug := childMap(data, "debug")
	rateLimit := childMap(data, "rate_limit")
	loggingRaw := childMap(data, "logging")
	clientRaw := childMap(data, "client")
	jobsRaw := childMap(data, "jobs")
	sessionRaw := childMap(data, "session")

	base := Default()
	defaultKeyEnv := asString(llm["api_key_env"], base.LLM.APIKeyEnv)
	roles := parseLLMRoles(llm["roles"], defaultKeyEnv)
	provider := asString(llm["provider"], "xai")
	baseDefault := "https://api.x.ai/v1"
	modelDefault := "grok-4-1-non-reasoning"
	if strings.EqualFold(provider, "custom") {
		baseDefault = ""
		modelDefault = ""
	}

	authMode := AuthMode(strings.ToLower(asString(z["auth_mode"], string(base.Zeus.AuthMode))))
	if !validAuthMode(authMode) {
		return RuntimeConfig{}, domain.NewConfig(domain.CodeZeusAuthModeInvalid, "config.loader",
			domain.WithMessage("zeus.auth_mode invalid: "+string(authMode)))
	}

	url := rstripSlash(asString(z["url"], ""))
	if url == "" {
		url = rstripSlash(asString(z["base_url"], ""))
	}
	if url == "" {
		url = base.Zeus.URL
	}
	if url == "" {
		return RuntimeConfig{}, domain.NewConfig(domain.CodeZeusURLMissing, "config.loader",
			domain.WithMessage("zeus.url missing or empty"))
	}

	tlsVerify := asBool(z["tls_verify"], asBool(z["verify_tls"], true))

	prof := profile
	if prof == "" {
		prof = asString(data["profile"], "development")
	}

	cfg := base
	cfg.Profile = prof
	if u := asString(data["user"], ""); u != "" {
		cfg.User = u
	}
	cfg.Zeus = ZeusEndpointConfig{
		URL:              url,
		AuthMode:         authMode,
		Username:         asString(z["username"], ""),
		PasswordEnv:      asString(firstNonNil(z["password_env"], z["password_env_name"]), ""),
		TokenEnv:         asString(firstNonNil(z["token_env"], z["token_env_name"]), ""),
		TimeoutS:         asFloat(z["timeout_s"], 30),
		TLSVerify:        tlsVerify,
		ScopeCredentials: parseScopeCredentials(z["scope_credentials"]),
		CertFile:         asString(z["cert_file"], ""),
		KeyFile:          asString(z["key_file"], ""),
		KeyFileEnv:       asString(z["key_file_env"], ""),
	}
	cfg.Target = DataTarget{
		Bucket:     asString(t["bucket"], "yelp-data"),
		Scope:      asString(t["scope"], "_default"),
		Collection: asString(t["collection"], "_default"),
	}
	cfg.LLM = LlmProviderConfig{
		Provider:            provider,
		BaseURL:             rstripSlash(asString(llm["base_url"], baseDefault)),
		Model:               asString(llm["model"], modelDefault),
		APIKeyEnv:           defaultKeyEnv,
		APIStyle:            asString(llm["api_style"], ""),
		ContextWindowTokens: asInt(llm["context_window_tokens"], 128000),
		ContextSoftLimit:    asFloat(llm["context_soft_limit"], 0.8),
		TimeoutS:            asFloat(llm["timeout_s"], 120),
		Roles:               roles,
	}
	cfg.Settings = parseClientSettings(settings, base.Settings)
	cfg.Retry = RetryPolicy{
		MaxAttempts: asInt(retry["max_attempts"], 3),
		BaseDelayMS: asInt(retry["base_delay_ms"], 200),
		MaxDelayMS:  asInt(retry["max_delay_ms"], 5000),
		Jitter:      asBool(retry["jitter"], true),
	}
	cfg.Redaction = RedactionPolicy{
		Enabled:         asBool(redaction["enabled"], true),
		PreviewMaxChars: asInt(redaction["preview_max_chars"], 2048),
	}
	cfg.Debug = DebugPolicy{
		DetectiveBriefing: asBool(debug["detective_briefing"], base.Debug.DetectiveBriefing),
		CaptureBodies:     asBool(debug["capture_bodies"], false),
		TransportReplay:   asBool(debug["transport_replay"], true),
		HubBaseURL:        asString(debug["hub_base_url"], ""),
		Rewind:            asBool(debug["rewind"], asBool(settings["rewind"], false)),
	}
	cfg.RateLimit = RateLimitPolicy{
		TypeaheadEnabled: asBool(rateLimit["typeahead_enabled"], true),
		TypeaheadRPS:     asFloat(rateLimit["typeahead_rps"], 10),
		TypeaheadBurst:   asFloat(rateLimit["typeahead_burst"], 20),
	}
	cfg.Logging = LoggingPolicy{
		Level:          strings.ToLower(strings.TrimSpace(asString(loggingRaw["level"], "info"))),
		Redact:         asBool(loggingRaw["redact"], true),
		OTELEnabled:    asBool(loggingRaw["otel_enabled"], false),
		OTELEndpoint:   strings.TrimSpace(asString(loggingRaw["otel_endpoint"], "")),
		ServiceName:    asString(loggingRaw["service_name"], "zeus_client"),
		ServiceVersion: asString(loggingRaw["service_version"], ""),
	}
	cfg.Client = ClientIdentity{IPAddress: strings.TrimSpace(asString(clientRaw["ip_address"], ""))}
	cfg.ChatRequestsDir = asString(data["chat_requests_dir"], "")
	cfg.Jobs = parseJobs(jobsRaw)
	cfg.SemanticCache = parseSemanticCache(sessionRaw["semantic_cache"])
	cfg.ClientFloor = asString(data["client_floor"], "client-floor-5")
	cfg.AllowDegradedCatalog = asBool(data["allow_degraded_catalog"], false)
	cfg.ProductionBaseID = productionBaseID(data)
	cfg.ScopeContracts = parseScopeContracts(data, z)
	return ApplyProfile(cfg, cfg.Profile)
}

func parseLLMRoles(raw any, defaultAPIKeyEnv string) map[string]LlmRoleConfig {
	m := asMap(raw)
	out := make(map[string]LlmRoleConfig, len(m))
	for name, spec := range m {
		if !isMap(spec) {
			continue
		}
		sm := asMap(spec)
		keyEnv := asString(sm["api_key_env"], "")
		if keyEnv == "" {
			keyEnv = defaultAPIKeyEnv
		}
		role := LlmRoleConfig{
			Model:     asString(sm["model"], ""),
			APIKeyEnv: keyEnv,
			BaseURL:   asString(sm["base_url"], ""),
			Provider:  asString(sm["provider"], ""),
		}
		if sm["temperature"] != nil {
			t := asFloat(sm["temperature"], 0)
			role.Temperature = &t
		}
		out[name] = role
	}
	return out
}

func parseClientSettings(settings map[string]any, def ClientSettings) ClientSettings {
	out := def
	out.AIProcessResult = asBool(settings["ai_process_result"], false)
	out.MaxRounds = asInt(settings["max_rounds"], 8)
	out.ForceTrace = asBool(settings["force_trace"], false)
	out.Mode = asString(settings["mode"], "analytics")
	out.DurableSessions = asBool(settings["durable_sessions"], def.DurableSessions)
	out.StickyFlags = asBoolMap(settings["sticky_flags"])
	out.Messages = asStringMap(settings["messages"])
	out.SoftRequirePolicyAction = asBool(settings["soft_require_policy_action"], true)
	out.AppOutputOnError = asString(settings["app_output_on_error"], "strip")
	if out.AppOutputOnError == "" {
		out.AppOutputOnError = "strip"
	}
	if or := asMap(settings["output_request"]); settings["output_request"] != nil && isMap(settings["output_request"]) {
		out.OutputRequest = copyAnyMap(or)
	}
	out.Rules = asRulesMap(settings["rules"])
	out.TenantRules = asRulesMap(settings["tenant_rules"])
	out.OverrideDefaults = asBool(settings["override_defaults"], false)
	out.CompanyContext = asString(settings["company_context"], "")
	out.Locale = asString(settings["locale"], "")
	out.Language = asString(settings["language"], "")
	out.Timezone = asString(settings["timezone"], "")
	out.Channel = asString(settings["channel"], "")
	out.Market = asString(settings["market"], "")
	out.DeploymentID = asString(settings["deployment_id"], "")
	out.RulesetID = asString(settings["ruleset_id"], "")
	if settings["force_return_rounds_left"] != nil {
		out.ForceReturnRoundsLeft = asInt(settings["force_return_rounds_left"], 1)
	} else {
		out.ForceReturnRoundsLeft = 1
	}
	out.IgnoreUserToolPathHints = asBool(settings["ignore_user_tool_path_hints"], true)
	out.ToolTrailEnabled = asBool(settings["tool_trail_enabled"], true)
	out.ToolTrailInject = asBool(settings["tool_trail_inject"], true)
	out.ToolTrailMaxEntries = asInt(settings["tool_trail_max_entries"], 16)
	return out
}

func asRulesMap(raw any) map[string]string {
	if raw == nil {
		return nil
	}
	if !isMap(raw) {
		return nil
	}
	return asStringMap(raw)
}

func parseJobs(raw map[string]any) JobsConfig {
	models := map[string]any{}
	if m := asMap(raw["models"]); isMap(raw["models"]) {
		models = copyAnyMap(m)
	}
	host := asString(raw["host_url"], "")
	return JobsConfig{HostURL: host, WatchTransport: "sse", Models: models}
}

func parseScopeCredentials(raw any) map[string]map[string]string {
	out := map[string]map[string]string{}
	if !isMap(raw) {
		return out
	}
	for k, v := range asMap(raw) {
		if !isMap(v) {
			continue
		}
		inner := map[string]string{}
		for ik, iv := range asMap(v) {
			inner[ik] = asString(iv, "")
		}
		out[k] = inner
	}
	return out
}

func parseScopeContracts(data, z map[string]any) map[string]any {
	if isMap(data["scope_contracts"]) {
		return copyAnyMap(asMap(data["scope_contracts"]))
	}
	if isMap(z["scope_contracts"]) {
		return copyAnyMap(asMap(z["scope_contracts"]))
	}
	return map[string]any{}
}

func productionBaseID(data map[string]any) string {
	if v := asString(data["production_base_id"], ""); v != "" {
		return v
	}
	cat := childMap(data, "catalog")
	return asString(cat["production_base_id"], "")
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func applyEnv(cfg RuntimeConfig, env map[string]string) (RuntimeConfig, error) {
	if env == nil {
		env = map[string]string{}
	}
	z := cfg.Zeus
	if url := envOr(env, "ZEUS_CLIENT_URL", envOr(env, "ZEUS_URL", "")); url != "" {
		z.URL = rstripSlash(url)
		z.Username = envOr(env, "ZEUS_CLIENT_USERNAME", z.Username)
		z.PasswordEnv = envOr(env, "ZEUS_CLIENT_PASSWORD_ENV", z.PasswordEnv)
		z.TokenEnv = envOr(env, "ZEUS_CLIENT_TOKEN_ENV", z.TokenEnv)
		if envHas(env, "ZEUS_CLIENT_TIMEOUT_S") {
			z.TimeoutS = asFloat(env["ZEUS_CLIENT_TIMEOUT_S"], z.TimeoutS)
		}
		z.TLSVerify = envBool(env, "ZEUS_CLIENT_TLS_VERIFY", z.TLSVerify)
	} else {
		if m := AuthMode(strings.ToLower(envOr(env, "ZEUS_CLIENT_AUTH_MODE", string(z.AuthMode)))); m != "" {
			z.AuthMode = m
		}
		if !validAuthMode(z.AuthMode) {
			return RuntimeConfig{}, domain.NewConfig(domain.CodeZeusAuthModeInvalid, "config.loader",
				domain.WithMessage("zeus.auth_mode invalid: "+string(z.AuthMode)))
		}
		z.Username = envOr(env, "ZEUS_CLIENT_USERNAME", z.Username)
		z.PasswordEnv = envOr(env, "ZEUS_CLIENT_PASSWORD_ENV", z.PasswordEnv)
		z.TokenEnv = envOr(env, "ZEUS_CLIENT_TOKEN_ENV", z.TokenEnv)
		if envHas(env, "ZEUS_CLIENT_TIMEOUT_S") {
			z.TimeoutS = asFloat(env["ZEUS_CLIENT_TIMEOUT_S"], z.TimeoutS)
		}
		z.TLSVerify = envBool(env, "ZEUS_CLIENT_TLS_VERIFY", z.TLSVerify)
	}

	target := cfg.Target
	if envHas(env, "ZEUS_CLIENT_BUCKET") || envHas(env, "ZEUS_CLIENT_SCOPE") || envHas(env, "ZEUS_CLIENT_COLLECTION") {
		target.Bucket = envOr(env, "ZEUS_CLIENT_BUCKET", target.Bucket)
		target.Scope = envOr(env, "ZEUS_CLIENT_SCOPE", target.Scope)
		target.Collection = envOr(env, "ZEUS_CLIENT_COLLECTION", target.Collection)
	}

	llm := cfg.LLM
	llm.Provider = envOr(env, "ZEUS_CLIENT_LLM_PROVIDER", llm.Provider)
	llm.BaseURL = rstripSlash(envOr(env, "ZEUS_CLIENT_LLM_BASE_URL", llm.BaseURL))
	llm.Model = envOr(env, "ZEUS_CLIENT_LLM_MODEL", llm.Model)
	llm.APIKeyEnv = envOr(env, "ZEUS_CLIENT_LLM_API_KEY_ENV", llm.APIKeyEnv)
	if envHas(env, "ZEUS_CLIENT_LLM_CONTEXT_WINDOW") {
		llm.ContextWindowTokens = asInt(env["ZEUS_CLIENT_LLM_CONTEXT_WINDOW"], llm.ContextWindowTokens)
	}
	if envHas(env, "ZEUS_CLIENT_LLM_CONTEXT_SOFT_LIMIT") {
		llm.ContextSoftLimit = asFloat(env["ZEUS_CLIENT_LLM_CONTEXT_SOFT_LIMIT"], llm.ContextSoftLimit)
	}
	if envHas(env, "ZEUS_CLIENT_LLM_TIMEOUT_S") {
		llm.TimeoutS = asFloat(env["ZEUS_CLIENT_LLM_TIMEOUT_S"], llm.TimeoutS)
	}

	settings := cfg.Settings
	if envHas(env, "ZEUS_CLIENT_AI_PROCESS_RESULT") || envHas(env, "ZEUS_CLIENT_MAX_ROUNDS") ||
		envHas(env, "ZEUS_CLIENT_FORCE_TRACE") || envHas(env, "ZEUS_CLIENT_MODE") ||
		envHas(env, "ZEUS_CLIENT_DURABLE_SESSIONS") {
		if envHas(env, "ZEUS_CLIENT_AI_PROCESS_RESULT") {
			settings.AIProcessResult = envBool(env, "ZEUS_CLIENT_AI_PROCESS_RESULT", settings.AIProcessResult)
		}
		if envHas(env, "ZEUS_CLIENT_MAX_ROUNDS") {
			settings.MaxRounds = asInt(env["ZEUS_CLIENT_MAX_ROUNDS"], settings.MaxRounds)
		}
		settings.ForceTrace = envBool(env, "ZEUS_CLIENT_FORCE_TRACE", settings.ForceTrace)
		settings.Mode = envOr(env, "ZEUS_CLIENT_MODE", settings.Mode)
		settings.DurableSessions = envBool(env, "ZEUS_CLIENT_DURABLE_SESSIONS", settings.DurableSessions)
	}

	logPol := cfg.Logging
	if envHas(env, "ZEUS_CLIENT_LOG_LEVEL") || envHas(env, "ZEUS_CLIENT_LOG_REDACT") ||
		envHas(env, "ZEUS_CLIENT_OTEL_ENABLED") || envHas(env, "ZEUS_CLIENT_OTEL_ENDPOINT") ||
		envHas(env, "ZEUS_CLIENT_SERVICE_NAME") || envHas(env, "ZEUS_CLIENT_SERVICE_VERSION") {
		if envHas(env, "ZEUS_CLIENT_LOG_LEVEL") {
			logPol.Level = strings.ToLower(strings.TrimSpace(env["ZEUS_CLIENT_LOG_LEVEL"]))
		}
		if envHas(env, "ZEUS_CLIENT_LOG_REDACT") {
			logPol.Redact = envBool(env, "ZEUS_CLIENT_LOG_REDACT", logPol.Redact)
		}
		if envHas(env, "ZEUS_CLIENT_OTEL_ENABLED") {
			logPol.OTELEnabled = envBool(env, "ZEUS_CLIENT_OTEL_ENABLED", logPol.OTELEnabled)
		}
		if envHas(env, "ZEUS_CLIENT_OTEL_ENDPOINT") {
			logPol.OTELEndpoint = strings.TrimSpace(env["ZEUS_CLIENT_OTEL_ENDPOINT"])
		}
		logPol.ServiceName = envOr(env, "ZEUS_CLIENT_SERVICE_NAME", logPol.ServiceName)
		if envHas(env, "ZEUS_CLIENT_SERVICE_VERSION") {
			logPol.ServiceVersion = env["ZEUS_CLIENT_SERVICE_VERSION"]
		}
	}

	ident := cfg.Client
	if envHas(env, "ZEUS_CLIENT_IP") {
		ident.IPAddress = strings.TrimSpace(env["ZEUS_CLIENT_IP"])
	}

	jobs := cfg.Jobs
	if host := env["ZEUS_CLIENT_JOBS_HOST_URL"]; host != "" {
		jobs.HostURL = host
	}

	semantic := cfg.SemanticCache
	if envHas(env, "ZEUS_CLIENT_SEMANTIC_CACHE") {
		semantic = semantic.withEnabled(envBool(env, "ZEUS_CLIENT_SEMANTIC_CACHE", false))
	}

	debug := cfg.Debug
	if envHas(env, "ZEUS_REWIND") {
		debug = debug.withRewind(asBool(env["ZEUS_REWIND"], debug.Rewind))
	} else if envHas(env, "ZEUS_CLIENT_REWIND") {
		debug = debug.withRewind(asBool(env["ZEUS_CLIENT_REWIND"], debug.Rewind))
	}

	cfg.Profile = envOr(env, "ZEUS_CLIENT_PROFILE", cfg.Profile)
	cfg.Zeus = z
	cfg.Target = target
	cfg.LLM = llm
	cfg.Settings = settings
	cfg.Logging = logPol
	cfg.Debug = debug
	cfg.Client = ident
	cfg.ChatRequestsDir = envOr(env, "ZEUS_CLIENT_CHAT_REQUESTS_DIR", cfg.ChatRequestsDir)
	cfg.Jobs = jobs
	cfg.ClientFloor = envOr(env, "ZEUS_CLIENT_FLOOR", cfg.ClientFloor)
	cfg.SemanticCache = semantic
	if envHas(env, "ZEUS_CLIENT_USER") {
		cfg.User = env["ZEUS_CLIENT_USER"]
	}
	return ApplyProfile(cfg, cfg.Profile)
}

// Load reads optional JSON path (RuntimeConfig mapping or sdk_bootstrap pins),
// then applies profile and env overlays. env nil uses the process environment;
// an empty map isolates tests from process env.
func Load(path, profile string, env map[string]string) (RuntimeConfig, error) {
	if env == nil {
		env = environMap()
	}
	data := map[string]any{}
	if path != "" {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return RuntimeConfig{}, domain.NewConfig(domain.CodeConfigPathNotFound, "config.loader",
				domain.WithMessage("config path not found: "+path),
				domain.WithDetails(map[string]any{"path": path}),
				domain.WithCause(err))
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return RuntimeConfig{}, domain.NewConfig(domain.CodeConfigNotReadable, "config.loader",
				domain.WithMessage("config path not readable"),
				domain.WithDetails(map[string]any{"path": path}),
				domain.WithCause(err))
		}
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return RuntimeConfig{}, domain.NewConfig(domain.CodeConfigParseFailed, "config.loader",
				domain.WithMessage("config file parse failed"),
				domain.WithDetails(map[string]any{"path": path, "error": err.Error()}),
				domain.WithCause(err))
		}
		m, ok := parsed.(map[string]any)
		if !ok {
			return RuntimeConfig{}, domain.NewConfig(domain.CodeConfigInvalid, "config.loader",
				domain.WithMessage("config mapping invalid"))
		}
		data = m
	}
	prof := profile
	if prof == "" {
		prof = envOr(env, "ZEUS_CLIENT_PROFILE", asString(data["profile"], "development"))
	}
	cfg, err := FromMapping(data, prof)
	if err != nil {
		return RuntimeConfig{}, err
	}
	return applyEnv(cfg, env)
}

func environMap() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}
