// SPDX-License-Identifier: BUSL-1.1

package detective

import "strings"

func tokensBlock(tokens map[string]any) map[string]any {
	if len(tokens) == 0 {
		return nil
	}
	prompt, _ := asInt(tokens["prompt"])
	comp, _ := asInt(tokens["completion"])
	total, _ := asInt(tokens["total"])
	cached, _ := asInt(tokens["cached"])
	extra, _ := asInt(tokens["extra"])
	return map[string]any{
		"prompt":     prompt,
		"completion": comp,
		"total":      total,
		"cached":     cached,
		"extra":      extra,
		"ok":         asBool(tokens["ok"]),
	}
}

// OverviewArgs is Python build_overview kwargs.
type OverviewArgs struct {
	TurnID              string
	Answer              string
	Status              string
	Rounds              int
	Hops                []map[string]any
	Notes               []string
	Messages            []map[string]any
	SystemPrompt        string
	Catalog             map[string]any
	LayerA              map[string]any
	TotalMS             *int
	HubBaseURL          string
	SessionID           string
	Target              map[string]any
	AIProcessResult     *bool
	AIProcessResultExit string
	Tokens              map[string]any
}

// BuildOverview is Python build_overview.
func BuildOverview(in OverviewArgs) map[string]any {
	hops := in.Hops
	reqIDs := CollectReqIDs(hops)
	pref := PreferredReqID(hops)
	system := SystemPromptOf(in.Messages, in.SystemPrompt, in.Catalog)
	flags := CatalogFlagsOf(system, in.Catalog)
	hub := strings.TrimRight(in.HubBaseURL, "/")
	links := map[string]string{}
	if hub != "" && pref != "" {
		links["req"] = hub + "/hub/debug/req/" + pref
		links["detective_req"] = hub + "/hub/debug/req/" + pref
	}
	if hub != "" && in.SessionID != "" {
		links["session"] = hub + "/hub/debug/session/" + in.SessionID
	}
	tgt := in.Target
	if tgt == nil {
		tgt = map[string]any{}
	}
	var layerSummary, layerConf any
	if in.LayerA != nil {
		layerSummary = in.LayerA["summary"]
		layerConf = in.LayerA["confidence"]
	}
	var total any
	if in.TotalMS != nil {
		total = *in.TotalMS
	}
	var ai any
	if in.AIProcessResult != nil {
		ai = *in.AIProcessResult
	}
	var aiExit any
	if in.AIProcessResultExit != "" {
		aiExit = in.AIProcessResultExit
	}
	if reqIDs == nil {
		reqIDs = []string{}
	}
	var prefAny any
	if pref != "" {
		prefAny = pref
	}
	return map[string]any{
		"turn_id":          in.TurnID,
		"status":           in.Status,
		"rounds":           in.Rounds,
		"total_ms":         total,
		"req_ids":          reqIDs,
		"preferred_req_id": prefAny,
		"hop_count":        len(hops),
		"answer_preview":   clipRunes(in.Answer, 240),
		"catalog_flags": map[string]any{
			"has_scope_brief": flags["has_scope_brief"],
			"has_mini_schema": flags["has_mini_schema"],
		},
		"layer_a_summary":        layerSummary,
		"layer_a_confidence":     layerConf,
		"ai_process_result":      ai,
		"ai_process_result_exit": aiExit,
		"hub_links":              links,
		"target": map[string]any{
			"bucket":     tgt["bucket"],
			"scope":      tgt["scope"],
			"collection": tgt["collection"],
			"mode":       tgt["mode"],
		},
		"notes_count": len(in.Notes),
		"tokens":      tokensBlock(in.Tokens),
	}
}
