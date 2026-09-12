// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const jobsHTTPComponent = "adapters.jobs_http"

// Runtime is Pattern B ports.Jobs (Python HttpxJobRuntime).
// Watch consumes SSE. Run / Get / Cancel stay 130001.
type Runtime struct {
	hostURL string
	hc      *http.Client
	owns    bool

	mu     sync.Mutex
	closed bool
}

var (
	_ ports.Jobs   = (*Runtime)(nil)
	_ ports.Closer = (*Runtime)(nil)
)

// Options constructs Runtime. HTTP nil → dedicated *http.Client (no process-global
// client; Watch has no overall timeout — cancel via ctx).
type Options struct {
	HTTP *http.Client
}

// New is HttpxJobRuntime(host_url). hostURL is required.
func New(hostURL string, opts Options) *Runtime {
	hc := opts.HTTP
	owns := false
	if hc == nil {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		tr := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       tlsCfg,
		}
		hc = &http.Client{Transport: tr} // no Timeout: SSE until ctx or server close
		owns = true
	}
	return &Runtime{
		hostURL: strings.TrimRight(strings.TrimSpace(hostURL), "/"),
		hc:      hc,
		owns:    owns,
	}
}

func unavailable() *domain.Error {
	return domain.NewJob(domain.CodeJobsUnavailable, jobsHTTPComponent)
}

func (r *Runtime) client() (*http.Client, error) {
	if r == nil {
		return nil, unavailable()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.hc == nil {
		return nil, unavailable()
	}
	return r.hc, nil
}

// HostURL is the sidecar base (no trailing slash).
func (r *Runtime) HostURL() string {
	if r == nil {
		return ""
	}
	return r.hostURL
}

func rejectSecrets(request map[string]any) error {
	blob := fmt.Sprint(request)
	if strings.Contains(blob, "api_key") && !strings.Contains(blob, "api_key_env") {
		return domain.NewJob(domain.CodeJobsInvalidUnitMap, jobsHTTPComponent)
	}
	return nil
}

// Run is 130001 on the current sidecar (Python).
func (r *Runtime) Run(_ context.Context, request map[string]any) (ports.JobHandle, error) {
	if err := rejectSecrets(request); err != nil {
		return ports.JobHandle{}, err
	}
	return ports.JobHandle{}, unavailable()
}

// Get is 130001 on the current sidecar.
func (r *Runtime) Get(context.Context, string) (ports.JobSnapshot, error) {
	return ports.JobSnapshot{}, unavailable()
}

// Cancel is 130001 on the current sidecar.
func (r *Runtime) Cancel(context.Context, string) (ports.JobSnapshot, error) {
	return ports.JobSnapshot{}, unavailable()
}

// Watch is GET {host}/v1/jobs/{id}/events?from_seq=N with Last-Event-ID.
// SSE is not durable SoT. Unknown job → 130004. Transport/5xx → 130001.
// Other 4xx / malformed JSON → 130005. Events with seq <= afterSeq are skipped.
func (r *Runtime) Watch(ctx context.Context, jobID string, afterSeq int) (<-chan ports.JobEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	hc, err := r.client()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(jobID) == "" {
		return nil, domain.NewJob(domain.CodeJobsNotFound, jobsHTTPComponent)
	}
	u, err := url.Parse(EventsURL(r.hostURL, jobID))
	if err != nil {
		return nil, unavailable()
	}
	q := u.Query()
	q.Set(FromSeqParam, seqString(afterSeq))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, unavailable()
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(LastEventIDHeader, seqString(afterSeq))

	resp, err := hc.Do(req)
	if err != nil {
		if de := domain.FromContext(ctx.Err()); de != nil {
			de.Component = jobsHTTPComponent
			return nil, de
		}
		if de := domain.FromContext(err); de != nil {
			de.Component = jobsHTTPComponent
			return nil, de
		}
		return nil, unavailable()
	}
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, domain.NewJob(domain.CodeJobsNotFound, jobsHTTPComponent)
	}
	if resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, unavailable()
	}
	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, domain.NewJob(domain.CodeJobsWatchFailed, jobsHTTPComponent)
	}

	events, err := readSSEEvents(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var buf []ports.JobEvent
	for _, ev := range events {
		if ev.Seq > afterSeq {
			buf = append(buf, ev)
		}
	}
	ch := make(chan ports.JobEvent, len(buf))
	for _, ev := range buf {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// Close drops idle connections when this Runtime owns the client. Idempotent.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	hc := r.hc
	owns := r.owns
	r.mu.Unlock()
	if owns && hc != nil {
		hc.CloseIdleConnections()
		if tr, ok := hc.Transport.(*http.Transport); ok && tr != nil {
			tr.CloseIdleConnections()
		}
	}
	return nil
}
