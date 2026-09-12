// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/koten-ai/zeus_client_golang/ports"
)

func TestEventToWireFieldNames(t *testing.T) {
	ev := ports.JobEvent{
		Seq:       3,
		Type:      "unit.finished",
		JobID:     "job_1",
		TSMS:      time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC).UnixMilli(),
		UnitID:    "unit_sales",
		Wave:      1,
		PlanEpoch: 2,
		Payload: map[string]any{
			"status":      "ok",
			"stage":       "run",
			"pct":         1.0,
			"summary":     "shortlist",
			"trace_id":    "tr-1",
			"duration_ms": 12,
			"dead_end":    false,
			"flag":        true,
		},
	}
	w := EventToWire(ev)
	for _, k := range []string{
		"schema", "seq", "job_id", "ts", "type", "plan_epoch", "unit_id",
		"status", "stage", "pct", "summary", "trace_id", "duration_ms",
	} {
		if _, ok := w[k]; !ok {
			t.Fatalf("missing wire field %s in %#v", k, w)
		}
	}
	if w["schema"] != EventSchemaVersion {
		t.Fatalf("schema %v", w["schema"])
	}
	extra, _ := w["extra"].(map[string]any)
	if extra["flag"] != true {
		t.Fatalf("extra %#v", extra)
	}
	if _, ok := extra["status"]; ok {
		t.Fatal("status should be lifted off extra")
	}
	raw, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	got, err := EventFromWire(back)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seq != 3 || got.Type != "unit.finished" || got.JobID != "job_1" || got.UnitID != "unit_sales" || got.PlanEpoch != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Payload["summary"] != "shortlist" {
		t.Fatalf("payload %#v", got.Payload)
	}
}

func TestEventFromWirePythonShapes(t *testing.T) {
	got, err := EventFromWire(map[string]any{
		"seq": 1, "type": "job.started", "job_id": "j1", "ts": 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Seq != 1 || got.Type != "job.started" || got.TSMS != 0 {
		t.Fatalf("%+v", got)
	}
	got, err = EventFromWire(map[string]any{
		"schema": 1, "seq": 2, "job_id": "j1", "ts": "2026-09-12T00:00:00Z",
		"type": "job.finished", "dead_end": true, "status": "partial",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TSMS == 0 || got.Payload["dead_end"] != true || got.Payload["status"] != "partial" {
		t.Fatalf("%+v payload %#v", got, got.Payload)
	}
}
