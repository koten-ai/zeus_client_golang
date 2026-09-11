// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"strings"

	"github.com/koten-ai/zeus_client_golang/domain"
)

// Profiles is the named set (Python PROFILES). API_CONFIG lists development / production / ci; hub is Python V2.
var Profiles = []string{"development", "production", "ci", "hub"}

// ListProfiles returns a copy of Profiles.
func ListProfiles() []string {
	out := make([]string, len(Profiles))
	copy(out, Profiles)
	return out
}

// ValidateProductionSecurity fail-closes insecure production combos (SECURITY §23).
func ValidateProductionSecurity(cfg RuntimeConfig) error {
	if cfg.Zeus.AuthMode == AuthNone {
		return domain.NewConfig(domain.CodeConfigInvalid, "config.profiles",
			domain.WithMessage("production profile rejects zeus.auth_mode=none; use basic, bearer, or session"))
	}
	if !cfg.Zeus.TLSVerify {
		return domain.NewConfig(domain.CodeConfigInvalid, "config.profiles",
			domain.WithMessage("production profile rejects zeus.tls_verify=false; TLS verification is required"))
	}
	return nil
}

// ApplyProfile returns cfg with profile-specific policy defaults.
func ApplyProfile(cfg RuntimeConfig, profile string) (RuntimeConfig, error) {
	name := strings.ToLower(strings.TrimSpace(profile))
	if name == "" {
		name = "development"
	}
	switch name {
	case "dev", "development":
		cfg.Profile = "development"
		cfg.Redaction = RedactionPolicy{Enabled: true, PreviewMaxChars: 16384}
		cfg.Debug.DetectiveBriefing = true
		cfg.Debug.CaptureBodies = true
		cfg.Debug.TransportReplay = true
		level := cfg.Logging.Level
		if level == "info" {
			level = "debug"
		}
		cfg.Logging.Level = level
		return cfg, nil
	case "prod", "production":
		cfg.Profile = "production"
		cfg.Redaction = RedactionPolicy{Enabled: true, PreviewMaxChars: 2048}
		cfg.Debug.DetectiveBriefing = true
		cfg.Debug.CaptureBodies = false
		cfg.Debug.TransportReplay = true
		cfg.Settings = cfg.Settings.withAIProcessResult(false)
		level := cfg.Logging.Level
		switch level {
		case "error", "info", "debug", "trace":
		default:
			level = "info"
		}
		cfg.Logging.Level = level
		cfg.Logging.Redact = true
		if cfg.Logging.OTELEndpoint != "" {
			cfg.Logging.OTELEnabled = true
		}
		if err := ValidateProductionSecurity(cfg); err != nil {
			return RuntimeConfig{}, err
		}
		return cfg, nil
	case "ci":
		cfg.Profile = "ci"
		cfg.Redaction = RedactionPolicy{Enabled: true, PreviewMaxChars: 4096}
		cfg.Debug.DetectiveBriefing = true
		cfg.Debug.CaptureBodies = false
		cfg.Debug.TransportReplay = true
		cfg.Settings = cfg.Settings.withAIProcessResult(false)
		return cfg, nil
	case "hub":
		cfg.Profile = "hub"
		cfg.Redaction = RedactionPolicy{Enabled: true, PreviewMaxChars: 16384}
		cfg.Debug.DetectiveBriefing = true
		cfg.Debug.CaptureBodies = true
		cfg.Debug.TransportReplay = true
		cfg.Settings = cfg.Settings.withAIProcessResult(true)
		return cfg, nil
	default:
		cfg.Profile = name
		cfg.Redaction = RedactionPolicy{Enabled: true, PreviewMaxChars: 16384}
		cfg.Debug.DetectiveBriefing = true
		cfg.Debug.CaptureBodies = true
		cfg.Debug.TransportReplay = true
		return cfg, nil
	}
}
