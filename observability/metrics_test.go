// SPDX-License-Identifier: BUSL-1.1

package observability

import "testing"

func TestMetricsIncrAndSnapshot(t *testing.T) {
	m := NewInMemoryMetrics()
	m.Incr("zeus_client_turns_total", map[string]string{"status": "ok", "mode": "analytics"}, 1)
	m.Incr("zeus_client_turns_total", map[string]string{"status": "ok", "mode": "analytics"}, 1)
	m.Observe("zeus_client_zeus_hop_latency_ms", 12.5, map[string]string{"verb": "find"})
	snap := m.Snapshot()
	counters, _ := snap["counters"].(map[string]any)
	rows, _ := counters["zeus_client_turns_total"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
	row, _ := rows[0].(map[string]any)
	if row["value"] != 2.0 {
		t.Fatalf("value %v", row["value"])
	}
	hists, _ := snap["histograms"].(map[string]any)
	hrows, _ := hists["zeus_client_zeus_hop_latency_ms"].([]any)
	if len(hrows) != 1 {
		t.Fatalf("hist %v", hrows)
	}
	h, _ := hrows[0].(map[string]any)
	if h["count"] != 1 {
		t.Fatalf("count %v", h["count"])
	}
}

func TestNullMetricsNoop(t *testing.T) {
	var m MetricsPort = NullMetrics{}
	m.Incr("x", nil, 1)
	m.Observe("y", 1, nil)
	snap := m.Snapshot()
	counters, _ := snap["counters"].(map[string]any)
	if len(counters) != 0 {
		t.Fatalf("%v", snap)
	}
}
