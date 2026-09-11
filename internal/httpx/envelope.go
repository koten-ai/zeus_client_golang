// SPDX-License-Identifier: BUSL-1.1

package httpx

import (
	"encoding/json"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// Wrap maps a hop (or transport error) to the family result envelope.
// req_id is taken from the response header on every status including 4xx/5xx.
func Wrap(resp ports.HTTPResponse, err error) domain.Result[map[string]any] {
	reqID := domain.ReqIDFromHeaders(resp.Headers)
	if err != nil {
		de, ok := domain.AsError(err)
		if !ok {
			de = domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx", domain.WithCause(err))
		}
		if reqID != "" {
			attachReqID(de, reqID)
		}
		return domain.FailResult[map[string]any](de, reqID)
	}
	body := parseBody(resp.Body)
	if resp.Status >= 200 && resp.Status < 300 {
		return domain.OKResult(body, reqID)
	}
	code := classifyStatus(resp.Status)
	hopErr := domain.NewZeusTransport(code, "internal.httpx",
		domain.WithDetails(map[string]any{
			"http.status_code": resp.Status,
			"req_id":           reqID,
		}),
	)
	return domain.FailResult[map[string]any](hopErr, reqID)
}

func classifyStatus(status int) domain.Code {
	switch {
	case status == 409:
		return domain.CodeZeusContractRequired
	case status == 401 || status == 403:
		return domain.CodeZeusAuthFailed
	case status >= 500:
		return domain.CodeZeusHTTP5xx
	case status >= 400:
		return domain.CodeZeusHTTP4xx
	default:
		return domain.CodeZeusTransport
	}
}

func parseBody(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		s := string(raw)
		if len(s) > 2000 {
			s = s[:2000]
		}
		return map[string]any{"raw": s}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"result": v}
}

func attachReqID(err *domain.Error, reqID string) {
	if err == nil || reqID == "" {
		return
	}
	if err.Details == nil {
		err.Details = map[string]any{}
	}
	if _, ok := err.Details["req_id"]; !ok {
		err.Details["req_id"] = reqID
	}
}
