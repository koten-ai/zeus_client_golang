// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const (
	typeaheadComponent = "application.typeahead"

	DefaultSuggestLimit    = 8
	MinQueryLen            = 2
	DefaultFTSTimeoutMS    = 2000
	DefaultSuggestEntity   = "Business"
	defaultSuggestStrategy = "fts"
	defaultSuggestMode     = "analytics"
)

// SuggestHit is one typeahead row (Python SuggestHit). Direct only — never agent.
type SuggestHit struct {
	ID          string
	Name        string
	Subtitle    string
	City        string
	State       string
	Stars       *float64
	ReviewCount *int
	Categories  string
	Address     string
	Score       *float64
	Source      string
}

// Map is SuggestHit.to_dict.
func (h SuggestHit) Map() map[string]any {
	return map[string]any{
		"id":           h.ID,
		"name":         h.Name,
		"subtitle":     h.Subtitle,
		"city":         h.City,
		"state":        h.State,
		"stars":        h.Stars,
		"review_count": h.ReviewCount,
		"categories":   h.Categories,
		"address":      h.Address,
		"score":        h.Score,
		"source":       h.Source,
	}
}

// SuggestResult is the typeahead product outcome (Python SuggestResult).
type SuggestResult struct {
	Query           string
	Hits            []SuggestHit
	Count           int
	Source          string
	Sources         []string
	FTSReqID        string
	Error           string
	FastTier        bool
	AIProcessResult bool
	ReqIDs          []string
	TraceClass      string
}

// Map is SuggestResult.to_dict.
func (r SuggestResult) Map() map[string]any {
	hits := make([]map[string]any, 0, len(r.Hits))
	for _, h := range r.Hits {
		hits = append(hits, h.Map())
	}
	return map[string]any{
		"query":             r.Query,
		"results":           hits,
		"hits":              hits,
		"count":             r.Count,
		"source":            r.Source,
		"sources":           copyStrings(r.Sources),
		"fts_req_id":        nilIfEmpty(r.FTSReqID),
		"error":             nilIfEmpty(r.Error),
		"fast_tier":         r.FastTier,
		"ai_process_result": r.AIProcessResult,
		"req_ids":           copyStrings(r.ReqIDs),
		"trace_class":       r.TraceClass,
	}
}

// SuggestOptions is typeahead knobs (Python SuggestOptions).
type SuggestOptions struct {
	EntityType     string
	Limit          int
	MinQueryLen    int
	FTSTimeoutMS   int
	Strategy       string
	ModeHeader     string
	UseN1QLHydrate bool
}

func (o SuggestOptions) withDefaults() SuggestOptions {
	if o.EntityType == "" {
		o.EntityType = DefaultSuggestEntity
	}
	if o.Limit <= 0 {
		o.Limit = DefaultSuggestLimit
	}
	if o.MinQueryLen <= 0 {
		o.MinQueryLen = MinQueryLen
	}
	if o.FTSTimeoutMS <= 0 {
		o.FTSTimeoutMS = DefaultFTSTimeoutMS
	}
	if o.Strategy == "" {
		o.Strategy = defaultSuggestStrategy
	}
	if o.ModeHeader == "" {
		o.ModeHeader = defaultSuggestMode
	}
	return o
}

