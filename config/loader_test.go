// SPDX-License-Identifier: BUSL-1.1

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
)

func writeJSON(t *testing.T, v any) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cfg.json")
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func repoPins(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	p := filepath.Join(filepath.Dir(file), "..", "sdk_bootstrap.pins.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAIProcessResultPackageDefaultFalse(t *testing.T) {
	cfg := Default()
	if cfg.Settings.AIProcessResult {
		t.Fatal("ai_process_result")
	}
	if cfg.Settings.IgnoreUserToolPathHints != true {
		t.Fatal("ignore_user_tool_path_hints")
	}
	if cfg.Logging.Redact != true {
		t.Fatal("log redact")
	}
	if cfg.User != "zeus_client" {
		t.Fatalf("user %q", cfg.User)
	}
	if cfg.SemanticCache.Enabled {
		t.Fatal("semantic cache")
	}
	if !cfg.SemanticCache.Write.WriteExplicitOnly {
		t.Fatal("write_explicit_only")
	}
	if len(cfg.SemanticCache.ApplyToModes) != 1 || cfg.SemanticCache.ApplyToModes[0] != "agent" {
		t.Fatalf("%v", cfg.SemanticCache.ApplyToModes)
	}
	if cfg.LLM.Roles == nil {
		t.Fatal("roles map must exist")
	}
}

func TestLoadFromTmpJSON(t *testing.T) {
	p := writeJSON(t, map[string]any{
		"zeus":     map[string]any{"url": "http://example:8080", "auth_mode": "none"},
		"target":   map[string]any{"bucket": "b1", "scope": "s1", "collection": "c1"},
		"settings": map[string]any{"ai_process_result": true, "max_rounds": 4},
	})
	cfg, err := Load(p, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Zeus.URL != "http://example:8080" {
		t.Fatalf("url %q", cfg.Zeus.URL)
	}
	if cfg.Target.Bucket != "b1" {
		t.Fatalf("bucket %q", cfg.Target.Bucket)
	}
	if cfg.Settings.MaxRounds != 4 {
		t.Fatalf("rounds %d", cfg.Settings.MaxRounds)
	}
	if cfg.Profile != "development" {
		t.Fatalf("profile %q", cfg.Profile)
	}
	if !cfg.Debug.CaptureBodies {
		t.Fatal("dev capture_bodies")
	}
}

func TestEnvOverridesURLAndBucket(t *testing.T) {
	p := writeJSON(t, map[string]any{
		"zeus": map[string]any{
			"url":          "http://file:1",
			"auth_mode":    "basic",
			"username":     "admin",
			"password_env": "ZEUS_PASSWORD",
		},
	})
	cfg, err := Load(p, "production", map[string]string{
		"ZEUS_CLIENT_URL":               "http://env:9090",
		"ZEUS_CLIENT_BUCKET":            "yelp-demo",
		"ZEUS_CLIENT_AI_PROCESS_RESULT": "false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Zeus.URL != "http://env:9090" {
		t.Fatalf("url %q", cfg.Zeus.URL)
	}
	if cfg.Target.Bucket != "yelp-demo" {
		t.Fatalf("bucket %q", cfg.Target.Bucket)
	}
	if cfg.Settings.AIProcessResult {
		t.Fatal("ai_process_result")
	}
	if cfg.Profile != "production" {
		t.Fatalf("profile %q", cfg.Profile)
	}
	if cfg.Debug.CaptureBodies {
		t.Fatal("prod capture_bodies")
	}
	if cfg.Redaction.PreviewMaxChars != 2048 {
		t.Fatalf("preview %d", cfg.Redaction.PreviewMaxChars)
	}
}

func TestLoggingAndIPEnvOverlays(t *testing.T) {
	p := writeJSON(t, map[string]any{
		"zeus": map[string]any{"url": "http://file:1", "auth_mode": "basic", "username": "u"},
	})
	cfg, err := Load(p, "production", map[string]string{
		"ZEUS_CLIENT_LOG_LEVEL":     "debug",
		"ZEUS_CLIENT_LOG_REDACT":    "true",
		"ZEUS_CLIENT_IP":            "203.0.113.10",
		"ZEUS_CLIENT_OTEL_ENDPOINT": "http://otel:4318",
		"ZEUS_CLIENT_OTEL_ENABLED":  "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logging.Level != "debug" {
		t.Fatalf("level %q", cfg.Logging.Level)
	}
	if !cfg.Logging.Redact {
		t.Fatal("redact")
	}
	if cfg.Client.IPAddress != "203.0.113.10" {
		t.Fatalf("ip %q", cfg.Client.IPAddress)
	}
	if cfg.Logging.OTELEndpoint != "http://otel:4318" || !cfg.Logging.OTELEnabled {
		t.Fatalf("otel %+v", cfg.Logging)
	}
}

func TestConfigReprAndPublicDictHaveNoSecretValues(t *testing.T) {
	cfg := Default()
	text := strings.ToLower(cfg.String())
	if strings.Contains(text, "password=") && !strings.Contains(text, "password_env") {
		t.Fatalf("password leaked: %s", cfg.String())
	}
	if strings.Contains(cfg.String(), "sk-") {
		t.Fatalf("sk leaked: %s", cfg.String())
	}
	pub := cfg.ToPublicDict()
	llm := pub["llm"].(map[string]any)
	if llm["api_key_env"] != "XAI_API_KEY" {
		t.Fatalf("api_key_env %v", llm["api_key_env"])
	}
	if _, ok := llm["api_key"]; ok {
		t.Fatal("api_key must not appear")
	}
}

func TestMissingConfigPathRaises(t *testing.T) {
	_, err := Load("/no/such/config.json", "", map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	var de *domain.Error
	if !errors.As(err, &de) {
		t.Fatalf("%T %v", err, err)
	}
	if de.Code != domain.CodeConfigPathNotFound {
		t.Fatalf("code %s", de.Code)
	}
}

func TestLoadLLMRolesAndJobsHost(t *testing.T) {
	p := writeJSON(t, map[string]any{
		"llm": map[string]any{
			"model":       "fast-worker",
			"api_key_env": "LLM_DEFAULT_KEY",
			"roles": map[string]any{
				"orchestrator": map[string]any{"model": "strong-planner", "api_key_env": "LLM_ORCH_KEY"},
				"advisor":      map[string]any{"model": "strong-planner"},
				"worker":       map[string]any{"model": "fast-worker", "api_key_env": "LLM_WORKER_KEY"},
			},
		},
		"jobs": map[string]any{
			"host_url": "http://127.0.0.1:7090",
			"models":   map[string]any{"worker": map[string]any{"model": "fast-worker-v2"}},
		},
	})
	cfg, err := Load(p, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Roles["orchestrator"].Model != "strong-planner" {
		t.Fatalf("%+v", cfg.LLM.Roles["orchestrator"])
	}
	if cfg.LLM.Roles["orchestrator"].APIKeyEnv != "LLM_ORCH_KEY" {
		t.Fatalf("orch key %q", cfg.LLM.Roles["orchestrator"].APIKeyEnv)
	}
	if cfg.LLM.Roles["advisor"].APIKeyEnv != "LLM_DEFAULT_KEY" {
		t.Fatalf("advisor key %q", cfg.LLM.Roles["advisor"].APIKeyEnv)
	}
	if cfg.Jobs.HostURL != "http://127.0.0.1:7090" {
		t.Fatalf("jobs %q", cfg.Jobs.HostURL)
	}
	pub := cfg.ToPublicDict()
	llm := pub["llm"].(map[string]any)
	roles := llm["roles"].(map[string]any)
	orch := roles["orchestrator"].(map[string]any)
	if orch["api_key_env"] != "LLM_ORCH_KEY" {
		t.Fatalf("%v", orch)
	}
	blob, _ := json.Marshal(pub)
	if strings.Contains(string(blob), "sk-") {
		t.Fatal("sk in public dict")
	}
	dumped, _ := json.Marshal(llm)
	if strings.Contains(string(dumped), `"api_key"`) {
		t.Fatalf("api_key key in %s", dumped)
	}
	if !strings.Contains(string(dumped), "api_key_env") {
		t.Fatal("api_key_env missing")
	}
}

func TestSemanticCacheBoolOrObjectAndEnv(t *testing.T) {
	p := writeJSON(t, map[string]any{"session": map[string]any{"semantic_cache": false}})
	cfg, err := Load(p, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SemanticCache.Enabled {
		t.Fatal("expected off")
	}

	p = writeJSON(t, map[string]any{
		"session": map[string]any{
			"semantic_cache": map[string]any{
				"enabled": true,
				"recall":  map[string]any{"top_k": 3, "timeout_ms": 80, "min_score": 0.7},
				"inject":  map[string]any{"max_chars": 500},
				"write": map[string]any{
					"write_explicit_only": false,
					"ttl_seconds":         map[string]any{"conversational": 3600, "profile": 86400},
				},
			},
		},
	})
	cfg, err = Load(p, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SemanticCache.Enabled {
		t.Fatal("expected on")
	}
	if cfg.SemanticCache.Recall.TopK != 3 || cfg.SemanticCache.Recall.TimeoutMS != 80 {
		t.Fatalf("recall %+v", cfg.SemanticCache.Recall)
	}
	if cfg.SemanticCache.Inject.MaxChars != 500 {
		t.Fatalf("inject %d", cfg.SemanticCache.Inject.MaxChars)
	}
	if cfg.SemanticCache.Write.WriteExplicitOnly {
		t.Fatal("write_explicit_only")
	}
	if cfg.SemanticCache.Write.TTLSeconds != 3600 {
		t.Fatalf("ttl %d", cfg.SemanticCache.Write.TTLSeconds)
	}

	off, err := Load(p, "development", map[string]string{"ZEUS_CLIENT_SEMANTIC_CACHE": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if off.SemanticCache.Enabled {
		t.Fatal("env off")
	}
	pub := cfg.ToPublicDict()["semantic_cache"].(map[string]any)
	if pub["enabled"] != true {
		t.Fatalf("%v", pub["enabled"])
	}
	if _, ok := pub["dev_user_id"]; ok {
		t.Fatal("dev_user_id must not appear")
	}
	if pub["has_dev_user_id"] != false {
		t.Fatalf("has_dev_user_id %v", pub["has_dev_user_id"])
	}
}

func TestPinsLoadWithoutSecrets(t *testing.T) {
	path := repoPins(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") {
		t.Fatal("pins file contains sk-")
	}
	if strings.Contains(string(raw), `"api_key"`) && !strings.Contains(string(raw), `"api_key_env"`) {
		t.Fatal("pins hold api_key value")
	}
	cfg, err := Load(path, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.APIKeyEnv != "XAI_API_KEY" {
		t.Fatalf("api_key_env %q", cfg.LLM.APIKeyEnv)
	}
	if cfg.LLM.Provider != "xai" {
		t.Fatalf("provider %q", cfg.LLM.Provider)
	}
	if cfg.Zeus.URL != "http://127.0.0.1:8080" {
		t.Fatalf("url %q", cfg.Zeus.URL)
	}
	if cfg.User != "zeus_client" {
		t.Fatalf("user %q", cfg.User)
	}
	if cfg.ClientFloor != "client-floor-5" {
		t.Fatalf("floor %q", cfg.ClientFloor)
	}
	if cfg.Zeus.AuthMode != AuthNone {
		t.Fatalf("lab auth %q", cfg.Zeus.AuthMode)
	}
	if cfg.ProductionBaseID != "" {
		t.Fatalf("production_base_id %q", cfg.ProductionBaseID)
	}
	blob, err := json.Marshal(cfg.ToPublicDict())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "sk-") {
		t.Fatal("public dump leaked key")
	}
	llm := cfg.ToPublicDict()["llm"].(map[string]any)
	if _, ok := llm["api_key"]; ok {
		t.Fatal("api_key in public llm")
	}
}

func TestPinsProductionRejectedUntilAuthSet(t *testing.T) {
	_, err := Load(repoPins(t), "production", map[string]string{})
	if err == nil {
		t.Fatal("pins auth_smoke=none must fail production")
	}
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConfigInvalid {
		t.Fatalf("%v", err)
	}
}

func TestLoadCustomLLMDoesNotInventHost(t *testing.T) {
	p := writeJSON(t, map[string]any{
		"zeus": map[string]any{"url": "http://127.0.0.1:8080", "auth_mode": "none"},
		"llm":  map[string]any{"provider": "custom", "api_style": "openai_compatible"},
	})
	cfg, err := Load(p, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Provider != "custom" {
		t.Fatalf("provider %q", cfg.LLM.Provider)
	}
	if cfg.LLM.BaseURL != "" {
		t.Fatalf("must not invent host %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "" {
		t.Fatalf("must not invent model %q", cfg.LLM.Model)
	}
	if cfg.LLM.APIStyle != "openai_compatible" {
		t.Fatalf("api_style %q", cfg.LLM.APIStyle)
	}
}

func TestLLMAPIKeyInFileIsIgnored(t *testing.T) {
	p := writeJSON(t, map[string]any{
		"llm": map[string]any{"api_key": "sk-should-not-load", "api_key_env": "XAI_API_KEY"},
	})
	cfg, err := Load(p, "development", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(cfg.ToPublicDict())
	if strings.Contains(string(blob), "sk-should-not-load") {
		t.Fatal("secret leaked from ignored api_key field")
	}
}

func TestRewindFromMappingAndEnv(t *testing.T) {
	cfg, err := FromMapping(map[string]any{"debug": map[string]any{"rewind": true}, "profile": "ci"}, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Debug.Rewind {
		t.Fatal("mapping rewind")
	}
	cfg, err = FromMapping(map[string]any{"settings": map[string]any{"rewind": true}, "profile": "ci"}, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Debug.Rewind {
		t.Fatal("settings rewind alias")
	}
	on, err := applyEnv(cfg, map[string]string{"ZEUS_REWIND": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if !on.Debug.Rewind {
		t.Fatal("env on")
	}
	off, err := applyEnv(on, map[string]string{"ZEUS_REWIND": "0"})
	if err != nil {
		t.Fatal(err)
	}
	if off.Debug.Rewind {
		t.Fatal("env off")
	}
}

func TestConcurrentLoadAndPublicDict(t *testing.T) {
	path := repoPins(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg, err := Load(path, "development", map[string]string{})
			if err != nil {
				t.Errorf("load: %v", err)
				return
			}
			_ = cfg.ToPublicDict()
			_ = cfg.String()
			_, _ = ApplyProfile(cfg, "ci")
		}()
	}
	wg.Wait()
}

func TestInvalidAuthMode(t *testing.T) {
	_, err := FromMapping(map[string]any{"zeus": map[string]any{"auth_mode": "nope"}}, "development")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeZeusAuthModeInvalid {
		t.Fatalf("%v", err)
	}
}
