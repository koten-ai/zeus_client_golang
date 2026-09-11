// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

type recordingZeus struct {
	reqs []ports.VerbRequest
	hop  ports.VerbHopResult
	err  error
	n    atomic.Int32
}

func (r *recordingZeus) ResolveAuth(_ context.Context, _ config.DataTarget, _ bool) (ports.AuthContext, error) {
	return ports.AuthContext{Mode: "none", Headers: map[string]string{}}, nil
}

func (r *recordingZeus) CallVerb(_ context.Context, req ports.VerbRequest) (ports.VerbHopResult, error) {
	r.n.Add(1)
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return ports.VerbHopResult{}, r.err
	}
	if r.hop.Body == nil && r.hop.StatusCode == 0 && !r.hop.OK {
		return ports.VerbHopResult{OK: true, StatusCode: 200, ReqID: "req-1", Body: map[string]any{"ok": true}}, nil
	}
	return r.hop, nil
}

var _ ports.ZeusPort = (*recordingZeus)(nil)

func mustCode(t *testing.T, err error, code domain.Code) *domain.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	de, ok := domain.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("got %v want %s", err, code)
	}
	return de
}

func TestExposedVerbsExcludePipeline(t *testing.T) {
	if zeushttp.IsExposedV2Verb("pipeline") {
		t.Fatal("pipeline must not be exposed")
	}
	for _, v := range []string{"find", "search", "get", "project", "explain", "describe"} {
		if !zeushttp.IsExposedV2Verb(v) {
			t.Fatalf("missing %s", v)
		}
	}
	for _, v := range ExposedV2Verbs {
		if v == "pipeline" {
			t.Fatal("pipeline in ExposedV2Verbs")
		}
	}
}

func TestRunDataVerbPipelineRejectedNoCall(t *testing.T) {
	z := &recordingZeus{}
	_, err := RunDataVerb(context.Background(), z, "pipeline", map[string]any{}, DataVerbOptions{
		Target: config.DataTarget{Bucket: "yelp-data", Scope: "_default", Collection: "_default"},
	})
	mustCode(t, err, domain.CodeZeusPipelineNotOnDirect)
	if z.n.Load() != 0 {
		t.Fatalf("HTTP/CallVerb sent %d times", z.n.Load())
	}
}

func TestRunDataVerbUnknownNotAllowed(t *testing.T) {
	z := &recordingZeus{}
	_, err := RunDataVerb(context.Background(), z, "nope", map[string]any{}, DataVerbOptions{})
	de := mustCode(t, err, domain.CodeZeusVerbNotAllowed)
	if de.Details["verb"] != "nope" {
		t.Fatalf("details %v", de.Details)
	}
	if z.n.Load() != 0 {
		t.Fatal("CallVerb sent")
	}
}

func TestRunDataVerbFindForwardsDirectRead(t *testing.T) {
	z := &recordingZeus{}
	east := config.DataTarget{Bucket: "east", Scope: "sales", Collection: "_default"}
	result, err := RunDataVerb(context.Background(), z, "find", map[string]any{"entity_type": "Beer", "rewind": true}, DataVerbOptions{
		Target:        east,
		ChatID:        "job_1",
		TurnID:        "unit_u1",
		ChatSessionID: "sess_direct_1",
		BriefSha12:    "aaa111bbb222",
		MiniSha12:     "ccc333ddd444",
		Rewind:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.ReqID != "req-1" {
		t.Fatalf("%+v", result)
	}
	if result.TraceClass != zeushttp.TraceClassDirectRead {
		t.Fatalf("trace %s", result.TraceClass)
	}
	if len(z.reqs) != 1 {
		t.Fatalf("reqs %d", len(z.reqs))
	}
	req := z.reqs[0]
	if req.AllowPipeline {
		t.Fatal("AllowPipeline must stay false")
	}
	if req.Verb != "find" || req.Target != east {
		t.Fatalf("%+v", req)
	}
	if _, ok := req.Body["rewind"]; ok {
		t.Fatal("rewind leaked into body")
	}
	if !req.Rewind {
		t.Fatal("rewind flag")
	}
	h := req.Headers
	if h["X-Zeus-Chat-Id"] != "job_1" || h["X-Zeus-Turn-Id"] != "unit_u1" {
		t.Fatalf("%v", h)
	}
	if h["X-Zeus-Trace-Class"] != zeushttp.TraceClassDirectRead {
		t.Fatalf("%v", h)
	}
	if h["X-Zeus-Chat-Session-Id"] != "sess_direct_1" {
		t.Fatal("session")
	}
	if _, ok := h["X-Zeus-Req-Id"]; ok {
		t.Fatal("must not mint req id")
	}
	if _, ok := h["X-Zeus-Session"]; ok {
		t.Fatal("must not set session")
	}
}

func TestRunDataVerbNilPort(t *testing.T) {
	_, err := RunDataVerb(context.Background(), nil, "find", nil, DataVerbOptions{})
	mustCode(t, err, domain.CodeNotImplemented)
}
