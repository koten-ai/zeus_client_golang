// SPDX-License-Identifier: BUSL-1.1

package journal

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/koten-ai/zeus_client_golang/security"
)

func evt(eventID string, ts int64, typ string, extra map[string]any) JournalEvent {
	data := map[string]any{"n": eventID}
	if extra != nil {
		data = extra
	}
	return JournalEvent{
		EventID:   eventID,
		TsMs:      ts,
		Type:      typ,
		Component: "test",
		TurnID:    "turn_1",
		Data:      data,
	}
}

func TestJournalAppendOrder(t *testing.T) {
	j := NewInMemoryJournal(nil)
	j.Append(evt("e1", 10, EventTurnStarted, nil))
	j.Append(evt("e2", 20, EventNote, nil))
	j.Append(evt("e3", 30, EventTurnCompleted, nil))
	ids := make([]string, 0, 3)
	for _, e := range j.Events() {
		ids = append(ids, e.EventID)
	}
	if got := strings.Join(ids, ","); got != "e1,e2,e3" {
		t.Fatalf("order: %q", got)
	}
}

func TestEventsSnapshotIsolation(t *testing.T) {
	j := NewInMemoryJournal(nil)
	j.Append(evt("e1", 1, EventNote, nil))
	snap := j.Events()
	if len(snap) != 1 {
		t.Fatalf("len %d", len(snap))
	}
	snap[0].Data["n"] = "mutated"
	snap[0].PayloadRefs = append(snap[0].PayloadRefs, "x")
	j.Append(evt("e2", 2, EventNote, nil))
	if len(snap) != 1 {
		t.Fatal("snapshot slice aliased inner events")
	}
	again := j.Events()
	if len(again) != 2 {
		t.Fatalf("len after append %d", len(again))
	}
	if again[0].Data["n"] != "e1" {
		t.Fatalf("stored data mutated: %v", again[0].Data["n"])
	}
	if len(again[0].PayloadRefs) != 0 {
		t.Fatalf("payload_refs leaked: %v", again[0].PayloadRefs)
	}
}

func TestAppendCopiesCallerData(t *testing.T) {
	j := NewInMemoryJournal(nil)
	data := map[string]any{"n": "e1"}
	refs := []string{"sha256:abc"}
	j.Append(JournalEvent{
		EventID:     "e1",
		TsMs:        1,
		Type:        EventNote,
		Component:   "test",
		TurnID:      "t1",
		Data:        data,
		PayloadRefs: refs,
	})
	data["n"] = "mutated"
	refs[0] = "mutated"
	got := j.Events()[0]
	if got.Data["n"] != "e1" {
		t.Fatalf("data: %v", got.Data["n"])
	}
	if got.PayloadRefs[0] != "sha256:abc" {
		t.Fatalf("refs: %v", got.PayloadRefs)
	}
}

func TestPayloadRefDedup(t *testing.T) {
	store := NewInMemoryPayloadStore()
	body := []byte(`{"hello":"world"}`)
	r1 := store.Put(body, "application/json", "http_body")
	r2 := store.Put(body, "application/json", "http_body")
	if r1 != r2 {
		t.Fatalf("%q vs %q", r1, r2)
	}
	if !strings.HasPrefix(r1, "sha256:") {
		t.Fatalf("ref %q", r1)
	}
	got, ok := store.Get(r1)
	if !ok || string(got) != string(body) {
		t.Fatalf("get %v %q", ok, got)
	}
	other := store.Put([]byte("other"), "text/plain", "note")
	if other == r1 {
		t.Fatal("different bytes must not collide")
	}
	// Python: same bytes, different kind — still same ref; first write wins meta.
	r3 := store.Put(body, "text/plain", "note")
	if r3 != r1 {
		t.Fatalf("dedup ignores kind: %q", r3)
	}
	meta, ok := store.Meta(r1)
	if !ok || meta.Kind != "http_body" || meta.ContentType != "application/json" {
		t.Fatalf("first-write meta: %+v ok=%v", meta, ok)
	}
}

