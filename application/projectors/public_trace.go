// SPDX-License-Identifier: BUSL-1.1

package projectors

// PublicTrace is the widget projection input (Python build_public_trace kwargs).
type PublicTrace struct {
	TurnID              string
	Answer              string
	Status              string
	Rounds              int
	Notes               []string
	Hops                []map[string]any
	AIProcessResult     bool
	AIProcessResultExit string
	LayerA              map[string]any
	Policy              string
	Flags               map[string]bool
	Steps               []map[string]any
	Tokens              map[string]any
	Session             map[string]any
	Inject              map[string]any
	Stamp               map[string]any
	Detective           map[string]any
}

// BuildPublicTrace is the widget-friendly projection — never put G2 dumps in answer.
func BuildPublicTrace(in PublicTrace) map[string]any {
	notes := in.Notes
	if notes == nil {
		notes = []string{}
	}
	hops := make([]map[string]any, 0, len(in.Hops))
	for _, h := range in.Hops {
		hops = append(hops, copyMap(h))
	}
	steps := make([]map[string]any, 0, len(in.Steps))
	for _, s := range in.Steps {
		steps = append(steps, copyMap(s))
	}
	flags := map[string]bool{}
	for k, v := range in.Flags {
		flags[k] = v
	}
	var layerA any
	artifacts := []string{}
	if len(in.LayerA) > 0 {
		layerA = copyMap(in.LayerA)
		artifacts = []string{"jail_break_attempt", "wish_i_knew", "hooks_jailbreak_score"}
	}
	var exit any
	if in.AIProcessResultExit != "" {
		exit = in.AIProcessResultExit
	}
	var policy any
	if in.Policy != "" {
		policy = in.Policy
	}
	out := map[string]any{
		"turn_id":                in.TurnID,
		"answer":                 in.Answer,
		"status":                 in.Status,
		"rounds":                 in.Rounds,
		"notes":                  notes,
		"steps":                  steps,
		"hops":                   hops,
		"ai_process_result":      in.AIProcessResult,
		"ai_process_result_exit": exit,
		"layer_a":                layerA,
		"policy":                 policy,
		"flags":                  flags,
		"artifacts_keys":         artifacts,
	}
	if in.Tokens != nil {
		out["tokens"] = copyMap(in.Tokens)
	}
	if len(in.Session) > 0 {
		out["session"] = copyMap(in.Session)
	}
	if len(in.Inject) > 0 {
		out["inject"] = copyMap(in.Inject)
	}
	if len(in.Stamp) > 0 {
		out["user"] = in.Stamp["user"]
		out["stamp"] = copyMap(in.Stamp)
	}
	if len(in.Detective) > 0 {
		out["detective"] = copyMap(in.Detective)
	}
	return out
}
