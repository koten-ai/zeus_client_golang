// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

const sseMaxToken = 1 << 20 // 1 MiB minified JobEvent

// IterSSEEvents yields JobEvents from an SSE body (Python iter_sse_events).
// Malformed data lines → 130005. Comments (": ping") are skipped.
func IterSSEEvents(body string) ([]ports.JobEvent, error) {
	return readSSEEvents(strings.NewReader(body))
}

func readSSEEvents(r io.Reader) ([]ports.JobEvent, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), sseMaxToken)
	var data []string
	var out []ports.JobEvent
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		ev, err := eventFromJSON([]byte(raw))
		if err != nil {
			return err
		}
		out = append(out, ev)
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimLeft(line[5:], " "))
		case strings.TrimSpace(line) == "":
			if err := flush(); err != nil {
				return nil, err
			}
		default:
			// id: / event: / comment — ignore (data is SoT)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, domain.NewJob(domain.CodeJobsWatchFailed, "adapters.jobs_http.sse", domain.WithCause(err))
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

func writeSSEEvent(w io.Writer, ev ports.JobEvent) error {
	payload, err := eventToJSON(ev)
	if err != nil {
		return err
	}
	typ := ev.Type
	if typ == "" {
		typ = "message"
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, typ, payload)
	return err
}
