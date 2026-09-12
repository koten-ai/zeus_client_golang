// SPDX-License-Identifier: BUSL-1.1

//go:build patterna

package main

import (
	"context"
	"sync"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// recordedZeus is an in-process ZeusPort for the checked-in Jobs().Run
// transcript. Not live Zeus. CallVerb is still a real unit hop.
type recordedZeus struct {
	mu    sync.Mutex
	calls []ports.VerbRequest
}

func (r *recordedZeus) ResolveAuth(context.Context, config.DataTarget, bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recordedZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	items := []any{
		map[string]any{"name": "Cherry Lambic", "entity_type": "Beer", "bucket": req.Target.Bucket},
		map[string]any{"name": "Raspberry Gose", "entity_type": "Beer", "bucket": req.Target.Bucket},
	}
	return ports.VerbHopResult{
		OK:         true,
		StatusCode: 200,
		ReqID:      "req-" + req.Target.Bucket,
		Body:       map[string]any{"items": items},
	}, nil
}

// recordedLLM scripts one worker completion. AgentTurn uses the worker role;
// Direct units do not call it. First hop: find tool; later: cheap text.
type recordedLLM struct {
	mu sync.Mutex
	n  int
}

func (s *recordedLLM) Complete(_ context.Context, _ ports.LlmRequest) (ports.LlmResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	if s.n == 1 {
		return ports.LlmResponse{
			ToolCalls: []map[string]any{
				{
					"id":   "c1",
					"type": "function",
					"function": map[string]any{
						"name":      "find",
						"arguments": `{"entity_type": "Beer"}`,
					},
				},
			},
			Usage: map[string]any{"prompt": 40, "completion": 12, "ok": true},
		}, nil
	}
	return ports.LlmResponse{
		Content: "Sales shortlist: cherry lambic, raspberry gose.",
		Usage:   map[string]any{"prompt": 60, "completion": 18, "ok": true},
	}, nil
}
