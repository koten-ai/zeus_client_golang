// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// EventSchemaVersion matches koten_multi_agent_golang multiagent.EventSchemaVersion.
const EventSchemaVersion = 1

// Wire field names (do not fork): schema, seq, job_id, ts, type, plan_epoch,
// unit_id, status, stage, pct, summary, trace_id, duration_ms, dead_end, extra.

var wireFixed = map[string]struct{}{
	"schema": {}, "seq": {}, "job_id": {}, "ts": {}, "ts_ms": {},
	"type": {}, "plan_epoch": {}, "unit_id": {}, "wave": {}, "extra": {},
}

type wireEvent struct {
	Schema     int             `json:"schema,omitempty"`
	Seq        uint64          `json:"seq"`
	JobID      string          `json:"job_id"`
	TS         json.RawMessage `json:"ts,omitempty"`
	Type       string          `json:"type"`
	PlanEpoch  int             `json:"plan_epoch,omitempty"`
	UnitID     string          `json:"unit_id,omitempty"`
	Status     string          `json:"status,omitempty"`
	Stage      string          `json:"stage,omitempty"`
	Pct        float64         `json:"pct,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	TraceID    string          `json:"trace_id,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	DeadEnd    bool            `json:"dead_end,omitempty"`
	Extra      map[string]any  `json:"extra,omitempty"`
	Wave       int             `json:"wave,omitempty"`
}

func cloneAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func tsMSFromWire(raw json.RawMessage) int64 {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			return 0
		}
		s = strings.Replace(s, "Z", "+00:00", 1)
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t, err = time.Parse(time.RFC3339, s)
		}
		if err != nil {
			return 0
		}
		return t.UnixMilli()
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	if n > 1e12 {
		return int64(n) // already ms
	}
	if n == float64(int64(n)) && n < 1e11 {
		// Python tests use ts: 0 / 1 as epoch ms (not seconds).
		return int64(n)
	}
	return int64(n * 1000)
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

// EventFromWire maps a Go JobEvent JSON object to ports.JobEvent (Python job_event_from_wire).
func EventFromWire(data map[string]any) (ports.JobEvent, error) {
	if data == nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse", domain.WithCause(err))
	}
	var w wireEvent
	if err := json.Unmarshal(raw, &w); err != nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse", domain.WithCause(err))
	}
	payload := map[string]any{}
	if len(w.Extra) > 0 {
		payload = cloneAnyMap(w.Extra)
	} else {
		for k, v := range data {
			if _, skip := wireFixed[k]; skip {
				continue
			}
			payload[k] = v
		}
	}
	if w.Status != "" {
		if _, ok := payload["status"]; !ok {
			payload["status"] = w.Status
		}
	}
	if w.Stage != "" {
		if _, ok := payload["stage"]; !ok {
			payload["stage"] = w.Stage
		}
	}
	if w.Summary != "" {
		if _, ok := payload["summary"]; !ok {
			payload["summary"] = w.Summary
		}
	}
	if w.TraceID != "" {
		if _, ok := payload["trace_id"]; !ok {
			payload["trace_id"] = w.TraceID
		}
	}
	if w.Pct != 0 {
		if _, ok := payload["pct"]; !ok {
			payload["pct"] = w.Pct
		}
	}
	if w.DurationMS != 0 {
		if _, ok := payload["duration_ms"]; !ok {
			payload["duration_ms"] = w.DurationMS
		}
	}
	if w.DeadEnd {
		if _, ok := payload["dead_end"]; !ok {
			payload["dead_end"] = true
		}
	}
	wave := w.Wave
	if wave == 0 {
		wave = int(asInt64(payload["wave"]))
	}
	return ports.JobEvent{
		Seq:       int(w.Seq),
		Type:      w.Type,
		JobID:     w.JobID,
		TSMS:      tsMSFromWire(w.TS),
		UnitID:    w.UnitID,
		Wave:      wave,
		PlanEpoch: w.PlanEpoch,
		Payload:   payload,
	}, nil
}

// EventToWire is the minified Go JobEvent object (no secrets).
func EventToWire(ev ports.JobEvent) map[string]any {
	payload := cloneAnyMap(ev.Payload)
	w := map[string]any{
		"schema": EventSchemaVersion,
		"seq":    ev.Seq,
		"job_id": ev.JobID,
		"type":   ev.Type,
	}
	if ev.TSMS == 0 {
		w["ts"] = 0
	} else {
		w["ts"] = time.UnixMilli(ev.TSMS).UTC().Format(time.RFC3339Nano)
	}
	if ev.PlanEpoch != 0 {
		w["plan_epoch"] = ev.PlanEpoch
	}
	if ev.UnitID != "" {
		w["unit_id"] = ev.UnitID
	}
	if ev.Wave != 0 {
		w["wave"] = ev.Wave
	}
	lift := []string{"status", "stage", "pct", "summary", "trace_id", "duration_ms", "dead_end"}
	for _, k := range lift {
		if v, ok := payload[k]; ok {
			w[k] = v
			delete(payload, k)
		}
	}
	delete(payload, "wave")
	if len(payload) > 0 {
		w["extra"] = payload
	}
	return w
}

func eventToJSON(ev ports.JobEvent) ([]byte, error) {
	return json.Marshal(EventToWire(ev))
}

func eventFromJSON(raw []byte) (ports.JobEvent, error) {
	var data map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse", domain.WithCause(err))
	}
	if data == nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse")
	}
	// json.Number → float/string as EventFromWire expects via marshal roundtrip
	normalized := map[string]any{}
	buf, err := json.Marshal(data)
	if err != nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse", domain.WithCause(err))
	}
	if err := json.Unmarshal(buf, &normalized); err != nil {
		return ports.JobEvent{}, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse", domain.WithCause(err))
	}
	return EventFromWire(normalized)
}

func seqString(n int) string {
	return strconv.Itoa(n)
}
