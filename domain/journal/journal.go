// SPDX-License-Identifier: BUSL-1.1

package journal

import (
	"sync"

	"github.com/koten-ai/zeus_client_golang/security"
)

// ExecutionJournal is the append-only SoT (Python ExecutionJournal).
type ExecutionJournal interface {
	Append(event JournalEvent)
	Events() []JournalEvent
	GetPayload(ref string) (data []byte, ok bool)
	Export() JournalExport
}

// InMemoryJournal is a mutex-protected event list plus PayloadStore.
type InMemoryJournal struct {
	mu    sync.Mutex
	evs   []JournalEvent
	store PayloadStore
}

var _ ExecutionJournal = (*InMemoryJournal)(nil)

// NewInMemoryJournal returns a journal. A nil store uses InMemoryPayloadStore.
func NewInMemoryJournal(store PayloadStore) *InMemoryJournal {
	if store == nil {
		store = NewInMemoryPayloadStore()
	}
	return &InMemoryJournal{store: store}
}

// PayloadStore returns the backing blob store.
func (j *InMemoryJournal) PayloadStore() PayloadStore {
	return j.store
}

// Append copies data / payload_refs and stores the frozen row.
func (j *InMemoryJournal) Append(event JournalEvent) {
	frozen := freezeEvent(event)
	j.mu.Lock()
	j.evs = append(j.evs, frozen)
	j.mu.Unlock()
}

// Events returns a snapshot (copied slice and per-event maps/refs).
func (j *InMemoryJournal) Events() []JournalEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]JournalEvent, len(j.evs))
	for i, e := range j.evs {
		out[i] = freezeEvent(e)
	}
	return out
}

// GetPayload copies stored bytes. Missing refs return ok=false.
func (j *InMemoryJournal) GetPayload(ref string) ([]byte, bool) {
	if j.store == nil {
		return nil, false
	}
	return j.store.Get(ref)
}

// PutPayload stores bytes and returns sha256:<hex> (Python put_payload).
func (j *InMemoryJournal) PutPayload(data []byte, contentType, kind string) string {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if kind == "" {
		kind = "blob"
	}
	return j.store.Put(data, contentType, kind)
}

// PayloadItems snapshots payload metadata for export.
func (j *InMemoryJournal) PayloadItems() map[string]PayloadMeta {
	if j.store == nil {
		return map[string]PayloadMeta{}
	}
	return j.store.Items()
}

// Export is the unredacted schema-v1 snapshot (Python export_journal).
func (j *InMemoryJournal) Export() JournalExport {
	return snapshotExport(j, nil)
}

// ExportRedacted walks event data through the ZCG-5 JSONValue hook.
func (j *InMemoryJournal) ExportRedacted(red security.Redactor) JournalExport {
	return ExportRedacted(j, red)
}