func TestPayloadGetMissing(t *testing.T) {
	store := NewInMemoryPayloadStore()
	if _, ok := store.Get("sha256:deadbeef"); ok {
		t.Fatal("missing should be ok=false")
	}
	j := NewInMemoryJournal(store)
	if _, ok := j.GetPayload("sha256:deadbeef"); ok {
		t.Fatal("journal missing should be ok=false")
	}
}

func TestPayloadPutCopiesBytes(t *testing.T) {
	store := NewInMemoryPayloadStore()
	body := []byte("abc")
	ref := store.Put(body, "text/plain", "note")
	body[0] = 'Z'
	got, ok := store.Get(ref)
	if !ok || string(got) != "abc" {
		t.Fatalf("store aliased input: %q ok=%v", got, ok)
	}
	got[0] = 'Q'
	again, _ := store.Get(ref)
	if string(again) != "abc" {
		t.Fatalf("get aliased storage: %q", again)
	}
}

func TestExportSchemaV1(t *testing.T) {
	j := NewInMemoryJournal(nil)
	ref := j.PutPayload([]byte("abc"), "text/plain", "note")
	j.Append(JournalEvent{
		EventID:     "e1",
		TsMs:        1,
		Type:        EventNote,
		Component:   "test",
		TurnID:      "t1",
		Data:        map[string]any{"msg": "hi"},
		PayloadRefs: []string{ref},
	})
	exp := j.Export()
	d := exp.ToMap()
	if d["journal_schema"] != JournalSchemaVersion || JournalSchemaVersion != 1 {
		t.Fatalf("schema %v", d["journal_schema"])
	}
	events, _ := d["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events %d", len(events))
	}
	row := events[0].(map[string]any)
	if row["type"] != EventNote {
		t.Fatalf("type %v", row["type"])
	}
	refs, _ := row["payload_refs"].([]string)
	if len(refs) != 1 || refs[0] != ref {
		t.Fatalf("payload_refs %v", row["payload_refs"])
	}
	payloads := d["payloads"].(map[string]any)
	meta := payloads[ref].(map[string]any)
	if meta["size"] != 3 {
		t.Fatalf("size %v", meta["size"])
	}
	if meta["sha256"] == "" || meta["sha256"] == nil {
		t.Fatal("sha256 missing")
	}
	if _, hasData := meta["data"]; hasData {
		t.Fatal("export payloads must not include bytes")
	}
	blob, err := json.Marshal(exp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "UNIQUE_SHOULD_NOT") {
		t.Fatal("unexpected")
	}
	secretBody := []byte("UNIQUE_PAYLOAD_BODY_xyz")
	ref2 := j.PutPayload(secretBody, "text/plain", "note")
	j.Append(JournalEvent{
		EventID:     "e2",
		TsMs:        2,
		Type:        EventNote,
		Component:   "test",
		TurnID:      "t1",
		PayloadRefs: []string{ref2},
	})
	blob2, _ := json.Marshal(j.Export())
	if strings.Contains(string(blob2), "UNIQUE_PAYLOAD_BODY_xyz") {
		t.Fatal("payload bytes leaked into export")
	}
}

