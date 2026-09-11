// SPDX-License-Identifier: BUSL-1.1

package projectors

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/koten-ai/zeus_client_golang/adapters/zeushttp"
	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/domain/journal"
)

const (
	projectorComponent = "application.projectors.session_trace"

	// TraceSnippetMax is longer than the historical 300-char snip so Detective
	// can show step_costs / empty-result signals without re-running.
	TraceSnippetMax = 2000
)

var searchFindNames = map[string]struct{}{
	"search":        {},
	"find":          {},
	"find_nodes":    {},
	"text_search":   {},
	"hybrid_search": {},
	"project":       {},
	"get":           {},
	"get_by_keys":   {},
}

// AggregateTracePayload is the typed aggregate for one session round
// (identical body per hop post).
type AggregateTracePayload struct {
	PrimaryReqID string
	Turns        []map[string]string
	ZeusResponse map[string]any
	Outcome      string
}

// SessionTraceProjectResult is the outcome of posting multi-hop session-trace
// (soft-fail friendly — never a raising error).
type SessionTraceProjectResult struct {
	OK             bool
	PrimaryReqID   string
	PreferredReqID string
	PostOrder      []string
	Posts          int
	Errors         []string
	ContractStatus string
	Aggregate      map[string]any
}

// ProjectOptions is project_session_trace kwargs.
type ProjectOptions struct {
	Handle      domain.SessionHandle
	Hops        []any
	ChatRequest map[string]any
	LayerA      map[string]any
	Inject      map[string]any
	Tokens      map[string]any
	Catalog     map[string]any
	Terminate   map[string]any
	Stamp       map[string]any
	Mode        string
	Headers     map[string]string
	Rewind      bool
	TurnID      string
	Target      config.DataTarget
	Journal     journal.ExecutionJournal
	Log         func(level, msg string, attrs map[string]any)
}

type hopScore struct {
	err          int
	hasRows      int
	isSearchFind int
	hasCosts     int
	hasBody      int
	nonPipeline  int
}

func statusOK(status any) bool {
	if status == nil {
		return true
	}
	if n, ok := asInt(status); ok {
		return n > 0 && n < 400
	}
	s := strings.ToLower(fmt.Sprint(status))
	switch s {
	case "ok", "200", "201", "204":
		return true
	default:
		return false
	}
}

// NormalizeHop normalizes a hop tuple/dict into a stable map.
func NormalizeHop(raw any) map[string]any {
	if raw == nil {
		return map[string]any{
			"req_id":  "",
			"name":    "",
			"status":  nil,
			"snippet": "",
			"url":     "",
		}
	}
	if m, ok := asMap(raw); ok {
		rid := strings.TrimSpace(asString(m["req_id"]))
		name := strings.TrimSpace(asString(m["name"]))
		if name == "" {
			name = strings.TrimSpace(asString(m["verb"]))
		}
		status := m["status"]
		if _, hasStatus := m["status"]; !hasStatus {
			status = m["tstatus"]
		}
		snip := m["snippet"]
		if snip == nil {
			snip = firstNonNil(m["result_text"], m["snip"], "")
		}
		snippet := clipSnippet(asString(snip))
		url := asString(m["url"])
		if url == "" {
			url = asString(m["dispatch_url"])
		}
		hop := map[string]any{
			"req_id":  rid,
			"name":    name,
			"status":  status,
			"snippet": snippet,
			"url":     url,
		}
		if ms, ok := asInt(m["ms"]); ok && m["ms"] != nil {
			hop["ms"] = ms
		}
		for _, key := range []string{
			"step_costs", "result_size", "pipeline_status", "steps_executed", "bytes", "error",
		} {
			if m[key] != nil {
				hop[key] = m[key]
			}
		}
		return hop
	}
	seq, ok := asSlice(raw)
	if !ok {
		return map[string]any{
			"req_id":  "",
			"name":    "",
			"status":  nil,
			"snippet": "",
			"url":     "",
		}
	}
	rid := ""
	if len(seq) > 0 {
		rid = strings.TrimSpace(asString(seq[0]))
	}
	name := ""
	if len(seq) > 1 {
		name = strings.TrimSpace(asString(seq[1]))
	}
	var status any
	if len(seq) > 2 {
		status = seq[2]
	}
	snip := ""
	if len(seq) > 3 {
		snip = asString(seq[3])
	}
	url := ""
	if len(seq) > 4 {
		url = asString(seq[4])
	}
	hop := map[string]any{
		"req_id":  rid,
		"name":    name,
		"status":  status,
		"snippet": clipSnippet(snip),
		"url":     url,
	}
	if len(seq) > 5 && seq[5] != nil {
		if ms, ok := asInt(seq[5]); ok {
			hop["ms"] = ms
		}
	}
	return hop
}

