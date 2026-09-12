// SPDX-License-Identifier: BUSL-1.1

package observability

import (
	"sort"
	"strings"
	"sync"
)

// MetricsPort is process-local counters + histograms (Python MetricsPort).
// Labels stay low-cardinality.
type MetricsPort interface {
	Incr(name string, labels map[string]string, amount float64)
	Observe(name string, value float64, labels map[string]string)
	Snapshot() map[string]any
}

func labelKeyOf(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

type seriesPoint struct {
	labels map[string]string
	value  float64
	count  int
}

// InMemoryMetrics is a thread-safe in-process sink (Python InMemoryMetrics).
type InMemoryMetrics struct {
	mu      sync.Mutex
	counter map[string]map[string]*seriesPoint
	hist    map[string]map[string]*seriesPoint
}

// NewInMemoryMetrics constructs an empty sink.
func NewInMemoryMetrics() *InMemoryMetrics {
	return &InMemoryMetrics{
		counter: map[string]map[string]*seriesPoint{},
		hist:    map[string]map[string]*seriesPoint{},
	}
}

func (m *InMemoryMetrics) Incr(name string, labels map[string]string, amount float64) {
	if m == nil {
		return
	}
	key := labelKeyOf(labels)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.counter[name] == nil {
		m.counter[name] = map[string]*seriesPoint{}
	}
	p := m.counter[name][key]
	if p == nil {
		p = &seriesPoint{labels: copyLabels(labels)}
		m.counter[name][key] = p
	}
	p.value += amount
}

func (m *InMemoryMetrics) Observe(name string, value float64, labels map[string]string) {
	if m == nil {
		return
	}
	key := labelKeyOf(labels)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hist[name] == nil {
		m.hist[name] = map[string]*seriesPoint{}
	}
	p := m.hist[name][key]
	if p == nil {
		p = &seriesPoint{labels: copyLabels(labels)}
		m.hist[name][key] = p
	}
	p.value += value
	p.count++
}

func (m *InMemoryMetrics) Snapshot() map[string]any {
	if m == nil {
		return map[string]any{"counters": map[string]any{}, "histograms": map[string]any{}}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	counters := map[string]any{}
	for name, series := range m.counter {
		rows := make([]any, 0, len(series))
		for _, p := range series {
			rows = append(rows, map[string]any{"labels": copyLabels(p.labels), "value": p.value})
		}
		counters[name] = rows
	}
	histograms := map[string]any{}
	for name, series := range m.hist {
		rows := make([]any, 0, len(series))
		for _, p := range series {
			avg := 0.0
			if p.count > 0 {
				avg = p.value / float64(p.count)
			}
			rows = append(rows, map[string]any{
				"labels": copyLabels(p.labels),
				"sum":    p.value,
				"count":  p.count,
				"avg":    avg,
			})
		}
		histograms[name] = rows
	}
	return map[string]any{"counters": counters, "histograms": histograms}
}

func copyLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// NullMetrics is a no-op sink (Python NullMetrics).
type NullMetrics struct{}

func (NullMetrics) Incr(string, map[string]string, float64)    {}
func (NullMetrics) Observe(string, float64, map[string]string) {}
func (NullMetrics) Snapshot() map[string]any {
	return map[string]any{"counters": map[string]any{}, "histograms": map[string]any{}}
}
