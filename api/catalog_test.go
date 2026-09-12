// SPDX-License-Identifier: BUSL-1.1

package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/koten-ai/zeus_client_golang/config"
	"github.com/koten-ai/zeus_client_golang/domain"
)

func requireCode(t *testing.T, err error, code domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("got %v want %s", err, code)
	}
}

func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := wd; ; d = filepath.Dir(d) {
		p := filepath.Join(d, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	t.Fatalf("missing %s from %s", rel, wd)
	return ""
}

func seedMock(t *testing.T, dir string) {
	t.Helper()
	src := filepath.Dir(findRepoFile(t, filepath.Join("testdata", "catalogs", "mock_base5_minimal", "chat_request.mock.json")))
	body, err := os.ReadFile(filepath.Join(src, "chat_request.mock.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join(src, "response_output_schema.min.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "chat_request_analytics_v2.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "response_output_schema.min.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, doc any) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogAPILoadMockAndInfo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedMock(t, dir)
	cfg := config.Default()
	cfg.ChatRequestsDir = dir
	cfg.ClientFloor = "client-floor-5"
	cfg.Target = config.DataTarget{Bucket: "x", Scope: "_default"}
	var logs []map[string]any
	api := NewCatalogAPI(CatalogOptions{
		Config: cfg,
		Log: func(msg string, attrs map[string]any) {
			cp := map[string]any{"msg": msg}
			for k, v := range attrs {
				cp[k] = v
			}
			logs = append(logs, cp)
		},
	})
	loaded, err := api.Load(ctx, LoadParams{Mode: "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BaseID != "base-5" {
		t.Fatalf("base_id %s", loaded.BaseID)
	}
	if domain.ExtractContractID(loaded.Body) != "mock_analytics_b5" {
		t.Fatal("id")
	}
	const mockHash = "mock:00000000000000000000000000000000"
	if loaded.ContractHash != mockHash {
		t.Fatalf("hash %s", loaded.ContractHash)
	}
	if domain.ComputeContractHash(loaded.Body) == mockHash {
		t.Fatal("compute must not equal mock stamp")
	}
	if loaded.ResponseOutputSchema == nil || !domain.SchemaRequiredFourDefined(loaded.ResponseOutputSchema) {
		t.Fatal("schema")
	}
	info, err := api.Info(ctx, LoadParams{Mode: "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	if info["stamp_present"] != "true" || info["has_response_schema"] != "true" {
		t.Fatalf("%v", info)
	}
	if info["client_floor"] != "client-floor-5" || info["contract_id"] != "mock_analytics_b5" {
		t.Fatalf("%v", info)
	}
	if info["base_id"] != "base-5" {
		t.Fatalf("info base %s", info["base_id"])
	}
	bound, err := api.Contract.Bind(ctx, "x", "_default", "analytics", loaded.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bound["hash_source"] != "stamp" || bound["contract_hash"] != mockHash {
		t.Fatalf("%v", bound)
	}
	mini := api.MiniSchema.FromCatalog(ctx, loaded.Body, false)
	if mini["source"] != "disk" {
		t.Fatal("source")
	}
	ents, _ := mini["entity_types"].(map[string]any)
	if len(ents) != 0 {
		t.Fatalf("invented %#v", ents)
	}
	gotLog := api.LastLoadLog()
	if gotLog["base_id"] != "base-5" || gotLog["client.floor"] != "client-floor-5" || gotLog["result"] != "ok" {
		t.Fatalf("log %#v", gotLog)
	}
	if len(logs) == 0 || logs[0]["msg"] != "zeus_client.catalog.loaded" {
		t.Fatalf("logs %#v", logs)
	}
}

func TestCatalogLoadForTurnBindsSchemaAndSkipsLiveBrief(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedMock(t, dir)
	cfg := config.Default()
	cfg.ChatRequestsDir = dir
	api := NewCatalogAPI(CatalogOptions{Config: cfg})
	loaded, err := api.LoadForTurn(ctx, LoadParams{Mode: "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ResponseOutputSchema == nil || !domain.SchemaRequiredFourDefined(loaded.ResponseOutputSchema) {
		t.Fatal("schema")
	}
	if !strings.Contains(loaded.Source, "live fetch skipped") {
		t.Fatalf("source %q", loaded.Source)
	}
}

func TestCatalogContractNeverInvents(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.ChatRequestsDir = t.TempDir()
	api := NewCatalogAPI(CatalogOptions{Config: cfg})
	_, err := api.Contract.Bind(ctx, "nope", "_default", "analytics", map[string]any{"messages": []any{}})
	requireCode(t, err, domain.CodeContractHashMissing)
	_, err = api.Contract.Hash(ctx, map[string]any{"messages": []any{}})
	requireCode(t, err, domain.CodeContractHashMissing)
}

func TestCatalogFloorBlocksHigherPack(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "chat_request_analytics_base-6.1.json"), map[string]any{
		"_lineage": map[string]any{"base_id": "base-6.1"},
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
		"contract": map[string]any{"hash": "md5:deadbeef"},
	})
	cfg := config.Default()
	cfg.ChatRequestsDir = dir
	cfg.Target = config.DataTarget{Bucket: "x", Scope: "_default"}
	cfg.ClientFloor = "client-floor-5"
	api := NewCatalogAPI(CatalogOptions{Config: cfg})
	_, err := api.Load(ctx, LoadParams{Mode: "analytics", BaseID: "base-6.1"})
	requireCode(t, err, domain.CodePreconditionFailed)
}

func TestCatalogLoadPinRequiresConfiguredID(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.ChatRequestsDir = t.TempDir()
	cfg.ProductionBaseID = ""
	api := NewCatalogAPI(CatalogOptions{Config: cfg})
	_, err := api.LoadPin(ctx, LoadParams{Mode: "analytics"})
	requireCode(t, err, domain.CodeCatalogNotFound)
}

func TestCatalogMiniSchemaGetDiskFallback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedMock(t, dir)
	cfg := config.Default()
	cfg.ChatRequestsDir = dir
	cfg.Target = config.DataTarget{Bucket: "x", Scope: "_default"}
	api := NewCatalogAPI(CatalogOptions{Config: cfg})
	got, err := api.MiniSchema.Get(ctx, MiniSchemaParams{Mode: "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	if got["source"] != "disk" {
		t.Fatalf("%v", got["source"])
	}
	ents, _ := got["entity_types"].(map[string]any)
	if len(ents) != 0 {
		t.Fatalf("invented %#v", ents)
	}
}

func TestCatalogStoreNotWired(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.ChatRequestsDir = ""
	api := NewCatalogAPI(CatalogOptions{Config: cfg})
	_, err := api.Load(ctx, LoadParams{Mode: "analytics"})
	requireCode(t, err, domain.CodeCatalogNotFound)
}
