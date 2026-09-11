// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"context"
	"errors"
	"fmt"
)

// Error is the family client error (GO_CLIENT_BOOTSTRAP Result envelope).
// error unwraps to *Error with Code as a six-digit string.
type Error struct {
	Code         Code
	Message      string
	Type         string
	Component    string
	Retryable    bool
	CauseEventID string
	Details      map[string]any
	Cause        error
}

// Option mutates Error at construction.
type Option func(*Error)

// WithMessage overrides the catalogue English (still no secrets).
func WithMessage(msg string) Option {
	return func(e *Error) { e.Message = msg }
}

// WithRetryable overrides DefaultRetryable(code).
func WithRetryable(retryable bool) Option {
	return func(e *Error) { e.Retryable = retryable }
}

// WithCauseEventID stores a journal / event ref (Python cause_event_id).
func WithCauseEventID(id string) Option {
	return func(e *Error) { e.CauseEventID = id }
}

// WithDetails copies attrs (http.status_code, …). Keys are not secrets.
func WithDetails(d map[string]any) Option {
	return func(e *Error) {
		if len(d) == 0 {
			return
		}
		e.Details = make(map[string]any, len(d))
		for k, v := range d {
			e.Details[k] = v
		}
	}
}

// WithCause wraps a lower-level error (ctx, net, provider).
func WithCause(err error) Option {
	return func(e *Error) { e.Cause = err }
}

// WithType overrides the catalogue error.type slug.
func WithType(typ string) Option {
	return func(e *Error) { e.Type = typ }
}

// New builds *Error with catalogue message, type, and retryable defaults.
func New(code Code, component string, opts ...Option) *Error {
	e := &Error{
		Code:      code,
		Message:   PublicMessage(code),
		Type:      defaultType(code),
		Component: component,
		Retryable: DefaultRetryable(code),
		Details:   map[string]any{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	return e
}

func typed(typ string, code Code, component string, opts ...Option) *Error {
	e := New(code, component, opts...)
	e.Type = typ
	return e
}

// Typed constructors match the Python ZeusClientError subclasses.

func NewConfig(code Code, component string, opts ...Option) *Error {
	return typed("config", code, component, opts...)
}

func NewAuth(code Code, component string, opts ...Option) *Error {
	return typed("auth", code, component, opts...)
}

func NewContract(code Code, component string, opts ...Option) *Error {
	return typed("contract", code, component, opts...)
}

func NewCatalog(code Code, component string, opts ...Option) *Error {
	return typed("catalog", code, component, opts...)
}

func NewSession(code Code, component string, opts ...Option) *Error {
	return typed("session", code, component, opts...)
}

func NewZeusTransport(code Code, component string, opts ...Option) *Error {
	return typed("zeus_http", code, component, opts...)
}

func NewZeusTool(code Code, component string, opts ...Option) *Error {
	return typed("zeus_tool", code, component, opts...)
}

func NewLLM(code Code, component string, opts ...Option) *Error {
	return typed("llm", code, component, opts...)
}

func NewPolicy(code Code, component string, opts ...Option) *Error {
	return typed("policy", code, component, opts...)
}

func NewValidation(code Code, component string, opts ...Option) *Error {
	return typed("invalid_argument", code, component, opts...)
}

func NewInternal(code Code, component string, opts ...Option) *Error {
	return typed("internal", code, component, opts...)
}

func NewJob(code Code, component string, opts ...Option) *Error {
	return typed("jobs", code, component, opts...)
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Map is the family wire shape (Python ZeusClientError.to_dict + error.type).
func (e *Error) Map() map[string]any {
	if e == nil {
		return nil
	}
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	return map[string]any{
		"error.code":           string(e.Code),
		"error.message":        e.Message,
		"error.type":           e.Type,
		"error.retryable":      e.Retryable,
		"error.component":      e.Component,
		"error.cause_event_id": e.CauseEventID,
		"error.details":        details,
	}
}

// AsError unwraps err to *Error (false if none).
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// FromContext maps context.Canceled → 000006 and DeadlineExceeded → 000010.
// Other errors return nil (caller keeps the original).
func FromContext(err error) *Error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return New(CodeCancelled, "context", WithCause(err))
	case errors.Is(err, context.DeadlineExceeded):
		return New(CodeContextDeadline, "context", WithCause(err))
	default:
		return nil
	}
}
