// SPDX-License-Identifier: BUSL-1.1

package config

// LlmRole is config.llm.roles later-wins (API_CONFIG §7.6).
const (
	LlmRoleOrchestrator = "orchestrator"
	LlmRoleAdvisor      = "advisor"
	LlmRoleWorker       = "worker"
)

// ResolvedLlmSlice is one later-wins LLM pin (no secret values).
type ResolvedLlmSlice struct {
	Role        string
	Model       string
	APIKeyEnv   string
	BaseURL     string
	Provider    string
	Temperature *float64
}

// ToPublicDict is the log/support snapshot (never includes api_key).
func (s ResolvedLlmSlice) ToPublicDict() map[string]any {
	var temp any
	if s.Temperature != nil {
		temp = *s.Temperature
	}
	return map[string]any{
		"role":        s.Role,
		"model":       nilIfEmpty(s.Model),
		"api_key_env": nilIfEmpty(s.APIKeyEnv),
		"base_url":    nilIfEmpty(s.BaseURL),
		"provider":    nilIfEmpty(s.Provider),
		"temperature": temp,
	}
}

// ResolveLlmOpts is resolve_llm_slice kwargs.
type ResolveLlmOpts struct {
	Role      string
	Jobs      *JobsConfig
	JobModels map[string]any
	UnitLLM   map[string]any
	UnitID    string
}

// ResolveLlmSlice is later-wins: default → role → jobs.models.<role> →
// jobs.run.models → unit (workers only).
func ResolveLlmSlice(base LlmProviderConfig, opts ResolveLlmOpts) ResolvedLlmSlice {
	role := opts.Role
	if role == "" {
		role = LlmRoleWorker
	}
	acc := map[string]any{
		"model":       base.Model,
		"api_key_env": base.APIKeyEnv,
		"base_url":    base.BaseURL,
		"provider":    base.Provider,
		"temperature": nil,
	}
	if roleCfg, ok := base.Roles[role]; ok {
		mergeLLMPartial(acc, map[string]any{
			"model":       roleCfg.Model,
			"api_key_env": roleCfg.APIKeyEnv,
			"base_url":    roleCfg.BaseURL,
			"provider":    roleCfg.Provider,
			"temperature": roleCfg.Temperature,
		})
	}
	if opts.Jobs != nil && opts.Jobs.Models != nil {
		if rm, ok := opts.Jobs.Models[role].(map[string]any); ok {
			mergeLLMPartial(acc, rm)
		}
	}
	if opts.JobModels != nil {
		if rm, ok := opts.JobModels[role].(map[string]any); ok {
			mergeLLMPartial(acc, rm)
		}
		if units, ok := opts.JobModels["units"].(map[string]any); ok && opts.UnitID != "" {
			if um, ok := units[opts.UnitID].(map[string]any); ok {
				mergeLLMPartial(acc, um)
			}
		}
	}
	if role == LlmRoleWorker {
		mergeLLMPartial(acc, opts.UnitLLM)
	}
	out := ResolvedLlmSlice{
		Role:      role,
		Model:     asLLMString(acc["model"]),
		APIKeyEnv: asLLMString(acc["api_key_env"]),
		BaseURL:   asLLMString(acc["base_url"]),
		Provider:  asLLMString(acc["provider"]),
	}
	if t, ok := acc["temperature"].(*float64); ok && t != nil {
		v := *t
		out.Temperature = &v
	} else if f, ok := acc["temperature"].(float64); ok {
		out.Temperature = &f
	}
	return out
}

func mergeLLMPartial(dst map[string]any, src map[string]any) {
	if src == nil {
		return
	}
	for _, key := range []string{"model", "api_key_env", "base_url", "provider", "temperature"} {
		v, ok := src[key]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" && key != "temperature" {
			continue
		}
		dst[key] = v
	}
}

func asLLMString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
