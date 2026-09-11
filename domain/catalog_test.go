// SPDX-License-Identifier: BUSL-1.1

package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func requireCode(t *testing.T, err error, code Code) {
	t.Helper()
	de, ok := AsError(err)
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
		sib := filepath.Join(d, "..", "zeus_client_design", rel)
		if _, err := os.Stat(sib); err == nil {
			return sib
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	t.Fatalf("missing %s from %s", rel, wd)
	return ""
}

func mockFixtureDir(t *testing.T) string {
	t.Helper()
	p := findRepoFile(t, filepath.Join("testdata", "catalogs", "mock_base5_minimal", "chat_request.mock.json"))
	return filepath.Dir(p)
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestScopeSubdirStripsLeadingUnderscore(t *testing.T) {
	if ScopeChatRequestsSubdir("beer-sample", "_default") != "beer-sample__default" {
		t.Fatal("default")
	}
	if ScopeChatRequestsSubdir("yelp-data", "inventory") != "yelp-data__inventory" {
		t.Fatal("inventory")
	}
}

func TestChatRequestFilename(t *testing.T) {
	if ChatRequestFilename("default", "") != "chat_request_v2.json" {
		t.Fatal("default")
	}
	if ChatRequestFilename("", "") != "chat_request_v2.json" {
		t.Fatal("empty")
	}
	if ChatRequestFilename("analytics", "") != "chat_request_analytics_v2.json" {
		t.Fatal("analytics")
	}
	if ChatRequestFilename("analytics_v2_base_6", "") != "chat_request_analytics_v2_base_6_v2.json" {
		t.Fatal("compound")
	}
}

func TestCatalogFilenamesForModeIncludesStem(t *testing.T) {
	names := CatalogFilenamesForMode("mode_v2_base-6.1_analytics_stamped", "")
	if len(names) == 0 || names[0] != "chat_request_mode_v2_base-6.1_analytics_stamped_v2.json" {
		t.Fatalf("%v", names)
	}
	found := false
	for _, n := range names {
		if n == "mode_v2_base-6.1_analytics_stamped.json" {
			found = true
		}
	}
	if !found {
		t.Fatalf("stem missing %v", names)
	}
	if len(CatalogFilenamesForMode("../escape", "")) != 0 {
		t.Fatal("escape")
	}
}

func TestModeFilenameRoundtripCompound(t *testing.T) {
	name := "chat_request_analytics_v2_base_6_v2.json"
	mode := ModeFromFilename(name)
	if mode != "analytics_v2_base_6" {
		t.Fatalf("%q", mode)
	}
	if ChatRequestFilename(mode, "") != name {
		t.Fatal("roundtrip")
	}
	bad := "chat_request_analytics_v2_base_6.json"
	if ChatRequestFilename(ModeFromFilename(bad), "") == bad {
		t.Fatal("misnamed drop must not round-trip")
	}
}

func TestResolvePrefersScopeDir(t *testing.T) {
	user := t.TempDir()
	scope := filepath.Join(user, "beer-sample__default")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(scope, "chat_request_analytics_v2.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(user, "yelp-data__default")
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "chat_request_analytics_v2.json"), []byte(`{"wrong":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "chat_request_analytics_v2.json"), []byte(`{"general":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "analytics", Bucket: "beer-sample", Scope: "_default", UserDir: user,
	})
	if !ok || got != target {
		t.Fatalf("got %q want %q", got, target)
	}
}

func TestResolveDoesNotPickSiblingScopeWhenTargetMissing(t *testing.T) {
	user := t.TempDir()
	if err := os.MkdirAll(filepath.Join(user, "yelp-demo__default"), 0o755); err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(user, "yelp-data__default")
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "chat_request_analytics_v2.json"), []byte(`{"from":"sibling"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "analytics", Bucket: "yelp-demo", Scope: "_default", UserDir: user,
	})
	if ok {
		t.Fatalf("sibling rglob leaked %q", got)
	}
}

func TestResolveUserGeneralTopLevelOnly(t *testing.T) {
	user := t.TempDir()
	general := filepath.Join(user, "chat_request_analytics_v2.json")
	if err := os.WriteFile(general, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(user, "other", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "chat_request_analytics_v2.json"), []byte(`{"nested":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "analytics", Bucket: "beer-sample", Scope: "_default", UserDir: user,
	})
	if !ok || got != general {
		t.Fatalf("got %q", got)
	}
}

func TestResolveBundledFallback(t *testing.T) {
	user := t.TempDir()
	bundled := t.TempDir()
	bfile := filepath.Join(bundled, "chat_request_analytics_v2.json")
	if err := os.WriteFile(bfile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "analytics", Bucket: "x", Scope: "_default", UserDir: user, BundledDir: bundled,
	})
	if !ok || got != bfile {
		t.Fatalf("got %q", got)
	}
}

