// SPDX-License-Identifier: BUSL-1.1

package application

import (
	"context"
	"strings"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const dataVerbComponent = "application.data_verb"

// ExposedV2Verbs is the public Direct allow-list (Python EXPOSED_V2_VERBS).
// pipeline is omitted; agent may set VerbRequest.AllowPipeline later (ZCG-24).
var ExposedV2Verbs = zeushttp.ExposedV2Verbs

// VerbResult is one direct V2 verb outcome (Python application.data_verb.VerbResult).
type VerbResult struct {
	Verb       string
	OK         bool
	StatusCode int
	ReqID      string
	Body       map[string]any
	Error      string
	URLHint    string
	ChatID     string
	TurnID     string
	ReqIDs     []string
	TraceClass string
}

// Map is the public dict shape (Python VerbResult.to_dict).
func (r VerbResult) Map() map[string]any {
	body := r.Body
	if body == nil {
		body = map[string]any{}
	}
	out := map[string]any{
		"verb":        r.Verb,
		"ok":          r.OK,
		"status_code": r.StatusCode,
		"req_id":      nilIfEmpty(r.ReqID),
		"body":        body,
		"error":       nilIfEmpty(r.Error),
		"url_hint":    r.URLHint,
		"chat_id":     nilIfEmpty(r.ChatID),
		"turn_id":     nilIfEmpty(r.TurnID),
		"req_ids":     copyStrings(r.ReqIDs),
		"trace_class": r.TraceClass,
	}
	return out
}

// DataVerbOptions is run_data_verb kwargs (Python). AllowPipeline is not
// exposed — the public Direct surface always keeps it false.
type DataVerbOptions struct {
	Target        config.DataTarget
	ModeHeader    string
	Headers       map[string]string
	ForceTrace    bool
	Rewind        bool
	ChatID        string
	TurnID        string
	ChatSessionID string
	BriefSha12    string
	MiniSha12     string
	BaseURL       string
	AuthMode      string
	PasswordEnv   string
	TokenEnv      string
	Username      string
}

// RunDataVerb executes one allow-listed V2 verb via ZeusPort.
// pipeline → 060010 (HTTP not sent). Unknown verb → 060006.
func RunDataVerb(ctx context.Context, zeus ports.ZeusPort, verb string, body map[string]any, opts DataVerbOptions) (VerbResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if de := domain.FromContext(err); de != nil {
			return VerbResult{}, de
		}
		return VerbResult{}, err
	}
	name := strings.TrimSpace(verb)
	if name == "pipeline" {
		return VerbResult{}, domain.NewZeusTool(domain.CodeZeusPipelineNotOnDirect, dataVerbComponent,
			domain.WithMessage("zeus pipeline not on direct surface"))
	}
	if !zeushttp.IsExposedV2Verb(name) {
		return VerbResult{}, domain.NewZeusTool(domain.CodeZeusVerbNotAllowed, dataVerbComponent,
			domain.WithMessage("zeus verb not allow-listed"),
			domain.WithDetails(map[string]any{
				"verb":    name,
				"allowed": zeushttp.ExposedV2VerbList(),
			}),
		)
	}
	if zeus == nil {
		return VerbResult{}, domain.New(domain.CodeNotImplemented, dataVerbComponent,
			domain.WithMessage("Zeus port not wired on runtime"))
	}
	mode := strings.TrimSpace(opts.ModeHeader)
	if mode == "" {
		mode = "analytics"
	}
	hop, err := zeus.CallVerb(ctx, ports.VerbRequest{
		Verb:       name,
		Body:       zeushttp.VerbBodyWithoutRewind(body),
		Target:     opts.Target,
		ModeHeader: mode,
		Headers: zeushttp.MergeHeaders(
			zeushttp.CorrelationHeaders(zeushttp.Correlation{
				ChatID:        opts.ChatID,
				TurnID:        opts.TurnID,
				ForceTrace:    opts.ForceTrace,
				TraceClass:    zeushttp.TraceClassDirectRead,
				ChatSessionID: opts.ChatSessionID,
				BriefSha12:    opts.BriefSha12,
				MiniSha12:     opts.MiniSha12,
			}),
			opts.Headers,
		),
		BaseURL:     opts.BaseURL,
		AuthMode:    opts.AuthMode,
		PasswordEnv: opts.PasswordEnv,
		TokenEnv:    opts.TokenEnv,
		Username:    opts.Username,
		Rewind:      opts.Rewind,
	})
	if err != nil {
		return VerbResult{}, err
	}
	reqIDs := []string{}
	if hop.ReqID != "" {
		reqIDs = []string{hop.ReqID}
	}
	b := hop.Body
	if b == nil {
		b = map[string]any{}
	}
	return VerbResult{
		Verb:       name,
		OK:         hop.OK,
		StatusCode: hop.StatusCode,
		ReqID:      hop.ReqID,
		Body:       b,
		Error:      hop.Error,
		URLHint:    hop.URL,
		ChatID:     opts.ChatID,
		TurnID:     opts.TurnID,
		ReqIDs:     reqIDs,
		TraceClass: zeushttp.TraceClassDirectRead,
	}, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func copyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
