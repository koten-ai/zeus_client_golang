// SPDX-License-Identifier: BUSL-1.1

// Package journal is the append-only execution journal (Python domain.journal).
//
// The journal is the source of truth. Detective, session-trace, and OTLP are
// projectors (not this package). Hot events store payload_refs, not bodies.
//
// InMemoryJournal and InMemoryPayloadStore are mutex-protected. Events and
// Export return snapshots. Mutating a caller's data map after Append does not
// change the stored event.
//
// Export is the unredacted Python export_journal snapshot. ExportRedacted
// walks event data through security.Redactor.JSONValue (ZCG-5). Payload bytes
// stay in the store; export payloads are metadata only.
package journal