func TestResolveDefaultModeFilename(t *testing.T) {
	user := t.TempDir()
	scope := filepath.Join(user, "beer-sample__default")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(scope, "chat_request_v2.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "default", Bucket: "beer-sample", Scope: "_default", UserDir: user,
	})
	if !ok || got != p {
		t.Fatalf("got %q", got)
	}
}

func TestMergeScopeBriefRstripsAndAppends(t *testing.T) {
	doc := map[string]any{
		"messages":     []any{map[string]any{"role": "system", "content": "rules only\n"}},
		"instructions": map[string]any{"system_prompt": "marker only\n"},
	}
	out := MergeScopeBrief(doc, "## SCOPE BRIEF\nscope: demo/_default")
	got := out["messages"].([]any)[0].(map[string]any)["content"].(string)
	if got != "rules only\n\n## SCOPE BRIEF\nscope: demo/_default" &&
		!startsWith(got, "rules only\n\n## SCOPE BRIEF") {
		t.Fatalf("%q", got)
	}
	sp := out["instructions"].(map[string]any)["system_prompt"].(string)
	if !contains(sp, "marker only\n\n## SCOPE BRIEF") {
		t.Fatalf("%q", sp)
	}
	if doc["messages"].([]any)[0].(map[string]any)["content"] != "rules only\n" {
		t.Fatal("original mutated")
	}
}

func startsWith(s, pfx string) bool {
	return len(s) >= len(pfx) && s[:len(pfx)] == pfx
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestResolveStemNamedModeInScopeDir(t *testing.T) {
	user := t.TempDir()
	scope := filepath.Join(user, "travel-sample__default")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	stamped := filepath.Join(scope, "mode_v2_base-6.1_analytics_stamped.json")
	if err := os.WriteFile(stamped, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "chat_request_v2.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "mode_v2_base-6.1_analytics_stamped", Bucket: "travel-sample", Scope: "_default", UserDir: user,
	})
	if !ok || got != stamped {
		t.Fatalf("got %q", got)
	}
}

func TestResolveUnknownModeDoesNotAliasGeneric(t *testing.T) {
	user := t.TempDir()
	if err := os.WriteFile(filepath.Join(user, "chat_request_v2.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "chat_request_analytics_v2.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "missing_mode_xyz_totally_absent", Bucket: "travel-sample", Scope: "_default", UserDir: user,
	})
	if ok {
		t.Fatalf("aliased %q", got)
	}
}

func TestParseCatalogFilename(t *testing.T) {
	mode, base, ok := ParseCatalogFilename("chat_request_analytics_base-5.3.json")
	if !ok || mode != "analytics" || base != "base-5.3" {
		t.Fatalf("%s %s %v", mode, base, ok)
	}
	mode, base, ok = ParseCatalogFilename("chat_request_analytics_cus-3.json")
	if !ok || base != "cus-3" || mode != "analytics" {
		t.Fatalf("cus %s %s", mode, base)
	}
	if _, _, ok := ParseCatalogFilename("chat_request_analytics_v2.json"); ok {
		t.Fatal("legacy v2 must not parse as lineage")
	}
}

func TestModeFromFilenameLineage(t *testing.T) {
	if ModeFromFilename("chat_request_analytics_base-5.3.json") != "analytics" {
		t.Fatal("mode")
	}
	if ChatRequestFilename("analytics", "base-5.3") != "chat_request_analytics_base-5.3.json" {
		t.Fatal("filename")
	}
}

func TestCheckLineageRejectsBadIDShape(t *testing.T) {
	err := CheckLineage(map[string]any{}, "not-a-base", true, "")
	requireCode(t, err, CodeCatalogLineageUnknown)
}

func TestLegacyV2PathUnchangedWhenBaseIDOmitted(t *testing.T) {
	tmp := t.TempDir()
	scope := filepath.Join(tmp, "beer-sample__default")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(scope, "chat_request_analytics_v2.json")
	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "analytics", Bucket: "beer-sample", Scope: "_default", UserDir: tmp,
	})
	if !ok || got != target {
		t.Fatalf("got %q", got)
	}
}

