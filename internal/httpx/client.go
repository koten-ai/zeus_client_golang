// SPDX-License-Identifier: BUSL-1.1

package httpx

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// DefaultTimeout is the Zeus hop timeout (config.Zeus.TimeoutS default).
const DefaultTimeout = 30 * time.Second

// Options constructs a dedicated transport. Zero Timeout → DefaultTimeout.
// SkipTLSVerify is lab-only; production profile already rejects tls_verify=false.
type Options struct {
	Timeout       time.Duration
	SkipTLSVerify bool
}

// Client is a ports.HttpPort over a private *http.Client (Python httpx.AsyncClient).
type Client struct {
	hc      *http.Client
	timeout time.Duration

	mu     sync.Mutex
	closed bool
}

var _ ports.HttpPort = (*Client)(nil)

// New returns a Client. Call Close to drop idle connections.
func New(opts Options) *Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if opts.SkipTLSVerify {
		tlsCfg.InsecureSkipVerify = true
	}
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsCfg,
	}
	return &Client{
		hc: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
		timeout: timeout,
	}
}

// Request performs one hop. Prefer omitting X-Zeus-Req-Id (Zeus mints).
// PreMintReqID sends a UUID v4 when the header is absent. Response headers
// always include the echoed id when the server sent it — including 4xx/5xx.
func (c *Client) Request(ctx context.Context, req ports.HTTPRequest) (ports.HTTPResponse, error) {
	if c == nil {
		return ports.HTTPResponse{}, domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx",
			domain.WithMessage("http client is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	closed := c.closed
	hc := c.hc
	c.mu.Unlock()
	if closed || hc == nil {
		return ports.HTTPResponse{}, domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx",
			domain.WithMessage("http client closed"))
	}

	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = http.MethodGet
	}

	callCtx := ctx
	var cancel context.CancelFunc
	if req.TimeoutS > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutS*float64(time.Second)))
		defer cancel()
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(callCtx, method, req.URL, body)
	if err != nil {
		return ports.HTTPResponse{}, domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx",
			domain.WithCause(err),
			domain.WithMessage("http request build failed"))
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if req.PreMintReqID && httpReq.Header.Get(domain.ReqIDHeader) == "" {
		httpReq.Header.Set(domain.ReqIDHeader, domain.NewZeusReqID())
	}
	if len(req.Body) > 0 && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := hc.Do(httpReq)
	if err != nil {
		if ce := domain.FromContext(callCtx.Err()); ce != nil {
			ce.Component = "internal.httpx"
			return ports.HTTPResponse{}, ce
		}
		if ce := domain.FromContext(err); ce != nil {
			ce.Component = "internal.httpx"
			return ports.HTTPResponse{}, ce
		}
		return ports.HTTPResponse{}, domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx",
			domain.WithCause(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if ce := domain.FromContext(callCtx.Err()); ce != nil {
			ce.Component = "internal.httpx"
			return ports.HTTPResponse{}, ce
		}
		return ports.HTTPResponse{}, domain.NewZeusTransport(domain.CodeZeusTransport, "internal.httpx",
			domain.WithCause(err),
			domain.WithMessage("http response body read failed"))
	}
	headers := flattenHeader(resp.Header)
	return ports.HTTPResponse{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    raw,
	}, nil
}

// Close drops idle connections. Idempotent and nil-safe.
func (c *Client) Close(_ context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.hc != nil {
		c.hc.CloseIdleConnections()
		if tr, ok := c.hc.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
	return nil
}

func flattenHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}
