// SPDX-License-Identifier: BUSL-1.1

package observability

import (
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/security"
)

func TestInfoEventNameAndAttrs(t *testing.T) {
	fl := NewFamilyLogger(FamilyLoggerOptions{Level: "trace", Redact: true, ServiceName: "zeus_client"})
	fl.Info("zeus_client.turn.started", map[string]any{"session.id": "sess_1", "scope": "b/s"})
	var rec LogRecord
	for _, r := range fl.Records() {
		if r.Event == "zeus_client.turn.started" {
			rec = r
			break
		}
	}
	if rec.Event == "" {
		t.Fatal("missing event")
	}
	if rec.Level != "INFO" {
		t.Fatalf("level %s", rec.Level)
	}
	if rec.Attrs["session.id"] != "sess_1" {
		t.Fatalf("attrs %v", rec.Attrs)
	}
	if rec.Attrs["service.name"] != "zeus_client" {
		t.Fatalf("service %v", rec.Attrs["service.name"])
	}
}

func TestHardDenySecretWhenRedactFalse(t *testing.T) {
	attrs := security.RedactAttrs(map[string]any{
		"authorization": "Bearer sk-secret",
		"req_id":        "abc",
	}, false)
	if attrs["authorization"] != security.Redacted {
		t.Fatalf("authorization %v", attrs["authorization"])
	}
	if attrs["req_id"] != "abc" {
		t.Fatalf("req_id %v", attrs["req_id"])
	}
}

func TestTraceLevelBelowDebug(t *testing.T) {
	fl := NewFamilyLogger(FamilyLoggerOptions{Level: "debug", Redact: true})
	fl.Trace("zeus_client.tool.result_shape", map[string]any{"verb": "find", "bytes": 12})
	for _, r := range fl.Records() {
		if r.Event == "zeus_client.tool.result_shape" {
			t.Fatal("trace must not emit at debug")
		}
	}
	fl.SetLevel("trace")
	fl.Trace("zeus_client.tool.result_shape", map[string]any{"verb": "find"})
	found := false
	for _, r := range fl.Records() {
		if r.Event == "zeus_client.tool.result_shape" {
			found = true
			if r.Level != "TRACE" {
				t.Fatalf("level %s", r.Level)
			}
		}
	}
	if !found {
		t.Fatal("trace missing")
	}
}

func TestConfigureRedactFalseIsLoud(t *testing.T) {
	fl := NewFamilyLogger(FamilyLoggerOptions{Level: "info", Redact: false})
	fl.Configure()
	found := false
	for _, r := range fl.Records() {
		if r.Event == "zeus_client.logging.configured" {
			found = true
			if r.Attrs["redact"] != false {
				t.Fatalf("redact %v", r.Attrs["redact"])
			}
		}
	}
	if !found {
		t.Fatal("configured missing")
	}
}

func TestNoHopTelemetryEventName(t *testing.T) {
	fl := NewFamilyLogger(FamilyLoggerOptions{Level: "trace", Redact: true})
	fl.Info("zeus_client.zeus.req", map[string]any{"req_id": "r1", "duration_ms": 11, "bytes.in": 10, "bytes.out": 4})
	for _, r := range fl.Records() {
		if strings.Contains(r.Event, "hop.telemetry") {
			t.Fatalf("invented event %s", r.Event)
		}
	}
}

func TestFamilyLoggerRedactsSecretsAtInfo(t *testing.T) {
	fl := NewFamilyLogger(FamilyLoggerOptions{Level: "info", Redact: true})
	fl.Info("zeus_client.turn.started", map[string]any{
		"authorization": "Bearer sk-secret-token",
		"req_id":        "abc",
	})
	rec := fl.Records()[0]
	if rec.Attrs["authorization"] != security.Redacted {
		t.Fatalf("leaked %v", rec.Attrs["authorization"])
	}
	if rec.Attrs["req_id"] != "abc" {
		t.Fatal("req_id")
	}
}
