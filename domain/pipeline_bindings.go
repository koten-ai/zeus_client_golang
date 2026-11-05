// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"regexp"
	"sort"
	"strings"
)

// passthroughVerbs copy source keys onto doc_keys and leave node_ids empty.
// A following @that.ids is the same miss as an FTS @step.ids.
var passthroughVerbs = map[string]struct{}{
	"order": {},
	"set":   {},
}

// RewriteFTSIdsToDocKeys returns a pipeline body whose later steps bind FTS
// hits as doc_keys. @step.ids is node ids only. An fts search leaves that list
// empty, so a later project of @step.ids returns no rows.
//
// Hybrid, vector, and find steps are left alone. @step.node_ids and an
// existing @step.doc_keys binding are left alone. The summary is not rewritten.
// The returned names are the steps whose @step.ids token was rewritten, in
// encounter order. When nothing changes, body is returned as given.
func RewriteFTSIdsToDocKeys(body map[string]any) (map[string]any, []string) {
	if body == nil {
		return map[string]any{}, nil
	}
	rawSteps, ok := body["steps"].([]any)
	if !ok {
		return body, nil
	}
	producers := map[string]struct{}{}
	for _, name := range ftsStepNames(rawSteps) {
		producers[name] = struct{}{}
	}
	if len(producers) == 0 {
		return body, nil
	}

	var changed []string
	seen := map[string]struct{}{}
	newSteps := make([]any, 0, len(rawSteps))
	for _, step := range rawSteps {
		pattern := producerPattern(producers)
		sm, isMap := step.(map[string]any)
		if !isMap || pattern == nil {
			newSteps = append(newSteps, step)
			continue
		}
		rewritten, _ := rewriteValue(cloneMap(sm), pattern, &changed, seen).(map[string]any)
		newSteps = append(newSteps, rewritten)
		verb := strings.ToLower(strings.TrimSpace(bindingString(sm["verb"])))
		name := strings.TrimSpace(bindingString(sm["as"]))
		if _, pass := passthroughVerbs[verb]; pass && name != "" {
			blob := textBlob(rewritten)
			for producer := range producers {
				if strings.Contains(blob, "@"+producer+".doc_keys") {
					producers[name] = struct{}{}
					break
				}
			}
		}
	}
	if len(changed) == 0 {
		return body, nil
	}
	out := cloneMap(body)
	out["steps"] = newSteps
	return out, changed
}

func ftsStepNames(steps []any) []string {
	var names []string
	seen := map[string]struct{}{}
	for _, step := range steps {
		sm, ok := step.(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(bindingString(sm["verb"]))) != "search" {
			continue
		}
		if strategyOf(sm) != "fts" {
			continue
		}
		name := strings.TrimSpace(bindingString(sm["as"]))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func strategyOf(step map[string]any) string {
	strategy := strings.TrimSpace(bindingString(step["strategy"]))
	if strategy == "" {
		if params, ok := step["params"].(map[string]any); ok {
			strategy = strings.TrimSpace(bindingString(params["strategy"]))
		}
	}
	return strings.ToLower(strategy)
}

func producerPattern(producers map[string]struct{}) *regexp.Regexp {
	if len(producers) == 0 {
		return nil
	}
	names := make([]string, 0, len(producers))
	for name := range producers {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString(`@(`)
	for i, name := range names {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(regexp.QuoteMeta(name))
	}
	b.WriteString(`)\.ids\b`)
	return regexp.MustCompile(b.String())
}

func rewriteValue(value any, pattern *regexp.Regexp, changed *[]string, seen map[string]struct{}) any {
	switch v := value.(type) {
	case string:
		return pattern.ReplaceAllStringFunc(v, func(match string) string {
			sub := pattern.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			step := sub[1]
			if _, ok := seen[step]; !ok {
				seen[step] = struct{}{}
				*changed = append(*changed, step)
			}
			return "@" + step + ".doc_keys"
		})
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = rewriteValue(item, pattern, changed, seen)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = rewriteValue(item, pattern, changed, seen)
		}
		return out
	default:
		return value
	}
}

func textBlob(step map[string]any) string {
	var parts []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case string:
			parts = append(parts, v)
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(step)
	return strings.Join(parts, "\n")
}

func bindingString(v any) string {
	s, _ := v.(string)
	return s
}
