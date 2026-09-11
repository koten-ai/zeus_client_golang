// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func TestMergeHitsDedupesIDAndNameFTSFirst(t *testing.T) {
	a := SuggestHit{ID: "biz:1", Name: "Sushi Place", Source: "fts"}
	b := SuggestHit{ID: "1", Name: "Other", Source: "find"}
	c := SuggestHit{ID: "2", Name: "sushi place", Source: "find"}
	d := SuggestHit{ID: "3", Name: "Ramen", Source: "find"}
	out := MergeHits(10, []SuggestHit{a}, []SuggestHit{b, c, d})
	if len(out) != 2 || out[0].ID != "biz:1" || out[1].ID != "3" {
		t.Fatalf("%+v", out)
	}
}

func TestSrcKeysFromFTSPayload(t *testing.T) {
	keys := SrcKeysFromFTSPayload(map[string]any{
		"result": map[string]any{"src_keys": []any{"biz:a", "biz:b", "biz:a"}},
	})
	if len(keys) != 2 || keys[0] != "biz:a" || keys[1] != "biz:b" {
		t.Fatalf("%v", keys)
	}
}

func TestTypeaheadShortQuerySkipsNetwork(t *testing.T) {
	z := &recordingZeus{}
	r, err := RunTypeaheadSearch(context.Background(), z, "a", config.DataTarget{}, SuggestOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 0 || r.Source != "none" {
		t.Fatalf("%+v", r)
	}
	if z.n.Load() != 0 {
		t.Fatal("short query must not call Zeus")
	}
}

func TestTypeaheadUsesSearchNotAgent(t *testing.T) {
	z := &recordingZeus{
		hop: ports.VerbHopResult{
			OK:         true,
			StatusCode: 200,
			ReqID:      "fts-1",
			Body: map[string]any{
				"result": map[string]any{
					"src_keys": []any{"biz:1"},
					"items": []any{
						map[string]any{
							"score": 1.5,
							"node":  map[string]any{"doc_key": "biz:1", "name": "Sushi", "city": "SF"},
						},
					},
				},
			},
		},
	}
	r, err := RunTypeaheadSearch(context.Background(), z, "sushi", config.DataTarget{
		Bucket: "yelp-data", Scope: "_default", Collection: "_default",
	}, SuggestOptions{Limit: 5}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.FastTier || r.AIProcessResult {
		t.Fatal("typeahead is Direct FTS, not agent")
	}
	if r.FTSReqID != "fts-1" || r.Count != 1 || r.Hits[0].Name != "Sushi" {
		t.Fatalf("%+v", r)
	}
	if r.TraceClass != zeushttp.TraceClassDirectInteractive {
		t.Fatalf("class %s", r.TraceClass)
	}
	if len(z.reqs) != 1 {
		t.Fatal("expected one search hop")
	}
	req := z.reqs[0]
	if req.Verb != "search" {
		t.Fatalf("verb %s (must wrap search, never agent)", req.Verb)
	}
	if req.AllowPipeline {
		t.Fatal("AllowPipeline")
	}
	if req.Headers["X-Zeus-Trace-Class"] != zeushttp.TraceClassDirectInteractive {
		t.Fatalf("%v", req.Headers)
	}
	if _, ok := req.Headers["X-Zeus-Trace"]; ok {
		t.Fatal("force trace")
	}
	if _, ok := req.Headers["X-Zeus-Req-Id"]; ok {
		t.Fatal("must not mint req id")
	}
	if req.Body["strategy"] != "fts" {
		t.Fatalf("strategy %v", req.Body["strategy"])
	}
	if req.Body["query_text"] != "sushi" {
		t.Fatalf("q %v", req.Body["query_text"])
	}
}

func TestTypeaheadHopErrorSurfacesReqID(t *testing.T) {
	z := &recordingZeus{
		hop: ports.VerbHopResult{OK: false, StatusCode: 400, ReqID: "fts-err", Error: "zeus HTTP 400"},
	}
	r, err := RunTypeaheadSearch(context.Background(), z, "sushi", config.DataTarget{}, SuggestOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "error" || r.FTSReqID != "fts-err" || r.Count != 0 {
		t.Fatalf("%+v", r)
	}
}