// MergeHits dedupes by normalized id then name; first-seen order (FTS first).
func MergeHits(limit int, groups ...[]SuggestHit) []SuggestHit {
	if limit <= 0 {
		return nil
	}
	seenIDs := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	out := make([]SuggestHit, 0, limit)
	for _, group := range groups {
		for _, hit := range group {
			nid := normID(hit.ID)
			nname := strings.ToLower(strings.TrimSpace(hit.Name))
			if nid != "" {
				if _, ok := seenIDs[nid]; ok {
					continue
				}
			}
			if nname != "" {
				if _, ok := seenNames[nname]; ok {
					continue
				}
			}
			if nid != "" {
				seenIDs[nid] = struct{}{}
			}
			if nname != "" {
				seenNames[nname] = struct{}{}
			}
			out = append(out, hit)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func normID(value string) string {
	s := strings.ToLower(strings.TrimSpace(value))
	s = strings.TrimPrefix(s, "biz:")
	return s
}

func resultItems(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}
	result, _ := payload["result"].(map[string]any)
	if result == nil {
		return nil
	}
	items, _ := result["items"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// SrcKeysFromFTSPayload extracts src_keys (or item ids) from an FTS search body.
func SrcKeysFromFTSPayload(payload map[string]any) []string {
	var result map[string]any
	if payload != nil {
		result, _ = payload["result"].(map[string]any)
	}
	out := []string{}
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if result != nil {
		if keys, ok := result["src_keys"].([]any); ok {
			for _, k := range keys {
				add(asString(k))
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, item := range resultItems(payload) {
		node, _ := item["node"].(map[string]any)
		if node == nil {
			node = item
		}
		for _, key := range []string{"doc_key", "source", "id", "key", "name"} {
			s := asString(node[key])
			if s != "" && key != "name" {
				add(s)
				break
			}
		}
	}
	return out
}

func hitsFromFTS(payload map[string]any, source string) []SuggestHit {
	hits := []SuggestHit{}
	for _, item := range resultItems(payload) {
		node, _ := item["node"].(map[string]any)
		if node == nil {
			node = item
		}
		hid := ""
		for _, key := range []string{"doc_key", "source", "id", "key"} {
			hid = strings.TrimSpace(asString(node[key]))
			if hid != "" {
				break
			}
		}
		name := strings.TrimSpace(asString(node["name"]))
		if name == "" {
			name = hid
		}
		if hid == "" {
			hid = name
		}
		var score *float64
		if item["score"] != nil {
			if f, ok := asFloat(item["score"]); ok {
				score = &f
			}
		}
		hits = append(hits, SuggestHit{
			ID:       hid,
			Name:     name,
			City:     asString(node["city"]),
			State:    asString(node["state"]),
			Subtitle: firstNonEmpty(asString(node["city"]), asString(node["categories"])),
			Score:    score,
			Source:   source,
		})
	}
	if len(hits) > 0 {
		return hits
	}
	for _, k := range SrcKeysFromFTSPayload(payload) {
		hits = append(hits, SuggestHit{ID: k, Name: k, Source: source})
	}
	return hits
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	s := fmt.Sprint(v)
	if s == "<nil>" {
		return ""
	}
	return s
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// RunTypeaheadSearch is FTS typeahead via Direct ZeusPort. Never agent.Run.
// Empty / short query → empty hits (no HTTP).
func RunTypeaheadSearch(ctx context.Context, zeus ports.ZeusPort, query string, target config.DataTarget, options SuggestOptions, rewind bool) (SuggestResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return SuggestResult{}, de
		}
		return SuggestResult{}, err
	}
	opts := options.withDefaults()
	q := strings.TrimSpace(query)
	empty := SuggestResult{
		Query:      q,
		Hits:       []SuggestHit{},
		Count:      0,
		Source:     "none",
		Sources:    []string{},
		FastTier:   true,
		TraceClass: zeushttp.TraceClassDirectInteractive,
	}
	if len(q) < opts.MinQueryLen {
		return empty, nil
	}
	if zeus == nil {
		return SuggestResult{}, domain.New(domain.CodeNotImplemented, typeaheadComponent,
			domain.WithMessage("Zeus port not wired on runtime"))
	}
	hop, err := zeus.CallVerb(ctx, ports.VerbRequest{
		Verb: "search",
		Body: map[string]any{
			"entity_type": opts.EntityType,
			"strategy":    opts.Strategy,
			"query_text":  q,
			"limit":       opts.Limit,
			"timeout_ms":  opts.FTSTimeoutMS,
		},
		Target:     target,
		ModeHeader: opts.ModeHeader,
		Headers: zeushttp.CorrelationHeaders(zeushttp.Correlation{
			TraceClass: zeushttp.TraceClassDirectInteractive,
		}),
		Rewind: rewind,
	})
	if err != nil {
		return SuggestResult{}, err
	}
	if !hop.OK {
		errMsg := hop.Error
		if errMsg == "" {
			errMsg = "status " + strconv.Itoa(hop.StatusCode)
		}
		return SuggestResult{
			Query:      q,
			Hits:       []SuggestHit{},
			Count:      0,
			Source:     "error",
			Sources:    []string{},
			FTSReqID:   hop.ReqID,
			Error:      errMsg,
			FastTier:   true,
			TraceClass: zeushttp.TraceClassDirectInteractive,
		}, nil
	}
	ftsHits := hitsFromFTS(hop.Body, "fts")
	merged := MergeHits(opts.Limit, ftsHits)
	source := "empty"
	sources := []string{}
	if len(merged) > 0 {
		source = "fts"
		sources = []string{"fts"}
	}
	reqIDs := []string{}
	if hop.ReqID != "" {
		reqIDs = []string{hop.ReqID}
	}
	if merged == nil {
		merged = []SuggestHit{}
	}
	return SuggestResult{
		Query:      q,
		Hits:       merged,
		Count:      len(merged),
		Source:     source,
		Sources:    sources,
		FTSReqID:   hop.ReqID,
		FastTier:   true,
		ReqIDs:     reqIDs,
		TraceClass: zeushttp.TraceClassDirectInteractive,
	}, nil
}
