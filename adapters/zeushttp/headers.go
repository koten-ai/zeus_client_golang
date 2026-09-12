// SPDX-License-Identifier: BUSL-1.1

package zeushttp

import (
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
)

// ProductUser is the product stamp (never Hub admin).
const ProductUser = "zeus_client"

// Trace-class values (Python TRACE_CLASS_*). Closed set for Rewind join.
const (
	TraceClassAgent             = "agent"
	TraceClassSession           = "session"
	TraceClassDirectInteractive = "direct.interactive"
	TraceClassDirectRead        = "direct.read"
	TraceClassDirectBatch       = "direct.batch"
)

// ApplyModeHeader sets X-Zeus-Mode when missing (does not overwrite).
func ApplyModeHeader(headers map[string]string, mode string) map[string]string {
	h := copyHeaders(headers)
	m := strings.TrimSpace(mode)
	if m == "" {
		return h
	}
	if !hasFold(h, "X-Zeus-Mode") {
		h["X-Zeus-Mode"] = m
	}
	return h
}

// ApplyForceTraceHeader sets X-Zeus-Trace=1 when force is true.
func ApplyForceTraceHeader(headers map[string]string, force bool) map[string]string {
	h := copyHeaders(headers)
	if force {
		h["X-Zeus-Trace"] = "1"
	}
	return h
}

// Correlation is Rewind / join ids. Never sets X-Zeus-Session or X-Zeus-Req-Id.
type Correlation struct {
	ChatID        string
	TurnID        string
	CallID        string
	Mode          string
	ForceTrace    bool
	TraceClass    string
	ChatSessionID string
	BriefSha12    string
	MiniSha12     string
}

// CorrelationHeaders builds join headers (Python correlation_headers).
func CorrelationHeaders(c Correlation) map[string]string {
	h := map[string]string{}
	if c.ChatID != "" {
		h["X-Zeus-Chat-Id"] = c.ChatID
	}
	if c.TurnID != "" {
		h["X-Zeus-Turn-Id"] = c.TurnID
	}
	if c.CallID != "" {
		h["X-Zeus-Call-Id"] = c.CallID
	}
	if c.Mode != "" {
		h["X-Zeus-Mode"] = c.Mode
	}
	if c.ForceTrace {
		h["X-Zeus-Trace"] = "1"
	}
	if tc := strings.TrimSpace(c.TraceClass); tc != "" {
		h["X-Zeus-Trace-Class"] = tc
	}
	if sid := strings.TrimSpace(c.ChatSessionID); sid != "" {
		h["X-Zeus-Chat-Session-Id"] = sid
	}
	if brief := strings.TrimSpace(c.BriefSha12); brief != "" {
		h["X-Zeus-Brief-Sha12"] = brief
	}
	if mini := strings.TrimSpace(c.MiniSha12); mini != "" {
		h["X-Zeus-Mini-Sha12"] = mini
	}
	return h
}

// StampHeaders is X-Zeus-Client + version. user empty → product zeus_client.
func StampHeaders(user, version string) map[string]string {
	return map[string]string{
		"X-Zeus-Client":         domain.ResolveStampUser(user),
		"X-Zeus-Client-Version": version,
	}
}

// ProductStampHeaders stamps product identity (never secrets).
func ProductStampHeaders(version string) map[string]string {
	return StampHeaders(ProductUser, version)
}

// ReqIDFromHeaders is the full X-Zeus-Req-Id (Python req_id_from_headers).
func ReqIDFromHeaders(headers map[string]string) string {
	return domain.ReqIDFromHeaders(headers)
}

// MergeHeaders copies parts left-to-right (later keys win).
func MergeHeaders(parts ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

// RewindQueryParams returns rewind=true when on; empty when off.
func RewindQueryParams(rewind bool) map[string]string {
	if rewind {
		return map[string]string{"rewind": "true"}
	}
	return map[string]string{}
}

// VerbBodyWithoutRewind drops rewind from verb JSON (query/session flag only).
func VerbBodyWithoutRewind(body map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range body {
		if k == "rewind" {
			continue
		}
		out[k] = v
	}
	return out
}

func copyHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}

func hasFold(h map[string]string, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}
