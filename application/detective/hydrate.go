// SPDX-License-Identifier: BUSL-1.1

package detective

import "sort"

// MergeHubHydrate attaches hub snapshot metadata without clobbering client inject pass.
func MergeHubHydrate(briefing map[string]any, hubPayload map[string]any, preferredReqID string) map[string]any {
	out := copyMap(briefing)
	if len(hubPayload) == 0 {
		out["hub_hydrated"] = false
		return out
	}
	out["hub_hydrated"] = true
	out["source"] = "client+hub"
	keys := make([]string, 0, len(hubPayload))
	for k := range hubPayload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	hubMeta := map[string]any{
		"preferred_req_id": preferredReqID,
		"keys":             keys,
	}
	if preferredReqID == "" {
		hubMeta["preferred_req_id"] = nil
	}
	hubPC := asMap(hubPayload["prompt_checklist"])
	clientPrompt := asMap(out["prompt"])
	clientVerdict := asString(clientPrompt["verdict"])
	var notes []string
	if clientPrompt != nil {
		switch n := clientPrompt["notes"].(type) {
		case []string:
			notes = append([]string{}, n...)
		case []any:
			for _, x := range n {
				notes = append(notes, asString(x))
			}
		}
	}
	if hubPC != nil {
		hubVerdict := asString(hubPC["verdict"])
		if hubVerdict == "" {
			hubVerdict = asString(hubPC["status"])
		}
		hubMeta["prompt_checklist_verdict"] = hubVerdict
		if clientVerdict == "pass" && (hubVerdict == "fail" || hubVerdict == "skip" || hubVerdict == "missing") {
			notes = append(notes, "hub_prompt_conflict: client inject pass; hub tool-hop checklist ="+hubVerdict+" (client remains authoritative)")
			prompt := copyMap(clientPrompt)
			prompt["notes"] = notes
			out["prompt"] = prompt
		}
	}
	out["hub"] = hubMeta
	return out
}
