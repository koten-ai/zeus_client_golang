// SPDX-License-Identifier: BUSL-1.1

package domain

import "strings"

// SessionHandle is a frozen durable-session handle (Python SessionHandle).
// session_id is server-minted — wrap, do not invent.
type SessionHandle struct {
	SessionID      string
	Round          int
	ChatID         string
	ContractID     string
	ContractHash   string
	ContractStatus string
	Mode           string
	Created        bool
	Rehydrated     bool
	RecoveredFrom  string
	Enabled        bool
	CreateReqID    string
	Error          string
}

// WithUpdates returns a copy with selected fields replaced (Python with_updates).
func (h SessionHandle) WithUpdates(upd SessionHandleUpdates) SessionHandle {
	out := h
	if upd.SessionID != nil {
		out.SessionID = *upd.SessionID
	}
	if upd.Round != nil {
		out.Round = *upd.Round
	}
	if upd.ChatID != nil {
		out.ChatID = *upd.ChatID
	}
	if upd.ContractID != nil {
		out.ContractID = *upd.ContractID
	}
	if upd.ContractHash != nil {
		out.ContractHash = *upd.ContractHash
	}
	if upd.ContractStatus != nil {
		out.ContractStatus = *upd.ContractStatus
	}
	if upd.Mode != nil {
		out.Mode = *upd.Mode
	}
	if upd.Created != nil {
		out.Created = *upd.Created
	}
	if upd.Rehydrated != nil {
		out.Rehydrated = *upd.Rehydrated
	}
	if upd.RecoveredFrom != nil {
		out.RecoveredFrom = *upd.RecoveredFrom
	}
	if upd.Enabled != nil {
		out.Enabled = *upd.Enabled
	}
	if upd.CreateReqID != nil {
		out.CreateReqID = *upd.CreateReqID
	}
	if upd.Error != nil {
		out.Error = *upd.Error
	}
	return out
}

// SessionHandleUpdates is a sparse patch for WithUpdates. Nil fields keep the original.
type SessionHandleUpdates struct {
	SessionID      *string
	Round          *int
	ChatID         *string
	ContractID     *string
	ContractHash   *string
	ContractStatus *string
	Mode           *string
	Created        *bool
	Rehydrated     *bool
	RecoveredFrom  *string
	Enabled        *bool
	CreateReqID    *string
	Error          *string
}

// NormalizeContractStatus maps wire "ok" → "match" (Python SessionLifecycle._create).
func NormalizeContractStatus(raw string) string {
	s := raw
	if s == "" {
		return "none"
	}
	if s == "ok" || s == "match" {
		return "match"
	}
	return s
}

// ContractStatusFromRehydrate derives contract_status after GET /v2/session
// (Python contract_status_from_rehydrate).
func ContractStatusFromRehydrate(contractID, sessionHash string, reh map[string]any) string {
	if reh == nil {
		reh = map[string]any{}
	}
	rehHash := asString(reh["hash"])
	rehCID := asString(reh["contract_id"])
	cid := strings.TrimSpace(contractID)
	wantH := strings.TrimSpace(sessionHash)
	if cid == "" && rehCID == "" {
		return "none"
	}
	if wantH != "" && rehHash != "" {
		if wantH == rehHash {
			return "match"
		}
		return "drift"
	}
	if cid != "" || rehCID != "" {
		return "match"
	}
	return "none"
}
