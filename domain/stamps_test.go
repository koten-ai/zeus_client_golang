// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"strings"
	"testing"
	"time"
)

func TestProductStampUserAndVersion(t *testing.T) {
	s := ProductStamp(StampOptions{Version: "0.1.0-dev"})
	if s["user"] != ProductUser {
		t.Fatalf("user %v", s["user"])
	}
	if s["version"] != "0.1.0-dev" {
		t.Fatalf("version %v", s["version"])
	}
	if _, ok := s["ip_address"]; ok {
		t.Fatal("ip")
	}
	if err := AssertProductStamp(s); err != nil {
		t.Fatal(err)
	}
	ts, _ := s["ts"].(string)
	if !strings.HasSuffix(ts, "Z") || len(ts) < 20 {
		t.Fatalf("ts %q", ts)
	}
}

func TestProductStampIncludesValidIP(t *testing.T) {
	s := ProductStamp(StampOptions{IPAddress: "203.0.113.42", Version: "1"})
	if s["ip_address"] != "203.0.113.42" {
		t.Fatalf("%v", s["ip_address"])
	}
}

func TestProductStampOmitsInvalidIP(t *testing.T) {
	s := ProductStamp(StampOptions{IPAddress: "not-an-ip", Version: "1"})
	if _, ok := s["ip_address"]; ok {
		t.Fatal("not-an-ip")
	}
	s2 := ProductStamp(StampOptions{IPAddress: "unknown", Version: "1"})
	if _, ok := s2["ip_address"]; ok {
		t.Fatal("unknown")
	}
}

func TestProductStampNeverAdmin(t *testing.T) {
	s := ProductStamp(StampOptions{Version: "1"})
	if s["user"] == "admin" {
		t.Fatal("admin")
	}
	err := AssertProductStamp(map[string]any{"user": "admin", "version": "1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveStampUser(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ProductUser},
		{"  ", ProductUser},
		{ProductUser, ProductUser},
		{HubUser, HubUser},
		{"helios", "helios"},
		{"zeus", "zeus"},
		{"not-a-user", ProductUser},
		{" Admin ", ProductUser},
	}
	for _, tc := range cases {
		if got := ResolveStampUser(tc.in); got != tc.want {
			t.Fatalf("%q → %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestProductStampHubAdminViaOptions(t *testing.T) {
	s := ProductStamp(StampOptions{User: HubUser, Version: "1"})
	if s["user"] != HubUser {
		t.Fatalf("%v", s["user"])
	}
	if err := AssertProductStamp(s); err == nil {
		t.Fatal("Helios-style filter must reject Hub admin")
	}
}

func TestProductStampOptionalFields(t *testing.T) {
	fixed := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	s := ProductStamp(StampOptions{
		Version:   "0.1.0-dev",
		Scope:     "beer-sample/_default",
		SessionID: "sess_1",
		Now:       func() time.Time { return fixed },
	})
	if s["scope"] != "beer-sample/_default" || s["session_id"] != "sess_1" {
		t.Fatalf("%v", s)
	}
	if s["ts"] != "2026-09-11T12:00:00.000Z" {
		t.Fatalf("ts %v", s["ts"])
	}
}

func TestResolveClientIPConfigAllowsLoopback(t *testing.T) {
	if ResolveClientIP("127.0.0.1", map[string]string{}, false) != "127.0.0.1" {
		t.Fatal("loopback")
	}
}

func TestResolveClientIPEnv(t *testing.T) {
	got := ResolveClientIP("", map[string]string{ipEnvName: "2001:db8::1"}, false)
	if got != "2001:db8::1" {
		t.Fatalf("%q", got)
	}
}

func TestResolveClientIPOmitWhenUnknown(t *testing.T) {
	if ResolveClientIP("  ", map[string]string{}, false) != "" {
		t.Fatal("unknown")
	}
}

func TestIsIPText(t *testing.T) {
	if !IsIPText("192.0.2.1") || !IsIPText("::1") {
		t.Fatal("valid")
	}
	if IsIPText("") || IsIPText("localhost") {
		t.Fatal("invalid")
	}
	if !IsLoopbackIP("127.0.0.1") || !IsLoopbackIP("::1") {
		t.Fatal("loopback")
	}
	if IsLoopbackIP("203.0.113.42") {
		t.Fatal("not loopback")
	}
}

func TestAssertProductStampInvalidIP(t *testing.T) {
	err := AssertProductStamp(map[string]any{
		"user":       ProductUser,
		"version":    "1",
		"ip_address": "nope",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
