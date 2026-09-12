// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
)

func TestProductionRejectsAuthModeNone(t *testing.T) {
	base := Default()
	base.Zeus.AuthMode = AuthNone
	base.Zeus.TLSVerify = true
	_, err := ApplyProfile(base, "production")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("%T %v", err, err)
	}
	if de.Code != domain.CodeConfigInvalid {
		t.Fatalf("code %s", de.Code)
	}
	if !strings.Contains(de.Message, "auth_mode=none") {
		t.Fatalf("message %q", de.Message)
	}
}

func TestProductionRejectsTLSVerifyOff(t *testing.T) {
	base := Default()
	base.Zeus.AuthMode = AuthBasic
	base.Zeus.Username = "u"
	base.Zeus.TLSVerify = false
	_, err := ApplyProfile(base, "production")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("%T %v", err, err)
	}
	if de.Code != domain.CodeConfigInvalid {
		t.Fatalf("code %s", de.Code)
	}
	if !strings.Contains(de.Message, "tls_verify") {
		t.Fatalf("message %q", de.Message)
	}
}

func TestProductionAcceptsBasicWithTLS(t *testing.T) {
	base := Default()
	base.Zeus.AuthMode = AuthBasic
	base.Zeus.Username = "admin"
	base.Zeus.PasswordEnv = "ZEUS_PASSWORD"
	base.Zeus.TLSVerify = true
	out, err := ApplyProfile(base, "production")
	if err != nil {
		t.Fatal(err)
	}
	if out.Profile != "production" {
		t.Fatalf("profile %q", out.Profile)
	}
	if out.Debug.CaptureBodies {
		t.Fatal("capture_bodies")
	}
	if out.Zeus.AuthMode != AuthBasic || !out.Zeus.TLSVerify {
		t.Fatalf("zeus %+v", out.Zeus)
	}
	if out.Settings.AIProcessResult {
		t.Fatal("ai_process_result")
	}
}

func TestDevelopmentStillAllowsAuthModeNone(t *testing.T) {
	base := Default()
	base.Zeus.AuthMode = AuthNone
	out, err := ApplyProfile(base, "development")
	if err != nil {
		t.Fatal(err)
	}
	if out.Profile != "development" || out.Zeus.AuthMode != AuthNone {
		t.Fatalf("%+v", out)
	}
	if out.Settings.AIProcessResult {
		t.Fatal("ai_process_result")
	}
}

func TestHubProfileInsightDefault(t *testing.T) {
	out, err := ApplyProfile(Default(), "hub")
	if err != nil {
		t.Fatal(err)
	}
	if out.Profile != "hub" {
		t.Fatalf("profile %q", out.Profile)
	}
	if !out.Settings.AIProcessResult {
		t.Fatal("hub ai_process_result")
	}
	if !out.Debug.CaptureBodies {
		t.Fatal("hub capture_bodies")
	}
	if !out.Debug.DetectiveBriefing {
		t.Fatal("hub detective_briefing")
	}
}

func TestDevelopmentDetectiveOff(t *testing.T) {
	out, err := ApplyProfile(Default(), "development")
	if err != nil {
		t.Fatal(err)
	}
	if out.Debug.DetectiveBriefing {
		t.Fatal("product profile detective")
	}
	if out.Settings.DurableSessions {
		t.Fatal("product profile durable_sessions")
	}
}

func TestCIStillAllowsAuthModeNone(t *testing.T) {
	base := Default()
	base.Zeus.AuthMode = AuthNone
	out, err := ApplyProfile(base, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if out.Profile != "ci" || out.Zeus.AuthMode != AuthNone {
		t.Fatalf("%+v", out)
	}
}

func TestProfileMatrix(t *testing.T) {
	got := ListProfiles()
	want := map[string]bool{"development": true, "production": true, "ci": true, "hub": true}
	if len(got) != 4 {
		t.Fatalf("%v", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("unexpected %q", p)
		}
	}
	base := Default()
	base.Zeus.AuthMode = AuthBasic
	base.Zeus.Username = "u"
	base.Zeus.PasswordEnv = "ZEUS_PASSWORD"
	dev, err := ApplyProfile(base, "development")
	if err != nil {
		t.Fatal(err)
	}
	prod, err := ApplyProfile(base, "production")
	if err != nil {
		t.Fatal(err)
	}
	ci, err := ApplyProfile(base, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if !dev.Debug.CaptureBodies {
		t.Fatal("dev capture")
	}
	if prod.Debug.CaptureBodies || ci.Debug.CaptureBodies {
		t.Fatal("prod/ci capture")
	}
	if dev.Redaction.PreviewMaxChars <= prod.Redaction.PreviewMaxChars {
		t.Fatalf("preview %d vs %d", dev.Redaction.PreviewMaxChars, prod.Redaction.PreviewMaxChars)
	}
}
