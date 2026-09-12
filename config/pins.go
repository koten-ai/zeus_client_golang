// SPDX-License-Identifier: BUSL-1.1

package config

import "strings"

func isPinsMapping(data map[string]any) bool {
	if data == nil {
		return false
	}
	if _, ok := data["pins_version"]; ok {
		return true
	}
	_, lang := data["language"]
	_, claim := data["claim"]
	return lang && claim
}

// overlayPins copies bootstrap pin fields onto defaults. Pins hold env names, never keys.
func overlayPins(base RuntimeConfig, data map[string]any) RuntimeConfig {
	if z := childMap(data, "zeus"); len(z) > 0 {
		if u := asString(z["default_base_url"], ""); u != "" {
			base.Zeus.URL = rstripSlash(u)
		}
	}
	if a := childMap(data, "auth_smoke"); len(a) > 0 {
		if m := strings.ToLower(asString(a["mode"], "")); m != "" {
			base.Zeus.AuthMode = AuthMode(m)
		}
	}
	if llm := childMap(data, "llm"); len(llm) > 0 {
		if v := asString(llm["provider"], ""); v != "" {
			base.LLM.Provider = v
		}
		if v := asString(llm["base_url"], ""); v != "" {
			base.LLM.BaseURL = rstripSlash(v)
		}
		if v := asString(llm["model"], ""); v != "" {
			base.LLM.Model = v
		}
		if v := asString(llm["api_key_env"], ""); v != "" {
			base.LLM.APIKeyEnv = v
		}
		if v := asString(llm["api_style"], ""); v != "" {
			base.LLM.APIStyle = v
		}
		if llm["context_window_tokens"] != nil {
			base.LLM.ContextWindowTokens = asInt(llm["context_window_tokens"], base.LLM.ContextWindowTokens)
		}
		if llm["context_soft_limit"] != nil {
			base.LLM.ContextSoftLimit = asFloat(llm["context_soft_limit"], base.LLM.ContextSoftLimit)
		}
		if llm["ai_process_result_default"] != nil {
			base.Settings.AIProcessResult = asBool(llm["ai_process_result_default"], false)
		}
	}
	if claim := childMap(data, "claim"); len(claim) > 0 {
		if v := asString(claim["client_floor"], ""); v != "" {
			base.ClientFloor = v
		}
	}
	if cat := childMap(data, "catalog"); len(cat) > 0 {
		base.ProductionBaseID = asString(cat["production_base_id"], "")
	}
	if stamps := childMap(data, "stamps"); len(stamps) > 0 {
		if v := asString(stamps["user"], ""); v != "" {
			base.User = v
		}
	}
	return base
}
