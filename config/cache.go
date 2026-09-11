// SPDX-License-Identifier: BUSL-1.1

package config

import "strings"

var blockTypes = map[string]struct{}{
	"conversational": {},
	"profile":        {},
	"semantic":       {},
}

var blockTypeOrder = []string{"conversational", "profile", "semantic"}

const defaultInjectKey = "semantic_memory"

func parseSemanticCache(raw any) SemanticCacheConfig {
	cfg := defaultSemanticCache()
	if raw == nil {
		return cfg
	}
	if b, ok := raw.(bool); ok {
		if b {
			cfg.Enabled = true
		}
		return cfg
	}
	data := asMap(raw)
	if len(data) == 0 {
		return cfg
	}
	recallRaw := asMap(data["recall"])
	injectRaw := asMap(data["inject"])
	writeRaw := asMap(data["write"])
	embedRaw := asMap(data["embed"])
	privacyRaw := asMap(data["privacy"])

	typesDefault := []string{"profile", "semantic", "conversational"}
	recallTypes := filterBlockTypes(asStringSlice(recallRaw["types"], typesDefault), typesDefault)
	allowed := filterBlockTypes(asStringSlice(writeRaw["types_allowed"], blockTypeOrder), blockTypeOrder)

	asyncRaw := writeRaw["async"]
	if asyncRaw == nil {
		asyncRaw = writeRaw["async_write"]
	}
	order := strings.ToLower(strings.TrimSpace(asString(injectRaw["order"], "score_desc")))
	if order != "score_desc" && order != "recency" {
		order = "score_desc"
	}
	bag := strings.TrimSpace(asString(injectRaw["bag"], "B"))
	if bag == "" {
		bag = "B"
	}
	key := strings.TrimSpace(asString(injectRaw["key"], defaultInjectKey))
	if key == "" {
		key = defaultInjectKey
	}
	defaultType := strings.ToLower(strings.TrimSpace(asString(writeRaw["default_type"], "conversational")))
	if _, ok := blockTypes[defaultType]; !ok {
		defaultType = "conversational"
	}

	cfg.Enabled = asBool(data["enabled"], false)
	cfg.ApplyToModes = asStringSlice(data["apply_to_modes"], []string{"agent"})
	cfg.Recall = SemanticCacheRecallConfig{
		Enabled:       asBool(recallRaw["enabled"], true),
		TopK:          asInt(recallRaw["top_k"], 5),
		MinScore:      asFloat(recallRaw["min_score"], 0),
		Types:         recallTypes,
		MinQueryChars: asInt(recallRaw["min_query_chars"], 12),
		TimeoutMS:     asInt(recallRaw["timeout_ms"], 150),
		FailClosed:    asBool(recallRaw["fail_closed"], false),
	}
	cfg.Inject = SemanticCacheInjectConfig{
		Bag:           bag,
		Key:           key,
		MaxChars:      asInt(injectRaw["max_chars"], 4000),
		MaxBlocks:     asInt(injectRaw["max_blocks"], 5),
		IncludeFields: asStringSlice(injectRaw["include_fields"], []string{"summary", "text"}),
		Order:         order,
	}
	cfg.Write = SemanticCacheWriteConfig{
		Enabled:               asBool(writeRaw["enabled"], true),
		OnTurnCommit:          asBool(writeRaw["on_turn_commit"], true),
		WriteExplicitOnly:     asBool(writeRaw["write_explicit_only"], true),
		WriteUserMessage:      asBool(writeRaw["write_user_message"], false),
		WriteAssistantSummary: asBool(writeRaw["write_assistant_summary"], false),
		DefaultType:           defaultType,
		TypesAllowed:          allowed,
		MinChars:              asInt(writeRaw["min_chars"], 24),
		MaxCharsPerBlock:      asInt(writeRaw["max_chars_per_block"], 2000),
		MaxBlocksPerTurn:      asInt(writeRaw["max_blocks_per_turn"], 3),
		TTLSeconds:            ttlSecondsFrom(writeRaw["ttl_seconds"], 604800),
		AsyncWrite:            asBool(asyncRaw, true),
	}
	cfg.Embed = SemanticCacheEmbedConfig{
		PreferSummaryForWrite: asBool(embedRaw["prefer_summary_for_write"], true),
	}
	cfg.Privacy = SemanticCachePrivacyConfig{
		RedactBeforeWrite: asBool(privacyRaw["redact_before_write"], false),
		DenyRegex:         asStringSliceAllowEmpty(privacyRaw["deny_regex"]),
	}
	if uid := data["dev_user_id"]; uid != nil {
		s := strings.TrimSpace(asString(uid, ""))
		if s != "" {
			cfg.DevUserID = s
		}
	}
	return cfg
}

func ttlSecondsFrom(raw any, def int) int {
	if raw == nil {
		return def
	}
	if m, ok := raw.(map[string]any); ok {
		for _, key := range []string{"conversational", "default", "profile", "semantic"} {
			if v, exists := m[key]; exists && v != nil {
				n := asInt(v, -1)
				if n >= 0 {
					return n
				}
			}
		}
		return def
	}
	n := asInt(raw, def)
	if n < 0 {
		return 0
	}
	return n
}

func isMap(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func asStringSlice(raw any, def []string) []string {
	if raw == nil {
		return copyStrings(def)
	}
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return copyStrings(def)
		}
		return []string{s}
	}
	if arr, ok := raw.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, x := range arr {
			s := strings.TrimSpace(asString(x, ""))
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return copyStrings(def)
		}
		return out
	}
	return copyStrings(def)
}

func asStringSliceAllowEmpty(raw any) []string {
	if raw == nil {
		return nil
	}
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		return []string{s}
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if x == nil {
			continue
		}
		out = append(out, asString(x, ""))
	}
	return out
}

func filterBlockTypes(in, fallback []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		if _, ok := blockTypes[t]; ok {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return copyStrings(fallback)
	}
	return out
}
