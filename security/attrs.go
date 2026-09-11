// SPDX-License-Identifier: BUSL-1.1

package security

import (
	"strings"
	"unicode/utf8"
)

// hardAttrKeys is the logging-boundary hard-deny set (Python _HARD_KEYS).
// Substring match after '.' / '-' → '_'. Secrets stay denied when redact=false.
var hardAttrKeys = []string{
	"authorization",
	"password",
	"passwd",
	"secret",
	"api_key",
	"apikey",
	"token",
	"refresh_token",
	"private_key",
	"cookie",
}

// RedactAttrs copies attrs for a log / journal / JobEvent boundary.
//
// When redact is false (lab / break-glass), free-text truncation is relaxed
// but hard-deny keys and JSONValue secret walks still apply. There is no
// off-switch for secrets (LOGGING.md §5.3; Python redact_attrs).
func RedactAttrs(attrs map[string]any, redact bool) map[string]any {
	return RedactAttrsWith(attrs, redact, New(), PreviewMaxChars)
}

// RedactAttrsWith is RedactAttrs with an explicit redactor and preview cap.
func RedactAttrsWith(attrs map[string]any, redact bool, red Redactor, previewMaxChars int) map[string]any {
	if red == nil {
		red = New()
	}
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if v == nil {
			continue
		}
		if isHardAttrKey(k) {
			out[k] = Redacted
			continue
		}
		walked := red.JSONValue(v)
		s, isStr := walked.(string)
		switch {
		case redact && isStr && previewMaxChars >= 0:
			out[k] = red.Text(s, previewMaxChars)
		case isStr && utf8.RuneCountInString(s) > loudMaxChars:
			out[k] = truncateRunes(s, loudMaxChars)
		default:
			out[k] = walked
		}
	}
	return out
}

func isHardAttrKey(key string) bool {
	lk := strings.ToLower(key)
	lk = strings.ReplaceAll(lk, ".", "_")
	lk = strings.ReplaceAll(lk, "-", "_")
	for _, h := range hardAttrKeys {
		if lk == h || strings.Contains(lk, h) {
			return true
		}
	}
	return false
}
