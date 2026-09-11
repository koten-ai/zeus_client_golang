// SPDX-License-Identifier: BUSL-1.1

package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// PayloadStore is a content-addressed blob store (Python PayloadStore).
type PayloadStore interface {
	Put(data []byte, contentType, kind string) string
	Get(ref string) (data []byte, ok bool)
	Meta(ref string) (PayloadRecord, bool)
	Items() map[string]PayloadMeta
}

// PayloadMeta is export metadata for one blob (no bytes).
type PayloadMeta struct {
	Ref         string `json:"ref"`
	ContentType string `json:"content_type"`
	Kind        string `json:"kind"`
	SHA256      string `json:"sha256"`
	Size        int    `json:"size"`
}

// PayloadRecord is stored metadata plus a copy of the bytes (Python PayloadRecord).
type PayloadRecord struct {
	PayloadMeta
	Data []byte
}

type storedPayload struct {
	meta PayloadMeta
	data []byte
}

// InMemoryPayloadStore is a process-local SHA-256 store.
// Same bytes always yield the same sha256:<hex> ref (first write wins metadata).
type InMemoryPayloadStore struct {
	mu    sync.Mutex
	byRef map[string]storedPayload
}

var _ PayloadStore = (*InMemoryPayloadStore)(nil)

// NewInMemoryPayloadStore returns an empty store.
func NewInMemoryPayloadStore() *InMemoryPayloadStore {
	return &InMemoryPayloadStore{byRef: make(map[string]storedPayload)}
}

// Put stores data under sha256:<hex>. Identical bytes return the existing ref.
func (s *InMemoryPayloadStore) Put(data []byte, contentType, kind string) string {
	raw := make([]byte, len(data))
	copy(raw, data)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	ref := "sha256:" + digest

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byRef == nil {
		s.byRef = make(map[string]storedPayload)
	}
	if _, ok := s.byRef[ref]; ok {
		return ref
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if kind == "" {
		kind = "blob"
	}
	s.byRef[ref] = storedPayload{
		meta: PayloadMeta{
			Ref:         ref,
			ContentType: contentType,
			Kind:        kind,
			SHA256:      digest,
			Size:        len(raw),
		},
		data: raw,
	}
	return ref
}

// Get copies stored bytes. Missing refs return ok=false.
func (s *InMemoryPayloadStore) Get(ref string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byRef[ref]
	if !ok {
		return nil, false
	}
	out := make([]byte, len(rec.data))
	copy(out, rec.data)
	return out, true
}

// Meta copies the record (including bytes). Missing refs return ok=false.
func (s *InMemoryPayloadStore) Meta(ref string) (PayloadRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byRef[ref]
	if !ok {
		return PayloadRecord{}, false
	}
	data := make([]byte, len(rec.data))
	copy(data, rec.data)
	return PayloadRecord{PayloadMeta: rec.meta, Data: data}, true
}

// Items returns a snapshot of payload metadata (no bytes).
func (s *InMemoryPayloadStore) Items() map[string]PayloadMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]PayloadMeta, len(s.byRef))
	for k, rec := range s.byRef {
		out[k] = rec.meta
	}
	return out
}