// NormalizeHops drops blank / duplicate req_id hops (first wins).
func NormalizeHops(rawHops []any) []map[string]any {
	out := make([]map[string]any, 0, len(rawHops))
	seen := map[string]struct{}{}
	for _, raw := range rawHops {
		hop := NormalizeHop(raw)
		rid := asString(hop["req_id"])
		if rid == "" {
			continue
		}
		if _, ok := seen[rid]; ok {
			continue
		}
		seen[rid] = struct{}{}
		out = append(out, hop)
	}
	return out
}

func resultSizeOf(hop map[string]any) (int, bool) {
	if hop == nil {
		return 0, false
	}
	if hop["result_size"] != nil {
		if n, ok := asInt(hop["result_size"]); ok {
			return n, true
		}
	}
	costs, ok := asSlice(hop["step_costs"])
	if !ok || len(costs) == 0 {
		return 0, false
	}
	total := 0
	anySz := false
	for _, c := range costs {
		cm, ok := asMap(c)
		if !ok || cm["result_size"] == nil {
			continue
		}
		n, ok := asInt(cm["result_size"])
		if !ok {
			continue
		}
		total += n
		anySz = true
	}
	if anySz {
		return total, true
	}
	return 0, false
}

func hopScoreOf(hop map[string]any) hopScore {
	name := strings.ToLower(asString(hop["name"]))
	ok := statusOK(hop["status"])
	err := 0
	if !ok {
		err = 1
	}
	isPipeline := 0
	if name == "pipeline" {
		isPipeline = 1
	}
	isSearchFind := 0
	if _, hit := searchFindNames[name]; hit {
		isSearchFind = 1
	}
	sz, hasSz := resultSizeOf(hop)
	hasRows := 0
	if hasSz && sz > 0 {
		hasRows = 1
	}
	hasCosts := 0
	if truthy(hop["step_costs"]) {
		hasCosts = 1
	}
	hasBody := 0
	if truthy(hop["snippet"]) || truthy(hop["bytes"]) {
		hasBody = 1
	}
	nonPipeline := 1
	if isPipeline == 1 {
		nonPipeline = 0
	}
	return hopScore{err, hasRows, isSearchFind, hasCosts, hasBody, nonPipeline}
}

func (a hopScore) greater(b hopScore) bool {
	if a.err != b.err {
		return a.err > b.err
	}
	if a.hasRows != b.hasRows {
		return a.hasRows > b.hasRows
	}
	if a.isSearchFind != b.isSearchFind {
		return a.isSearchFind > b.isSearchFind
	}
	if a.hasCosts != b.hasCosts {
		return a.hasCosts > b.hasCosts
	}
	if a.hasBody != b.hasBody {
		return a.hasBody > b.hasBody
	}
	return a.nonPipeline > b.nonPipeline
}

// SelectPrimaryHop picks the best hop for TraceDoc.req_id / Detective deep-link.
// Empty hops (or none with req_id) → nil.
func SelectPrimaryHop(hops []map[string]any) map[string]any {
	ranked := make([]map[string]any, 0, len(hops))
	for _, h := range hops {
		if asString(h["req_id"]) != "" {
			ranked = append(ranked, h)
		}
	}
	if len(ranked) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(ranked); i++ {
		if hopScoreOf(ranked[i]).greater(hopScoreOf(ranked[best])) {
			best = i
		} else if !hopScoreOf(ranked[best]).greater(hopScoreOf(ranked[i])) {
			// equal scores: keep earlier hop (Python stable reverse sort)
		}
	}
	return copyMap(ranked[best])
}

