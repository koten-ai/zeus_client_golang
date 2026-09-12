// SPDX-License-Identifier: BUSL-1.1

package jobshttp

import "strings"

// JobsEventsPath is the sidecar WatchJob path (Python JOBS_EVENTS_PATH).
const JobsEventsPath = "/v1/jobs/{job_id}/events"

// FromSeqParam is the exclusive resume cursor query (Python FROM_SEQ_PARAM).
const FromSeqParam = "from_seq"

// LastEventIDHeader is the SSE reconnect header (Python Last-Event-ID).
const LastEventIDHeader = "Last-Event-ID"

// EventsURL is GET {host}/v1/jobs/{job_id}/events (no query).
func EventsURL(hostURL, jobID string) string {
	base := strings.TrimRight(strings.TrimSpace(hostURL), "/")
	id := strings.TrimSpace(jobID)
	return base + "/v1/jobs/" + id + "/events"
}