func TestSpanParentLinkage(t *testing.T) {
	j := NewInMemoryJournal(nil)
	tms := int64(1000)
	clock := func() int64 {
		tms++
		return tms
	}
	tr := NewSpanTracer(j, "turn_x", clock)
	root := tr.Start("turn", "", nil)
	child := tr.Start("zeus.hop", "", nil)
	tr.End(child, "ok", nil)
	tr.End(root, "ok", nil)

	events := j.Events()
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	want := []string{EventSpanStarted, EventSpanStarted, EventSpanEnded, EventSpanEnded}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("types %v", types)
	}
	if events[0].SpanID != root.SpanID || events[0].ParentSpanID != "" {
		t.Fatalf("root start %+v handle %+v", events[0], root)
	}
	if events[1].SpanID != child.SpanID || events[1].ParentSpanID != root.SpanID {
		t.Fatalf("child start %+v", events[1])
	}
	if events[2].SpanID != child.SpanID || events[2].ParentSpanID != root.SpanID {
		t.Fatalf("child end %+v", events[2])
	}
	if events[3].SpanID != root.SpanID {
		t.Fatalf("root end %+v", events[3])
	}
	if !strings.HasPrefix(root.SpanID, "span_") || len(root.SpanID) != len("span_")+16 {
		t.Fatalf("span_id %q", root.SpanID)
	}
	if !strings.HasPrefix(events[0].EventID, "evt_") || len(events[0].EventID) != len("evt_")+16 {
		t.Fatalf("event_id %q", events[0].EventID)
	}
}

func TestJobAndUnitLifecycleEventTypes(t *testing.T) {
	if EventJobStarted != "job.started" || EventJobFinished != "job.finished" {
		t.Fatal("job types")
	}
	if EventUnitStarted != "unit.started" || EventUnitFinished != "unit.finished" {
		t.Fatal("unit types")
	}
	j := NewInMemoryJournal(nil)
	j.Append(evt("j1", 1, EventJobStarted, map[string]any{"job_id": "job_1", "llm.role": "worker"}))
	j.Append(evt("u1", 2, EventUnitStarted, map[string]any{"unit_id": "u1", "llm.model": "fast"}))
	j.Append(evt("u2", 3, EventUnitFinished, map[string]any{"unit_id": "u1", "status": "ok"}))
	j.Append(evt("j2", 4, EventJobFinished, map[string]any{"job_id": "job_1", "status": "ok"}))
	types := make([]string, 0, 4)
	var dumped strings.Builder
	for _, e := range j.Events() {
		types = append(types, e.Type)
		fmt.Fprintf(&dumped, "%v", e.Data)
	}
	want := strings.Join([]string{EventJobStarted, EventUnitStarted, EventUnitFinished, EventJobFinished}, ",")
	if strings.Join(types, ",") != want {
		t.Fatalf("types %v", types)
	}
	s := dumped.String()
	if strings.Contains(s, "password") || strings.Contains(s, "sk-") {
		t.Fatalf("unexpected secret in %q", s)
	}
}

func TestExportRedactsSensitiveKeys(t *testing.T) {
	j := NewInMemoryJournal(nil)
	j.Append(JournalEvent{
		EventID:   "e1",
		TsMs:      1,
		Type:      EventTurnStarted,
		Component: "test",
		TurnID:    "turn_a",
		Data:      map[string]any{"msg": "start"},
	})
	tr := NewSpanTracer(j, "turn_a", func() int64 { return 10 })
	h := tr.Start("agent.round", "", nil)
	j.Append(JournalEvent{
		EventID:   "e2",
		TsMs:      11,
		Type:      EventLLMRound,
		Component: "test",
		TurnID:    "turn_a",
		SpanID:    h.SpanID,
		Data:      map[string]any{"ok": true, "Authorization": "Bearer sk-secret-key-value"},
	})
	j.Append(JournalEvent{
		EventID:   "e3",
		TsMs:      12,
		Type:      EventZeusHop,
		Component: "test",
		TurnID:    "turn_a",
		SpanID:    h.SpanID,
		Data:      map[string]any{"req_id": "req-1", "api_key": "should-redact"},
	})
	tr.End(h, "ok", nil)
	j.Append(JournalEvent{
		EventID:   "e4",
		TsMs:      20,
		Type:      EventTurnCompleted,
		Component: "test",
		TurnID:    "turn_a",
		Data:      map[string]any{"status": "ok"},
	})

	rawBlob, err := json.Marshal(j.Export())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawBlob), "should-redact") {
		t.Fatal("internal Export must keep secrets so the redactor hook has work to do")
	}

	exp := ExportRedacted(j, nil)
	d := exp.ToMap()
	if d["journal_schema"] != 1 {
		t.Fatalf("schema %v", d["journal_schema"])
	}
	blob, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	if strings.Contains(s, "sk-secret-key-value") {
		t.Fatalf("bearer leaked: %s", s)
	}
	if strings.Contains(s, "should-redact") {
		t.Fatalf("api_key leaked: %s", s)
	}
	if !strings.Contains(s, security.Redacted) {
		t.Fatalf("missing %s in %s", security.Redacted, s)
	}
	types := make([]string, 0, len(exp.Events))
	for _, e := range exp.Events {
		types = append(types, e.Type)
	}
	if types[0] != EventTurnStarted || types[len(types)-1] != EventTurnCompleted {
		t.Fatalf("sequence %v", types)
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, EventLLMRound) || !strings.Contains(joined, EventZeusHop) {
		t.Fatalf("sequence %v", types)
	}
}

