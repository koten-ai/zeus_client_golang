// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Client-minted ids are RFC 4122 UUID v4 (guides/CORRELATION_IDS.md).
// Never reuse a Zeus hop req_id as chat.id. Never invent session.id.

var (
	// ErrEmptyID is returned when a typed id is blank or whitespace.
	ErrEmptyID = errors.New("id must be non-empty")

	// UUIDV4Pattern is version nibble 4, variant 8|9|a|b, lowercase hyphenated.
	UUIDV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// ReqIDHeader is the Zeus hop correlation header. Prefer Zeus mint; read echo.
const ReqIDHeader = "X-Zeus-Req-Id"

// TurnID is one agent/direct turn (client-minted UUID v4).
type TurnID string

// ChatID is a logical conversation id (sticky across turns; not a req_id).
type ChatID string

// CallID is one LLM or tool call within a turn.
type CallID string

// SessionID is a Zeus durable session id (server-minted — wrap, do not invent).
type SessionID string

// ReqID is a Zeus hop correlation id (prefer server X-Zeus-Req-Id).
type ReqID string

func parseID(kind, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("%s: %w", kind, ErrEmptyID)
	}
	return s, nil
}

func ParseTurnID(raw string) (TurnID, error) {
	s, err := parseID("TurnID", raw)
	return TurnID(s), err
}

func ParseChatID(raw string) (ChatID, error) {
	s, err := parseID("ChatID", raw)
	return ChatID(s), err
}

func ParseCallID(raw string) (CallID, error) {
	s, err := parseID("CallID", raw)
	return CallID(s), err
}

func ParseSessionID(raw string) (SessionID, error) {
	s, err := parseID("SessionID", raw)
	return SessionID(s), err
}

func ParseReqID(raw string) (ReqID, error) {
	s, err := parseID("ReqID", raw)
	return ReqID(s), err
}

// NewZeusReqID mints a lowercase hyphenated UUID v4 (Python new_zeus_req_id).
func NewZeusReqID() string {
	return uuid.NewString()
}

// NewTraceID is a W3C 32-hex trace id (not a Zeus req_id).
func NewTraceID() string {
	return hexUUID()
}

// IsUUIDv4 reports whether value is a canonical lowercase RFC 4122 v4 string.
func IsUUIDv4(value string) bool {
	return UUIDV4Pattern.MatchString(strings.TrimSpace(value))
}

func hexUUID() string {
	u := uuid.New()
	return hex.EncodeToString(u[:])
}

// MintTurnID returns a TurnID. Empty prefix → canonical UUID v4. A prefix is
// for tests only and is not a Zeus req_id (Python new_id).
func MintTurnID(prefix string) TurnID {
	if prefix == "" {
		return TurnID(NewZeusReqID())
	}
	return TurnID(prefix + hexUUID())
}

// MintChatID mints a conversation id. Independent of hop req_id.
func MintChatID() ChatID { return ChatID(NewZeusReqID()) }

// MintCallID mints a call id (or wrap the model tool_calls[].id via ParseCallID).
func MintCallID() CallID { return CallID(NewZeusReqID()) }

// MintReqID pre-mints a hop id when the client must log before the response.
func MintReqID() ReqID { return ReqID(NewZeusReqID()) }

// IDFactory is the Python ports.IdFactory surface (mint only; no session.id).
type IDFactory interface {
	TurnID() string
	ChatID() string
	CallID() string
	SpanID() string
}

// UUIDFactory is the default CSPRNG v4 factory (Python UuidIdFactory).
type UUIDFactory struct{}

func (UUIDFactory) TurnID() string { return NewZeusReqID() }
func (UUIDFactory) ChatID() string { return NewZeusReqID() }
func (UUIDFactory) CallID() string { return NewZeusReqID() }
func (UUIDFactory) SpanID() string { return hexUUID()[:16] }
