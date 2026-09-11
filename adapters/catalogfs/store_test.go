// SPDX-License-Identifier: BUSL-1.1

package catalogfs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/koten-ai/zeus_client_golang/domain"
	"github.com/koten-ai/zeus_client_golang/ports"
)

func requireCode(t *testing.T, err error, code domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != code {
		t.Fatalf("got %v want %s", err, code)
	}
}

func writeJSON(t *testing.T, path string, doc any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
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

func mockDir(t *testing.T) string {
	t.Helper()
	return filepath.Dir(findRepoFile(t, filepath.Join("testdata", "catalogs", "mock_base5_minimal", "chat_request.mock.json")))
}

func seedMockAnalytics(t *testing.T, dir string) {
	t.Helper()
	src := mockDir(t)
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

func TestFsStoreLoadRaisesCatalogNotFound(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	bundled := t.TempDir()
	store := New(user, Options{BundledDir: bundled})
	_, err := store.Load(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "a", Scope: "_default"})
	requireCode(t, err, domain.CodeCatalogNotFound)
}

func TestFsStoreLoadStemNamedMode(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	scope := filepath.Join(user, "travel-sample__default")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"messages": []any{map[string]any{"role": "system", "content": "stamped"}}}
	writeJSON(t, filepath.Join(scope, "mode_v2_base-6.1_analytics_stamped.json"), body)
	store := New(user, Options{})
	doc, err := store.Load(ctx, ports.CatalogKey{
		Mode: "mode_v2_base-6.1_analytics_stamped", Bucket: "travel-sample", Scope: "_default",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := doc.Body["messages"].([]any)[0].(map[string]any)["content"]
	if got != "stamped" {
		t.Fatalf("%v", got)
	}
	if filepath.Base(doc.Path) != "mode_v2_base-6.1_analytics_stamped.json" {
		t.Fatalf("%s", doc.Path)
	}
}

func TestFsStoreLoadHealsAndReturnsDocument(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	scope := filepath.Join(user, "beer-sample__default")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "rules only"}},
		"verbs":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "find"}}},
	}
	h := domain.ComputeContractHash(body)
	body["contract"] = map[string]any{"hash": h}
	writeJSON(t, filepath.Join(scope, "chat_request_analytics_v2.json"), body)
	store := New(user, Options{})
	doc, err := store.Load(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "beer-sample", Scope: "_default"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.ContractHash != h {
		t.Fatalf("hash %s want %s", doc.ContractHash, h)
	}
	if doc.Body["messages"].([]any)[0].(map[string]any)["content"] != "rules only" {
		t.Fatal("content")
	}
	if doc.Path == "" || !contains(doc.Path, "beer-sample__default") {
		t.Fatalf("path %s", doc.Path)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

func TestFsStoreLineageMismatch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	writeJSON(t, filepath.Join(tmp, "chat_request_analytics_base-5.3.json"), map[string]any{
		"_lineage": map[string]any{"base_id": "base-5.2"},
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
	})
	store := New(tmp, Options{})
	_, err := store.Load(ctx, ports.CatalogKey{
		Mode: "analytics", Bucket: "x", Scope: "_default", BaseID: "base-5.3",
	})
	requireCode(t, err, domain.CodeCatalogLineageUnknown)
}

func TestFsStoreMissingLineageRefused(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	writeJSON(t, filepath.Join(tmp, "chat_request_analytics_base-5.3.json"), map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
	})
	store := New(tmp, Options{})
	_, err := store.Load(ctx, ports.CatalogKey{
		Mode: "analytics", Bucket: "x", Scope: "_default", BaseID: "base-5.3",
	})
	requireCode(t, err, domain.CodeCatalogLineageUnknown)
}

func TestFsStoreLoadRichLineage(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	writeJSON(t, filepath.Join(tmp, "chat_request_analytics_base-5.3.json"), map[string]any{
		"_format":  "zeus.chat_request.v2",
		"_lineage": map[string]any{"base_id": "base-5.3"},
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
		"contract": map[string]any{"hash": "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "id": "a"},
		"verbs":    []any{map[string]any{"name": "search"}},
	})
	writeJSON(t, filepath.Join(tmp, "response_output_schema.json"), map[string]any{
		"required": []any{"summary", "query_decomposition", "decomposition", "confidence"},
	})
	store := New(tmp, Options{})
	doc, err := store.LoadRich(ctx, ports.CatalogKey{
		Mode: "analytics", Bucket: "x", Scope: "_default", BaseID: "base-5.3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.BaseID != "base-5.3" || doc.LineageID != "base-5.3" {
		t.Fatalf("%+v", doc)
	}
	if doc.Body["_format"] != "zeus.chat_request.v2" {
		t.Fatal("format")
	}
	if doc.ContractHash != "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("hash %s", doc.ContractHash)
	}
	if doc.ResponseOutputSchema == nil {
		t.Fatal("schema")
	}
}

func TestFsStoreLoadMockExtractsStampNotCompute(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	seedMockAnalytics(t, user)
	store := New(user, Options{})
	doc, err := store.LoadRich(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "x", Scope: "_default"})
	if err != nil {
		t.Fatal(err)
	}
	const mockHash = "mock:00000000000000000000000000000000"
	if doc.ContractHash != mockHash {
		t.Fatalf("hash %s", doc.ContractHash)
	}
	if domain.ExtractContractID(doc.Body) != "mock_analytics_b5" {
		t.Fatal("id")
	}
	if doc.BaseID != "base-5" {
		t.Fatalf("base_id %s", doc.BaseID)
	}
	if doc.ResponseOutputSchema == nil {
		t.Fatal("sibling schema")
	}
	if !domain.SchemaRequiredFourDefined(doc.ResponseOutputSchema) {
		t.Fatal("required four")
	}
	computed := domain.ComputeContractHash(doc.Body)
	if computed == mockHash {
		t.Fatal("must not treat compute as mock stamp")
	}
	if len(doc.Body["verbs"].([]any)) < 2 {
		t.Fatal("verbs")
	}
}

func TestFsStoreParseFailed(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	if err := os.WriteFile(filepath.Join(user, "chat_request_analytics_v2.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(user, Options{})
	_, err := store.Load(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "x", Scope: "_default"})
	requireCode(t, err, domain.CodeCatalogParseFailed)
}

func TestFsStoreSaveWritesScopeSubdir(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	store := New(user, Options{})
	err := store.Save(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "beer-sample", Scope: "_default"}, ports.CatalogDocument{
		Body: map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(user, "beer-sample__default", "chat_request_analytics_v2.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

func TestFsStoreLoadCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := New(t.TempDir(), Options{})
	_, err := store.Load(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "x", Scope: "_default"})
	requireCode(t, err, domain.CodeCancelled)
}

func TestFsStoreUnstampedHashEmpty(t *testing.T) {
	ctx := context.Background()
	user := t.TempDir()
	writeJSON(t, filepath.Join(user, "chat_request_analytics_v2.json"), map[string]any{
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
	})
	store := New(user, Options{})
	doc, err := store.Load(ctx, ports.CatalogKey{Mode: "analytics", Bucket: "x", Scope: "_default"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.ContractHash != "" {
		t.Fatalf("invented %q", doc.ContractHash)
	}
	_, err = domain.StampedHash(doc.Body)
	requireCode(t, err, domain.CodeContractHashMissing)
}
