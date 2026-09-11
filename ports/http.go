// SPDX-License-Identifier: BUSL-1.1

package ports

import "context"

// HTTPRequest is a low-level hop. Body is already-encoded bytes (no live
// *http.Client here — that belongs in internal/httpx, ZCG-8).
type HTTPRequest struct {
	Method   string
	URL      string
	Headers  map[string]string
	Body     []byte
	TimeoutS float64 // 0 → adapter default (Zeus 30s)
	// PreMintReqID sends X-Zeus-Req-Id as UUID v4 when the header is absent.
	// Default false: omit the header and read Zeus's echo (preferred).
	PreMintReqID bool
}

// HTTPResponse is status + headers + body. Adapters must capture
// X-Zeus-Req-Id on every response including 4xx/5xx (ZCG-8).
type HTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// HttpPort is optional low-level HTTP (Python HttpPort.request / aclose).
type HttpPort interface {
	Request(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
	Close(ctx context.Context) error
}