// SelectPrimaryReqID normalizes hops then returns the primary req_id.
func SelectPrimaryReqID(hops []any) string {
	norm := NormalizeHops(hops)
	primary := SelectPrimaryHop(norm)
	if primary == nil {
		return ""
	}
	return asString(primary["req_id"])
}

func turnLine(hop map[string]any) string {
	name := asString(hop["name"])
	if name == "" {
		name = "tool"
	}
	status := hop["status"]
	parts := []string{fmt.Sprintf("%s → %v", name, status)}
	if hop["ms"] != nil {
		parts = append(parts, fmt.Sprintf("%vms", hop["ms"]))
	}
	if sz, ok := resultSizeOf(hop); ok {
		parts = append(parts, fmt.Sprintf("result_size=%d", sz))
	}
	costs, ok := asSlice(hop["step_costs"])
	if ok && len(costs) > 0 {
		parts = append(parts, fmt.Sprintf("steps=%d", len(costs)))
		brief := make([]string, 0, 8)
		limit := len(costs)
		if limit > 8 {
			limit = 8
		}
		for _, c := range costs[:limit] {
			cm, ok := asMap(c)
			if !ok {
				continue
			}
			a := asString(firstNonNil(cm["as"], cm["name"], "?"))
			st := asString(cm["status"])
			if st == "" {
				st = "ok"
			}
			if cm["result_size"] != nil {
				brief = append(brief, fmt.Sprintf("%s:%s:%v", a, st, cm["result_size"]))
			} else {
				brief = append(brief, a+":"+st)
			}
		}
		if len(brief) > 0 {
			parts = append(parts, "["+strings.Join(brief, ", ")+"]")
		}
	}
	if hop["error"] != nil {
		err := fmt.Sprint(hop["error"])
		if len(err) > 120 {
			err = err[:120]
		}
		parts = append(parts, "err="+err)
	} else if !statusOK(status) && truthy(hop["snippet"]) {
		snip := asString(hop["snippet"])
		if len(snip) > 120 {
			snip = snip[:120]
		}
		parts = append(parts, snip)
	}
	return strings.Join(parts, " · ")
}

