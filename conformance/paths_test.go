// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepoRoot(t *testing.T) {
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(root, "sdk_bootstrap.pins.json")) {
		t.Fatalf("pins missing under %s", root)
	}
}

func TestResolveDesignRootMissing(t *testing.T) {
	tmp := t.TempDir()
	pins := map[string]any{"suite": map[string]any{"design_repo_ref": "local:../no_such_design_repo_zcg23"}}
	_, err := ResolveDesignRoot(tmp, pins)
	if err == nil {
		t.Fatal("expected missing design repo")
	}
}

func TestPackageVersionFromVersionGo(t *testing.T) {
	root, err := FindRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	v := packageVersion(root)
	if v == "" {
		t.Fatal("empty version")
	}
	raw, err := os.ReadFile(filepath.Join(root, "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(root) {
		t.Fatal("repo root should be absolute")
	}
	_ = raw
	if v != "0.1.0-dev" {
		t.Fatalf("version %q (ZCG-23 must not bump; ZCG-21 owns semver)", v)
	}
}
