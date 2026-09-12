// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GetPath walks a dotted JSON-path on nested objects. Missing → nil.
func GetPath(obj any, dotted string) any {
	cur := obj
	for _, part := range strings.Split(dotted, ".") {
		m, ok := cur.(map[string]any)
		if !ok || m == nil {
			return nil
		}
		if _, ok := m[part]; !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

// AssertExpects returns diff messages (empty = pass). Unknown keys still fail.
func AssertExpects(observe, expect map[string]any) []string {
	var diffs []string
	if expect == nil {
		return diffs
	}
	if observe == nil {
		observe = map[string]any{}
	}
	for key, want := range expect {
		if strings.HasSuffix(key, "_gte") && isNumber(want) {
			base := strings.TrimSuffix(key, "_gte")
			got := observe[key]
			if got == nil {
				got = observe[base]
			}
			if got == nil {
				got = GetPath(observe, base)
			}
			gf, gok := asFloat(got)
			wf, wok := asFloat(want)
			if !gok || !wok {
				diffs = append(diffs, fmt.Sprintf("%s: got %#v not comparable to %#v", key, got, want))
				continue
			}
			if gf < wf {
				diffs = append(diffs, fmt.Sprintf("%s: got %#v < %#v", key, got, want))
			}
			continue
		}
		var got any
		if _, ok := observe[key]; ok {
			got = observe[key]
		} else {
			got = GetPath(observe, key)
		}
		if !valuesEqual(got, want) {
			diffs = append(diffs, fmt.Sprintf("%s: got %#v want %#v", key, got, want))
		}
	}
	return diffs
}

// AssertCase applies ADAPTER_CONTRACT expects plus Python _contains keys.
func AssertCase(observe, expect map[string]any) []string {
	var diffs []string
	rest := map[string]any{}
	if expect == nil {
		return diffs
	}
	if observe == nil {
		observe = map[string]any{}
	}
	for key, want := range expect {
		if strings.HasSuffix(key, "_contains") {
			if s, ok := want.(string); ok {
				hay := asString(observe[key])
				if !strings.Contains(hay, s) {
					clip := hay
					if len(clip) > 120 {
						clip = clip[:120]
					}
					diffs = append(diffs, fmt.Sprintf("%s: missing %#v in %#v", key, s, clip))
				}
				continue
			}
		}
		rest[key] = want
	}
	diffs = append(diffs, AssertExpects(observe, rest)...)
	return diffs
}

func isNumber(v any) bool {
	_, ok := asFloat(v)
	return ok
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func asInt(v any) int {
	if f, ok := asFloat(v); ok {
		return int(f)
	}
	if s, ok := v.(string); ok {
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return 0
}

func valuesEqual(got, want any) bool {
	if got == nil && want == nil {
		return true
	}
	if gf, ok := asFloat(got); ok {
		if wf, ok := asFloat(want); ok {
			return gf == wf
		}
	}
	switch w := want.(type) {
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case string:
		g, ok := got.(string)
		return ok && g == w
	}
	if g, ok := got.(bool); ok {
		if w, ok := want.(bool); ok {
			return g == w
		}
	}
	return false
}

func asBool(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	default:
		if f, ok := asFloat(v); ok {
			return f != 0
		}
		return v != nil
	}
}