// BuildAggregateTracePayload builds the typed aggregate for one round
// (identical body for every hop post).
func BuildAggregateTracePayload(hops []any, layerA, inject, tokens, catalog, terminate, stamp map[string]any) AggregateTracePayload {
	norm := NormalizeHops(hops)
	if len(norm) == 0 {
		z := map[string]any{}
		if len(layerA) > 0 {
			z["layer_a"] = copyMap(layerA)
		}
		if len(inject) > 0 {
			z["inject"] = copyMap(inject)
		}
		if len(tokens) > 0 {
			z["tokens"] = copyMap(tokens)
		}
		if len(catalog) > 0 {
			z["catalog"] = copyMap(catalog)
		}
		if len(terminate) > 0 {
			z["terminate"] = copyMap(terminate)
		}
		for k, v := range stamp {
			if v != nil {
				z[k] = v
			}
		}
		return AggregateTracePayload{
			Turns:        []map[string]string{},
			ZeusResponse: z,
			Outcome:      "ok",
		}
	}
	primary := SelectPrimaryHop(norm)
	primaryRID := asString(norm[0]["req_id"])
	if primary != nil {
		primaryRID = asString(primary["req_id"])
	}
	turns := make([]map[string]string, 0, len(norm))
	anyErr := false
	for _, h := range norm {
		turns = append(turns, map[string]string{"role": "tool", "content": turnLine(h)})
		if !statusOK(h["status"]) {
			anyErr = true
		}
	}
	outcome := "ok"
	if anyErr {
		outcome = "error"
	}
	hopSummaries := make([]map[string]any, 0, len(norm))
	reqIDs := make([]string, 0, len(norm))
	for _, h := range norm {
		rid := asString(h["req_id"])
		reqIDs = append(reqIDs, rid)
		entry := map[string]any{
			"req_id":  h["req_id"],
			"name":    h["name"],
			"status":  h["status"],
			"url":     h["url"],
			"primary": rid == primaryRID,
		}
		if h["ms"] != nil {
			entry["ms"] = h["ms"]
		}
		if sz, ok := resultSizeOf(h); ok {
			entry["result_size"] = sz
		}
		if h["step_costs"] != nil {
			entry["step_costs"] = h["step_costs"]
		}
		if h["pipeline_status"] != nil {
			entry["pipeline_status"] = h["pipeline_status"]
		}
		if h["steps_executed"] != nil {
			entry["steps_executed"] = h["steps_executed"]
		}
		if h["error"] != nil {
			entry["error"] = h["error"]
		}
		if truthy(h["snippet"]) {
			entry["snippet"] = h["snippet"]
		}
		hopSummaries = append(hopSummaries, entry)
	}
	prim := primary
	if prim == nil {
		prim = norm[0]
	}
	zeusResponse := map[string]any{
		"status":         prim["status"],
		"url":            asString(prim["url"]),
		"snippet":        asString(prim["snippet"]),
		"primary_req_id": primaryRID,
		"req_ids":        reqIDs,
		"tool_hops":      hopSummaries,
		"hop_count":      len(norm),
		"aggregate":      true,
	}
	if prim["ms"] != nil {
		zeusResponse["ms"] = prim["ms"]
	}
	if prim["step_costs"] != nil {
		zeusResponse["step_costs"] = prim["step_costs"]
	}
	if sz, ok := resultSizeOf(prim); ok {
		zeusResponse["result_size"] = sz
	}
	if len(layerA) > 0 {
		zeusResponse["layer_a"] = copyMap(layerA)
	}
	if len(inject) > 0 {
		zeusResponse["inject"] = copyMap(inject)
	}
	if len(tokens) > 0 {
		zeusResponse["tokens"] = copyMap(tokens)
	}
	if len(catalog) > 0 {
		zeusResponse["catalog"] = copyMap(catalog)
	}
	if len(terminate) > 0 {
		zeusResponse["terminate"] = copyMap(terminate)
	}
	for k, v := range stamp {
		if v == nil {
			continue
		}
		if _, exists := zeusResponse[k]; !exists {
			zeusResponse[k] = v
		}
	}
	return AggregateTracePayload{
		PrimaryReqID: primaryRID,
		Turns:        turns,
		ZeusResponse: zeusResponse,
		Outcome:      outcome,
	}
}

// OrderedReqIDsForTracePosts returns req ids to POST: secondaries first,
// primary last (round doc req_id).
func OrderedReqIDsForTracePosts(hops []any) []string {
	norm := NormalizeHops(hops)
	if len(norm) == 0 {
		return []string{}
	}
	primary := SelectPrimaryHop(norm)
	primaryRID := asString(norm[len(norm)-1]["req_id"])
	if primary != nil {
		primaryRID = asString(primary["req_id"])
	}
	out := make([]string, 0, len(norm))
	for _, h := range norm {
		rid := asString(h["req_id"])
		if rid != primaryRID {
			out = append(out, rid)
		}
	}
	out = append(out, primaryRID)
	return out
}

