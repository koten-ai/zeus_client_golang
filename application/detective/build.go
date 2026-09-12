// SPDX-License-Identifier: BUSL-1.1

package detective

import (
	"os"
	"strings"
)

// BriefingArgs is Python build_detective_briefing kwargs.
type BriefingArgs struct {
	TurnID              string
	Answer              string
	Status              string
	Rounds              int
	Hops                []map[string]any
	Notes               []string
	Messages            []map[string]any
	SystemPrompt        string
	Catalog             map[string]any
	Tools               []map[string]any
	LayerA              map[string]any
	TotalMS             *int
	HubBaseURL          string
	SessionID           string
	Target              map[string]any
	ContractStatus      string
	AIProcessResult     *bool
	AIProcessResultExit string
	HubPayload          map[string]any
	PublicTrace         map[string]any
	ChatID              string
	ZeusURL             string
	ClientVersion       string
	ExportRef           string
	Enabled             *bool
	Env                 map[string]string
}

// Enabled reports whether Detective briefing is on (Python detective_enabled).
// Env nil uses process env; a non-nil map (even empty) isolates tests.
func Enabled(debugPolicyEnabled bool, env map[string]string) bool {
	raw := "1"
	if env != nil {
		if v, ok := env["ZEUS_CLIENT_DETECTIVE"]; ok {
			raw = v
		}
	} else if v, ok := os.LookupEnv("ZEUS_CLIENT_DETECTIVE"); ok {
		raw = v
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	}
	if !debugPolicyEnabled {
		return false
	}
	return true
}

// Build is Python build_detective_briefing.
func Build(in BriefingArgs) map[string]any {
	pt := in.PublicTrace
	if pt == nil {
		pt = map[string]any{}
	}
	hops := in.Hops
	if hops == nil {
		if raw, ok := pt["hops"].([]map[string]any); ok {
			hops = raw
		} else if raw, ok := pt["hops"].([]any); ok {
			for _, x := range raw {
				if m, ok := x.(map[string]any); ok {
					hops = append(hops, m)
				}
			}
		}
	}
	notes := in.Notes
	if notes == nil {
		if raw, ok := pt["notes"].([]string); ok {
			notes = raw
		}
	}
	answer := in.Answer
	if answer == "" {
		answer = asString(pt["answer"])
	}
	status := in.Status
	if status == "" {
		status = asString(pt["status"])
	}
	rounds := in.Rounds
	if rounds == 0 {
		if n, ok := asInt(pt["rounds"]); ok {
			rounds = n
		}
	}
	layerA := in.LayerA
	if layerA == nil {
		layerA = asMap(pt["layer_a"])
	}
	turnID := in.TurnID
	if turnID == "" {
		turnID = asString(pt["turn_id"])
	}
	ai := in.AIProcessResult
	if ai == nil {
		if b, ok := pt["ai_process_result"].(bool); ok {
			ai = &b
		}
	}
	aiExit := in.AIProcessResultExit
	if aiExit == "" {
		aiExit = asString(pt["ai_process_result_exit"])
	}

	prompt := BuildPromptChecklist(in.Messages, in.SystemPrompt, in.Catalog, in.Tools)
	tokensArg := asMap(pt["tokens"])
	if tokensArg == nil {
		if steps, ok := pt["steps"].([]map[string]any); ok && len(steps) > 0 {
			tokensArg = sumSteps(steps)
		} else if raw, ok := pt["steps"].([]any); ok && len(raw) > 0 {
			var steps []map[string]any
			for _, x := range raw {
				if m, ok := x.(map[string]any); ok {
					steps = append(steps, m)
				}
			}
			tokensArg = sumSteps(steps)
		}
	}

	overview := BuildOverview(OverviewArgs{
		TurnID:              turnID,
		Answer:              answer,
		Status:              status,
		Rounds:              rounds,
		Hops:                hops,
		Notes:               notes,
		Messages:            in.Messages,
		SystemPrompt:        in.SystemPrompt,
		Catalog:             in.Catalog,
		LayerA:              layerA,
		TotalMS:             in.TotalMS,
		HubBaseURL:          in.HubBaseURL,
		SessionID:           in.SessionID,
		Target:              in.Target,
		AIProcessResult:     ai,
		AIProcessResultExit: aiExit,
		Tokens:              tokensArg,
	})
	exportRef := in.ExportRef
	if exportRef == "" {
		exportRef = turnID
	}
	diagnosis := BuildDiagnosis(DiagnosisArgs{
		Answer:         answer,
		Hops:           hops,
		Notes:          notes,
		Prompt:         prompt,
		ContractStatus: in.ContractStatus,
		LayerA:         layerA,
		TurnID:         turnID,
		ChatID:         in.ChatID,
		SessionID:      in.SessionID,
		Status:         status,
		TotalMS:        in.TotalMS,
		Rounds:         rounds,
		Target:         in.Target,
		ZeusURL:        in.ZeusURL,
		ClientVersion:  in.ClientVersion,
		Catalog:        in.Catalog,
		Tokens:         tokensArg,
		ExportRef:      exportRef,
	})
	briefing := map[string]any{
		"version":      1,
		"source":       "client",
		"hub_hydrated": false,
		"overview":     overview,
		"prompt":       prompt,
		"diagnosis":    diagnosis,
	}
	if in.HubPayload != nil {
		briefing = MergeHubHydrate(briefing, in.HubPayload, asString(overview["preferred_req_id"]))
	}
	return briefing
}

func sumSteps(steps []map[string]any) map[string]any {
	prompt, completion, total := 0, 0, 0
	ok := false
	for _, s := range steps {
		u := asMap(s["usage"])
		if u == nil {
			continue
		}
		p, _ := asInt(u["prompt_tokens"])
		c, _ := asInt(u["completion_tokens"])
		t, _ := asInt(u["total_tokens"])
		if t == 0 && (p != 0 || c != 0) {
			t = p + c
		}
		if p != 0 || c != 0 || t != 0 {
			ok = true
		}
		prompt += p
		completion += c
		total += t
	}
	if !ok {
		return nil
	}
	return map[string]any{
		"prompt": prompt, "completion": completion, "total": total,
		"cached": 0, "extra": 0, "ok": true,
	}
}

// SafeBuild is the soft-fail wrapper (Python safe_build_detective_briefing).
func SafeBuild(in BriefingArgs) (out map[string]any) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	on := true
	if in.Enabled != nil {
		on = *in.Enabled
	}
	if !Enabled(on, in.Env) {
		return nil
	}
	return Build(in)
}
