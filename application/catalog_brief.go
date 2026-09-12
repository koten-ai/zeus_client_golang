// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
)

// ChatRequestFetch borrows a live chat_request.json (bucket, scope, mode).
// Optional — missing remote skips the merge (Python catalog_remote).
type ChatRequestFetch func(ctx context.Context, bucket, scope, mode string) (map[string]any, error)

// ScopeBriefResult is ensure_scope_brief output (Python ScopeBriefResult).
type ScopeBriefResult struct {
	Body   map[string]any
	Note   string
	Merged bool
	ReqID  string
}

var knownLiveModes = map[string]struct{}{
	"analytics": {}, "auto": {}, "code": {}, "custom": {}, "fraud": {},
	"open": {}, "private": {}, "prototype": {}, "regulated": {},
	"research": {}, "tenant": {},
}

var skipV2Family = map[string]struct{}{
	"": {}, "mode": {}, "chat_request": {}, "default": {},
}

// LiveModeCandidates is the live /v1/ai/chat_request.json mode try-list.
func LiveModeCandidates(mode, lineageMode string) []string {
	m := strings.TrimSpace(mode)
	if m == "" {
		m = "analytics"
	}
	var out []string
	add := func(name string) {
		n := strings.TrimSpace(name)
		if n == "" {
			return
		}
		for _, existing := range out {
			if existing == n {
				return
			}
		}
		out = append(out, n)
	}
	add(m)
	add(lineageMode)
	if i := strings.Index(m, "_v2_"); i >= 0 {
		family := strings.TrimSpace(m[:i])
		if _, skip := skipV2Family[family]; !skip {
			add(family)
		}
	}
	for _, token := range strings.Split(m, "_") {
		if _, ok := knownLiveModes[token]; ok {
			add(token)
		}
	}
	add("analytics")
	return out
}

func lineageModeFromCatalog(chatReq map[string]any) string {
	if chatReq == nil {
		return ""
	}
	for _, key := range []string{"_lineage", "_base_meta"} {
		raw, _ := chatReq[key].(map[string]any)
		if raw == nil {
			continue
		}
		if m, _ := raw["mode"].(string); strings.TrimSpace(m) != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

// EnsureScopeBrief merges a live ## SCOPE BRIEF when the catalog has none.
// Never raises — fetch errors leave the disk catalog unchanged.
func EnsureScopeBrief(ctx context.Context, chatReq map[string]any, fetch ChatRequestFetch, bucket, scope, mode string) ScopeBriefResult {
	body := cloneAnyMap(chatReq)
	if domain.ExtractScopeBrief(body) != "" {
		return ScopeBriefResult{Body: body, Note: "scope_brief: already present"}
	}
	if fetch == nil {
		return ScopeBriefResult{Body: body, Note: "scope_brief: live fetch skipped (no catalog_remote)"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lastErr := ""
	sawEmpty := false
	for _, candidate := range LiveModeCandidates(mode, lineageModeFromCatalog(body)) {
		live, err := fetch(ctx, bucket, scope, candidate)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		liveDoc := cloneAnyMap(live)
		brief := domain.ExtractScopeBrief(liveDoc)
		if brief == "" {
			sawEmpty = true
			continue
		}
		note := "scope_brief: live merge"
		if candidate != strings.TrimSpace(mode) {
			note = "scope_brief: live merge (mode=" + candidate + ")"
		}
		return ScopeBriefResult{
			Body:   domain.MergeScopeBrief(body, brief),
			Note:   note,
			Merged: true,
		}
	}
	if lastErr != "" && !sawEmpty {
		return ScopeBriefResult{Body: body, Note: "scope_brief: live fetch failed: " + lastErr}
	}
	return ScopeBriefResult{Body: body, Note: "scope_brief: live response had no SCOPE BRIEF"}
}
