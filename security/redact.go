// SPDX-License-Identifier: BUSL-1.1

package security

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Redacted is the marker string substituted for secrets (Python REDACTED).
const Redacted = "[REDACTED]"

// PreviewMaxChars is the default free-text cap when REDACT=true (Python _MAX_STR).
const PreviewMaxChars = 2048

// loudMaxChars is the emergency cap when REDACT=false (Python 16_384).
const loudMaxChars = 16384

// Header names compared case-insensitively (Python SENSITIVE_HEADER_NAMES).
var sensitiveHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"x-api-key":           {},
	"api-key":             {},
	"cookie":              {},
	"set-cookie":          {},
	"x-auth-token":        {},
	"x-access-token":      {},
}

// SensitiveKeyRE matches JSON object keys whose values must be redacted
// (Python SENSITIVE_KEY_RE). Unanchored, case-insensitive.
var SensitiveKeyRE = regexp.MustCompile(
	`(?i)(password|passwd|secret|api[_-]?key|token|authorization|auth|credential|credit[_-]?card|ssn|private[_-]?key)`,
)

var (
	bearerRE = regexp.MustCompile(`(?i)\b(authorization\s*:\s*)?(bearer|basic)\s+\S+`)
	skRE     = regexp.MustCompile(`(?i)\b(sk-[a-z0-9\-_]{8,}|xai-[a-z0-9\-_]{8,})\b`)
)

// Redactor strips secrets at journal / log / export boundaries.
type Redactor interface {
	Headers(h map[string]string) map[string]string
	JSONValue(v any) any
	Text(s string, maxChars int) string
}

// DefaultRedactor is the production redactor. Hard-deny secrets cannot be
// switched off (LOGGING.md §5.3).
type DefaultRedactor struct{}

var _ Redactor = (*DefaultRedactor)(nil)

// New returns the default redactor (Python default_redactor).
func New() *DefaultRedactor {
	return &DefaultRedactor{}
}

// IsSensitiveHeader reports whether a header name is in the hard-deny set.
func IsSensitiveHeader(name string) bool {
	_, ok := sensitiveHeaderNames[strings.ToLower(name)]
	return ok
}

// Headers copies h, replacing sensitive header values with [REDACTED].
// Original key casing is preserved. Nil h yields an empty map.
func (r *DefaultRedactor) Headers(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if IsSensitiveHeader(k) {
			out[k] = Redacted
		} else {
			out[k] = v
		}
	}
	return out
}

// JSONValue walks nested maps and slices. Keys matching SensitiveKeyRE have
// their values replaced with [REDACTED]. String leaves that contain Bearer /
// Basic or sk-… / xai-… tokens become [REDACTED] wholesale. Input is not mutated.
func (r *DefaultRedactor) JSONValue(v any) any {
	return walkJSON(v)
}

// Text scrubs Bearer/Basic and sk-… / xai-… tokens, then truncates when
// maxChars >= 0 and the result is longer. maxChars == 0 yields "…".
func (r *DefaultRedactor) Text(s string, maxChars int) string {
	s = bearerRE.ReplaceAllString(s, Redacted)
	s = skRE.ReplaceAllString(s, Redacted)
	return truncateRunes(s, maxChars)
}

func walkJSON(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, child := range x {
			if SensitiveKeyRE.MatchString(k) {
				out[k] = Redacted
			} else {
				out[k] = walkJSON(child)
			}
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, child := range x {
			if SensitiveKeyRE.MatchString(k) {
				out[k] = Redacted
			} else {
				out[k] = walkJSON(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = walkJSON(item)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = walkJSON(item)
		}
		return out
	case []byte:
		return walkJSON(string(x))
	case string:
		if bearerRE.MatchString(x) || skRE.MatchString(x) {
			return Redacted
		}
		return x
	default:
		return v
	}
}

func truncateRunes(s string, maxChars int) string {
	if maxChars < 0 {
		return s
	}
	n := utf8.RuneCountInString(s)
	if n <= maxChars {
		return s
	}
	if maxChars == 0 {
		return "…"
	}
	return string([]rune(s)[:maxChars]) + "…"
}