func TestBaseIDDoesNotRglobSiblingScope(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "yelp-demo__default"), 0o755); err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(tmp, "yelp-data__default")
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(sib, "chat_request_analytics_base-5.3.json"), map[string]any{
		"_lineage": map[string]any{"base_id": "base-5.3"},
	})
	got, ok := ResolveCatalogPath(CatalogResolveOpts{
		Mode: "analytics", Bucket: "yelp-demo", Scope: "_default", UserDir: tmp, BaseID: "base-5.3",
	})
	if ok {
		t.Fatalf("sibling %q", got)
	}
}

func TestLineageMismatchAndMissing(t *testing.T) {
	tmp := t.TempDir()
	writeJSON(t, filepath.Join(tmp, "chat_request_analytics_base-5.3.json"), map[string]any{
		"_lineage": map[string]any{"base_id": "base-5.2"},
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
	})
	doc := readJSONFile(t, filepath.Join(tmp, "chat_request_analytics_base-5.3.json"))
	err := CheckLineage(doc, "base-5.3", true, "chat_request_analytics_base-5.3.json")
	requireCode(t, err, CodeCatalogLineageUnknown)

	missing := map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}}
	err = CheckLineage(missing, "base-5.3", true, "chat_request_analytics_base-5.3.json")
	requireCode(t, err, CodeCatalogLineageUnknown)
}

func TestListIncludesLineageAndStats(t *testing.T) {
	tmp := t.TempDir()
	writeJSON(t, filepath.Join(tmp, "chat_request_analytics_base-5.3.json"), map[string]any{
		"_lineage": map[string]any{"base_id": "base-5.3"},
		"contract": map[string]any{"id": "analytics_base5", "hash": "md5:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"verbs":    []any{map[string]any{"name": "search"}, map[string]any{"name": "return"}},
		"messages": []any{map[string]any{"role": "system", "content": "x"}},
	})
	found := ListCatalogEntries(tmp, "")
	var hit CatalogListEntry
	ok := false
	for _, e := range found {
		if e.BaseID == "base-5.3" {
			hit = e
			ok = true
		}
	}
	if !ok {
		t.Fatalf("%+v", found)
	}
	if hit.Mode != "analytics" || hit.StampPresent != "true" || hit.LineageID != "base-5.3" {
		t.Fatalf("%+v", hit)
	}
	if hit.VerbCount != "2" {
		t.Fatalf("verbs %s", hit.VerbCount)
	}
}

func TestSuiteL0CatalogLoadMock(t *testing.T) {
	dir := mockFixtureDir(t)
	body := readJSONFile(t, filepath.Join(dir, "chat_request.mock.json"))
	schema := readJSONFile(t, filepath.Join(dir, "response_output_schema.min.json"))
	got := CatalogLoadMockExpect(body, schema)
	if got["catalog.base_id"] != "base-5" {
		t.Fatalf("base_id %v", got["catalog.base_id"])
	}
	if got["catalog.contract.id"] != "mock_analytics_b5" {
		t.Fatalf("id %v", got["catalog.contract.id"])
	}
	const mockHash = "mock:00000000000000000000000000000000"
	if got["catalog.contract.hash"] != mockHash {
		t.Fatalf("hash %v", got["catalog.contract.hash"])
	}
	if ExtractStampedHash(body) != mockHash {
		t.Fatal("extract")
	}
	computed := ComputeContractHash(body)
	if computed == mockHash {
		t.Fatal("mock hash must not equal local compute")
	}
	if got["catalog.verb_count_gte"].(int) < 2 {
		t.Fatalf("verbs %v", got["catalog.verb_count_gte"])
	}
	if got["schema.required_four_defined"] != true {
		t.Fatal("schema")
	}
	if body["_kit"] != "MOCK_NOT_FOR_PRODUCTION" {
		t.Fatal("kit")
	}
}

func TestUnstampedExtractIsNullSafe(t *testing.T) {
	doc := map[string]any{"messages": []any{map[string]any{"role": "system", "content": "x"}}}
	if ExtractStampedHash(doc) != "" {
		t.Fatal("extract empty")
	}
	_, err := StampedHash(doc)
	requireCode(t, err, CodeContractHashMissing)
	_, err = InventProductionHash(doc)
	requireCode(t, err, CodeContractHashInventForbidden)
}

func TestDocumentBaseIDPrefersLineageThenTopLevel(t *testing.T) {
	if DocumentBaseID(map[string]any{
		"_lineage": map[string]any{"base_id": "base-5.3"},
		"base_id":  "base-5",
	}) != "base-5.3" {
		t.Fatal("lineage")
	}
	if DocumentBaseID(map[string]any{
		"_lineage": "mock_base5_minimal",
		"base_id":  "base-5",
	}) != "base-5" {
		t.Fatal("mock kit string lineage")
	}
}