// ExtractPipelineMeta pulls compact pipeline fields from a parsed tool JSON body.
func ExtractPipelineMeta(parsed any) map[string]any {
	out := map[string]any{}
	root, ok := asMap(parsed)
	if !ok {
		return out
	}
	meta, metaOK := asMap(root["meta"])
	if !metaOK {
		if data, ok := asMap(root["data"]); ok {
			meta, metaOK = asMap(data["meta"])
			if root["status"] != nil {
				out["pipeline_status"] = root["status"]
			}
		}
	}
	if metaOK {
		if costs, ok := asSlice(meta["step_costs"]); ok {
			out["step_costs"] = costs
			total := 0
			anySz := false
			for _, c := range costs {
				cm, ok := asMap(c)
				if !ok || cm["result_size"] == nil {
					continue
				}
				n, ok := asInt(cm["result_size"])
				if !ok {
					continue
				}
				total += n
				anySz = true
			}
			if anySz {
				out["result_size"] = total
			}
		}
		if meta["steps_executed"] != nil {
			out["steps_executed"] = meta["steps_executed"]
		}
		if meta["elapsed_ms"] != nil {
			if _, hasMS := out["ms"]; !hasMS {
				if n, ok := asInt(meta["elapsed_ms"]); ok {
					if _, exists := out["ms_meta"]; !exists {
						out["ms_meta"] = n
					}
				}
			}
		}
	}
	if root["status"] != nil {
		if _, exists := out["pipeline_status"]; !exists {
			if s, ok := root["status"].(string); ok {
				out["pipeline_status"] = s
			}
		}
	}
	if result, ok := asMap(root["result"]); ok {
		rc := result["returned_count"]
		if rc == nil {
			if items, ok := asSlice(result["items"]); ok {
				rc = len(items)
			}
		}
		if rc == nil {
			if ids, ok := asSlice(result["node_ids"]); ok {
				rc = len(ids)
			}
		}
		if rc != nil {
			if _, exists := out["result_size"]; !exists {
				if n, ok := asInt(rc); ok {
					out["result_size"] = n
				}
			}
		}
	}
	if root["error"] != nil && out["error"] == nil {
		if em, ok := asMap(root["error"]); ok {
			if em["message"] != nil {
				out["error"] = em["message"]
			} else if em["code"] != nil {
				out["error"] = em["code"]
			} else {
				out["error"] = fmt.Sprint(em)
			}
		} else {
			out["error"] = fmt.Sprint(root["error"])
		}
	}
	if root["error"] != nil {
		if _, exists := out["error"]; !exists {
			if msg, ok := root["message"].(string); ok {
				out["error"] = msg
			}
		}
	}
	return out
}

// MergeHopIntoTraceSession stamps multi-hop ids onto trace["session"].
func MergeHopIntoTraceSession(trace map[string]any, reqIDs []string, primaryReqID string) {
	if trace == nil {
		return
	}
	sess, ok := asMap(trace["session"])
	if !ok {
		if trace["session"] != nil {
			return
		}
		sess = map[string]any{}
		trace["session"] = sess
	}
	copied := make([]string, len(reqIDs))
	copy(copied, reqIDs)
	sess["req_ids"] = copied
	if primaryReqID != "" {
		sess["primary_req_id"] = primaryReqID
		sess["preferred_req_id"] = primaryReqID
	}
}

