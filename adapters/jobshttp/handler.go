// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

// Handler serves SSE WatchJob for Pattern B consumers:
//
//	GET /v1/jobs/{job_id}/events?from_seq=N
//	Last-Event-ID: N
//
// Query from_seq wins over Last-Event-ID. SSE is not durable SoT.
// Events come from ports.Jobs.Watch (in-process snapshot or sidecar).
type Handler struct {
	Jobs ports.Jobs
	// PathJobID extracts job id from r.URL.Path. Default: segment before "events".
	PathJobID func(r *http.Request) (string, error)
}

func (h *Handler) jobID(r *http.Request) (string, error) {
	if h != nil && h.PathJobID != nil {
		return h.PathJobID(r)
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "events" && i > 0 {
			return parts[i-1], nil
		}
	}
	if len(parts) > 0 {
		return parts[len(parts)-1], nil
	}
	return "", fmt.Errorf("no job id")
}

func parseFromSeq(r *http.Request) int {
	if s := r.URL.Query().Get(FromSeqParam); s != "" {
		n, _ := strconv.Atoi(s)
		return n
	}
	if s := r.Header.Get(LastEventIDHeader); s != "" {
		n, _ := strconv.Atoi(s)
		return n
	}
	return 0
}

func writeJobHTTPError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if de, ok := domain.AsError(err); ok {
		switch de.Code {
		case domain.CodeJobsNotFound:
			code = http.StatusNotFound
		case domain.CodeJobsUnavailable:
			code = http.StatusServiceUnavailable
		case domain.CodeJobsWatchFailed:
			code = http.StatusBadRequest
		}
	}
	http.Error(w, err.Error(), code)
}

// ServeHTTP implements http.Handler (SSE WatchJob).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h == nil || h.Jobs == nil {
		http.Error(w, "jobs unavailable", http.StatusServiceUnavailable)
		return
	}
	jobID, err := h.jobID(r)
	if err != nil || jobID == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}
	fromSeq := parseFromSeq(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	ch, err := h.Jobs.Watch(ctx, jobID, fromSeq)
	if err != nil {
		writeJobHTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if ch != nil {
		for ev := range ch {
			if err := writeSSEEvent(w, ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
	_, _ = fmt.Fprintf(w, ": done\n\n")
	flusher.Flush()
}