func TestConcurrentAppendExport(t *testing.T) {
	j := NewInMemoryJournal(nil)
	const n = 32
	const per = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; k < per; k++ {
				data := map[string]any{"i": i, "k": k, "api_key": "should-redact"}
				ref := j.PutPayload([]byte(fmt.Sprintf("body-%d-%d", i, k)), "text/plain", "note")
				j.Append(JournalEvent{
					EventID:     fmt.Sprintf("e-%d-%d", i, k),
					TsMs:        int64(i*per + k),
					Type:        EventNote,
					Component:   "test",
					TurnID:      "turn_race",
					Data:        data,
					PayloadRefs: []string{ref},
				})
				data["mutated"] = true
				_ = j.Events()
				_ = j.Export()
				_ = ExportRedacted(j, nil)
				_, _ = j.GetPayload(ref)
			}
		}(i)
	}
	wg.Wait()

	evs := j.Events()
	if len(evs) != n*per {
		t.Fatalf("got %d events want %d", len(evs), n*per)
	}
	for _, e := range evs {
		if _, ok := e.Data["mutated"]; ok {
			t.Fatal("caller mutation leaked into stored event")
		}
		if e.Data["api_key"] != "should-redact" {
			t.Fatalf("stored data: %v", e.Data["api_key"])
		}
	}
	blob, err := json.Marshal(ExportRedacted(j, nil))
	if err != nil {
		t.Fatal(err)
	}
	s := string(blob)
	if strings.Contains(s, "should-redact") {
		t.Fatal("redacted export leaked api_key")
	}
	if !strings.Contains(s, security.Redacted) {
		t.Fatal("redacted export missing marker")
	}

	same := []byte("shared-body")
	var wg2 sync.WaitGroup
	refs := make([]string, n)
	for i := 0; i < n; i++ {
		wg2.Add(1)
		go func(i int) {
			defer wg2.Done()
			refs[i] = j.PutPayload(same, "text/plain", "note")
		}(i)
	}
	wg2.Wait()
	for i := 1; i < n; i++ {
		if refs[i] != refs[0] {
			t.Fatalf("dedup raced: %q vs %q", refs[0], refs[i])
		}
	}
}

func TestEventTypeStrings(t *testing.T) {
	pairs := []struct {
		got, want string
	}{
		{EventTurnStarted, "turn.started"},
		{EventTurnCompleted, "turn.completed"},
		{EventSpanStarted, "span.started"},
		{EventSpanEnded, "span.ended"},
		{EventErrorRaised, "error.raised"},
		{EventNote, "note"},
		{EventZeusHop, "zeus.hop"},
		{EventLLMRound, "llm.round"},
		{EventJobStarted, "job.started"},
		{EventJobFinished, "job.finished"},
		{EventUnitStarted, "unit.started"},
		{EventUnitFinished, "unit.finished"},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("%q != %q", p.got, p.want)
		}
	}
}