// ProjectSessionTrace POSTs the identical multi-hop aggregate once per req_id
// (primary last). Soft-fail: never raises; ok=false + journal note on hop errors
// so the agent turn can still complete.
func ProjectSessionTrace(ctx context.Context, client *zeushttp.SessionClient, opts ProjectOptions) SessionTraceProjectResult {
	if ctx == nil {
		ctx = context.Background()
	}
	handle := opts.Handle
	if !handle.Enabled || strings.TrimSpace(handle.SessionID) == "" {
		return SessionTraceProjectResult{
			OK:        true,
			PostOrder: []string{},
		}
	}
	agg := BuildAggregateTracePayload(opts.Hops, opts.LayerA, opts.Inject, opts.Tokens, opts.Catalog, opts.Terminate, opts.Stamp)
	postOrder := OrderedReqIDsForTracePosts(opts.Hops)
	if len(postOrder) == 0 {
		return SessionTraceProjectResult{
			OK:             true,
			PrimaryReqID:   agg.PrimaryReqID,
			PreferredReqID: agg.PrimaryReqID,
			PostOrder:      []string{},
			Aggregate:      copyMap(agg.ZeusResponse),
		}
	}
	mode := opts.Mode
	if mode == "" {
		mode = "analytics"
	}
	chatReq := opts.ChatRequest
	if chatReq == nil {
		chatReq = map[string]any{}
	}
	turns := make([]any, len(agg.Turns))
	for i, t := range agg.Turns {
		turns[i] = t
	}
	zresp := copyMap(agg.ZeusResponse)
	errors := make([]string, 0)
	posts := 0
	liveCST := handle.ContractStatus
	if client == nil {
		errors = append(errors, "session client not wired")
		out := SessionTraceProjectResult{
			OK:             false,
			PrimaryReqID:   agg.PrimaryReqID,
			PreferredReqID: agg.PrimaryReqID,
			PostOrder:      append([]string(nil), postOrder...),
			Posts:          0,
			Errors:         errors,
			ContractStatus: liveCST,
			Aggregate:      zresp,
		}
		noteProjectorError(opts.Journal, opts.TurnID, handle.SessionID, out)
		return out
	}
	for _, rid := range postOrder {
		if err := ctx.Err(); err != nil {
			errors = append(errors, fmt.Sprintf("trace %s exception: %v", rid, err))
			continue
		}
		result, err := client.PostTrace(ctx, zeushttp.SessionTraceRequest{
			SessionID:    handle.SessionID,
			ClientRound:  handle.Round,
			ReqID:        rid,
			ContractID:   handle.ContractID,
			ContractHash: handle.ContractHash,
			ChatRequest:  chatReq,
			Turns:        turns,
			ZeusResponse: zresp,
			Outcome:      agg.Outcome,
			Headers:      opts.Headers,
			Mode:         mode,
			Target:       opts.Target,
			Rewind:       opts.Rewind,
			TurnID:       opts.TurnID,
		})
		if err != nil {
			errors = append(errors, fmt.Sprintf("trace %s exception: %v", rid, err))
			continue
		}
		if result.OK {
			posts++
			if result.Body != nil {
				if cst := asString(result.Body["contract_status"]); cst != "" {
					liveCST = cst
				}
			}
			emitProject(opts.Log, "info", "zeus_client.session.trace", map[string]any{
				"session.id":       handle.SessionID,
				"req_id":           rid,
				"http.status_code": result.StatusCode,
				"result":           "ok",
			})
			continue
		}
		errPart := result.Error
		if errPart == "" {
			errPart = fmt.Sprint(result.Body)
			if len(errPart) > 120 {
				errPart = errPart[:120]
			}
		}
		errors = append(errors, fmt.Sprintf("trace %s -> %d: %s", rid, result.StatusCode, errPart))
		emitProject(opts.Log, "error", "zeus_client.session.trace", map[string]any{
			"session.id":       handle.SessionID,
			"req_id":           rid,
			"http.status_code": result.StatusCode,
			"result":           "error",
		})
	}
	out := SessionTraceProjectResult{
		OK:             len(errors) == 0,
		PrimaryReqID:   agg.PrimaryReqID,
		PreferredReqID: agg.PrimaryReqID,
		PostOrder:      append([]string(nil), postOrder...),
		Posts:          posts,
		Errors:         errors,
		ContractStatus: liveCST,
		Aggregate:      zresp,
	}
	if len(errors) > 0 {
		noteProjectorError(opts.Journal, opts.TurnID, handle.SessionID, out)
	}
	return out
}

func noteProjectorError(j journal.ExecutionJournal, turnID, sessionID string, result SessionTraceProjectResult) {
	if j == nil {
		return
	}
	j.Append(journal.JournalEvent{
		EventID:   journal.NewEventID(),
		TsMs:      time.Now().UnixMilli(),
		Type:      journal.EventNote,
		Component: projectorComponent,
		TurnID:    turnID,
		Data: map[string]any{
			"kind":           "projector.session_trace",
			"ok":             false,
			"session.id":     sessionID,
			"primary_req_id": result.PrimaryReqID,
			"errors":         append([]string(nil), result.Errors...),
			"posts":          result.Posts,
		},
	})
}

func emitProject(logf func(level, msg string, attrs map[string]any), level, msg string, attrs map[string]any) {
	if logf != nil {
		logf(level, msg, attrs)
	}
}

func clipSnippet(s string) string {
	if len(s) > TraceSnippetMax {
		return s[:TraceSnippetMax]
	}
	return s
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int8:
		return int(x), true
	case int16:
		return int(x), true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case uint:
		return int(x), true
	case uint64:
		return int(x), true
	case float32:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			f, err2 := x.Float64()
			if err2 != nil {
				return 0, false
			}
			return int(f), true
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asSlice(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []map[string]any:
		out := make([]any, len(x))
		for i, m := range x {
			out[i] = m
		}
		return out, true
	default:
		return nil, false
	}
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func truthy(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case string:
		return x != ""
	case bool:
		return x
	case []any:
		return len(x) > 0
	case []map[string]any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	default:
		return true
	}
}
