// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

func asBool(value any, def bool) bool {
	if value == nil {
		return def
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return i != 0
		}
		return def
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return def
	}
}

func asString(value any, def string) string {
	if value == nil {
		return def
	}
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return def
	}
}

func asInt(value any, def int) int {
	if value == nil {
		return def
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return def
		}
		return int(i)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return def
		}
		return i
	default:
		return def
	}
}

func asFloat(value any, def float64) float64 {
	if value == nil {
		return def
	}
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return def
		}
		return f
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return def
		}
		return f
	default:
		return def
	}
}

func asMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func childMap(data map[string]any, key string) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	return asMap(data[key])
}

func rstripSlash(s string) string {
	return strings.TrimRight(s, "/")
}

func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyBoolMap(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func asStringMap(raw any) map[string]string {
	m := asMap(raw)
	out := make(map[string]string, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		out[k] = asString(v, "")
	}
	return out
}

func asBoolMap(raw any) map[string]bool {
	m := asMap(raw)
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = asBool(v, false)
	}
	return out
}

func envHas(env map[string]string, key string) bool {
	_, ok := env[key]
	return ok
}

func envOr(env map[string]string, key, def string) string {
	if v, ok := env[key]; ok {
		return v
	}
	return def
}

func envBool(env map[string]string, key string, def bool) bool {
	v, ok := env[key]
	if !ok {
		return def
	}
	return asBool(v, def)
}
