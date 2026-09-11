// SPDX-License-Identifier: BUSL-1.1

package domain

// Result is the family L0 envelope (API_RESULT / GO_CLIENT_BOOTSTRAP).
// JSON/map shape: {result, value?, error?, req_id?, req_ids?, meta?}.
type Result[T any] struct {
	OK     bool
	Value  T
	Err    *Error
	ReqID  string
	ReqIDs []string
	Meta   map[string]any
}

// OKResult is a successful envelope. reqID may be empty for local calls.
func OKResult[T any](value T, reqID string) Result[T] {
	r := Result[T]{OK: true, Value: value, ReqID: reqID}
	if reqID != "" {
		r.ReqIDs = []string{reqID}
	}
	return r
}

// FailResult is a failed envelope. reqID is required after a Zeus hop.
func FailResult[T any](err *Error, reqID string) Result[T] {
	r := Result[T]{OK: false, Err: err, ReqID: reqID}
	if reqID != "" {
		r.ReqIDs = []string{reqID}
	}
	return r
}

// Unwrap is the Go idiom: (value, error) with error unwrapping to *Error.
func (r Result[T]) Unwrap() (T, error) {
	var zero T
	if r.OK {
		return r.Value, nil
	}
	if r.Err != nil {
		return zero, r.Err
	}
	return zero, New(CodeUnknown, "result")
}

// Map is the conceptual envelope for suite/conformance field asserts.
func (r Result[T]) Map() map[string]any {
	out := map[string]any{"result": r.OK}
	if r.OK {
		out["value"] = r.Value
	} else if r.Err != nil {
		out["error"] = r.Err.Object()
	}
	if r.ReqID != "" {
		out["req_id"] = r.ReqID
	}
	if len(r.ReqIDs) > 0 {
		ids := make([]string, len(r.ReqIDs))
		copy(ids, r.ReqIDs)
		out["req_ids"] = ids
	}
	if len(r.Meta) > 0 {
		meta := make(map[string]any, len(r.Meta))
		for k, v := range r.Meta {
			meta[k] = v
		}
		out["meta"] = meta
	}
	return out
}
