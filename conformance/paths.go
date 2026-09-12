// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const pinsName = "sdk_bootstrap.pins.json"

// FindRepoRoot walks from cwd and this file toward the module root
// (go.mod + sdk_bootstrap.pins.json).
func FindRepoRoot() (string, error) {
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(file))
	}
	seen := map[string]struct{}{}
	for _, start := range starts {
		for d := start; ; d = filepath.Dir(d) {
			if _, ok := seen[d]; ok {
				break
			}
			seen[d] = struct{}{}
			if fileExists(filepath.Join(d, pinsName)) && fileExists(filepath.Join(d, "go.mod")) {
				return d, nil
			}
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
		}
	}
	return "", fmt.Errorf("zeus_client_golang repo root not found (need %s + go.mod)", pinsName)
}

// LoadPins reads sdk_bootstrap.pins.json from repoRoot.
func LoadPins(repoRoot string) (map[string]any, error) {
	p := filepath.Join(repoRoot, pinsName)
	if !fileExists(p) {
		return map[string]any{}, nil
	}
	return loadJSONMap(p)
}

// ResolveDesignRoot maps pins suite.design_repo_ref (local:../zeus_client_design)
// then falls back to the sibling directory. Missing both is an error.
func ResolveDesignRoot(repoRoot string, pins map[string]any) (string, error) {
	ref := "local:../zeus_client_design"
	if suite := childMap(pins, "suite"); suite != nil {
		if s := strings.TrimSpace(asString(suite["design_repo_ref"])); s != "" {
			ref = s
		}
	}
	var path string
	if strings.HasPrefix(ref, "local:") {
		rel := strings.TrimPrefix(ref, "local:")
		path = filepath.Clean(filepath.Join(repoRoot, rel))
	} else {
		path = filepath.Clean(ref)
	}
	if isDir(path) {
		return path, nil
	}
	alt := filepath.Clean(filepath.Join(filepath.Dir(repoRoot), "zeus_client_design"))
	if isDir(alt) {
		return alt, nil
	}
	return "", fmt.Errorf("design repo not found: %s", path)
}

// ManifestPath is designRoot/conformance/manifest.json.
func ManifestPath(designRoot string) string {
	return filepath.Join(designRoot, "conformance", "manifest.json")
}

// LoadManifest reads conformance/manifest.json.
func LoadManifest(designRoot string) (map[string]any, error) {
	return loadJSONMap(ManifestPath(designRoot))
}

func resolveRel(designRoot, rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) && fileExists(rel) {
		return rel
	}
	for _, cand := range []string{
		filepath.Join(designRoot, rel),
		filepath.Join(designRoot, "conformance", rel),
	} {
		if fileExists(cand) {
			return cand
		}
	}
	return filepath.Join(designRoot, rel)
}

func loadJSON(path string) (any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func loadJSONMap(path string) (map[string]any, error) {
	doc, err := loadJSON(path)
	if err != nil {
		return nil, err
	}
	m, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: want object got %T", path, doc)
	}
	return m, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func childMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func packageVersion(repoRoot string) string {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "version.go"))
	if err != nil {
		return "0.1.0"
	}
	const needle = `const Version = "`
	s := string(raw)
	i := strings.Index(s, needle)
	if i < 0 {
		return "0.1.0"
	}
	s = s[i+len(needle):]
	j := strings.Index(s, `"`)
	if j < 0 {
		return "0.1.0"
	}
	v := strings.TrimSpace(s[:j])
	if v == "" {
		return "0.1.0"
	}
	return v
}
