// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func requireDesignRoot(t *testing.T) string {
	t.Helper()
	root, err := FindRepoRoot()
	if err != nil {
		t.Skip(err.Error())
	}
	pins, err := LoadPins(root)
	if err != nil {
		t.Skip(err.Error())
	}
	design, err := ResolveDesignRoot(root, pins)
	if err != nil || !fileExists(ManifestPath(design)) {
		msg := "conformance manifest missing. Local CHECKLIST E requires sibling zeus_client_design; CI skips until the private design repo is cloned."
		if design != "" {
			msg = "conformance manifest missing under " + design + ". Local CHECKLIST E requires sibling zeus_client_design; CI skips until the private design repo is cloned."
		}
		t.Skip(msg)
	}
	return design
}

func TestConformanceSuiteCandidateOffline(t *testing.T) {
	design := requireDesignRoot(t)
	if !fileExists(ManifestPath(design)) {
		t.Fatal("manifest")
	}
	report, err := RunSuite(RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	repo, err := FindRepoRoot()
	if err == nil {
		_ = WriteReport(filepath.Join(repo, "conformance", "last_report.json"), report)
	}
	if report.SuiteVersion != "conformance-0.2-dev" {
		t.Fatalf("suite_version %q", report.SuiteVersion)
	}
	if report.Language != "go" {
		t.Fatalf("language %q", report.Language)
	}
	if report.ClaimLevel != "candidate" {
		t.Fatalf("claim_level %q", report.ClaimLevel)
	}
	if report.Package != "github.com/koten-ai/zeus_client_golang" {
		t.Fatalf("package %q", report.Package)
	}
	if report.PackageVersion != "0.1.0" {
		t.Fatalf("package_version %q", report.PackageVersion)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"suite_version", "language", "package", "package_version",
		"client_floor", "started_at", "finished_at", "summary", "cases",
	} {
		if _, ok := asMap[k]; !ok {
			t.Errorf("missing report key %s", k)
		}
	}
	s := report.Summary
	if s.Failed != 0 || s.Error != 0 {
		failed, _ := json.MarshalIndent(failedCases(report), "", "  ")
		t.Fatalf("summary failed=%d error=%d\n%s", s.Failed, s.Error, failed)
	}
	if s.Passed < 10 {
		t.Fatalf("passed %d want >= 10", s.Passed)
	}
	ids := map[string]struct{}{}
	for _, c := range report.Cases {
		if c.Status == "passed" {
			ids[c.ID] = struct{}{}
		}
	}
	need := []string{
		"L0.catalog.load_mock.001",
		"L1.loop.single_tool_return.001",
		"L1.loop.force_return_max_rounds.001",
		"L2.layer_a.required_four.001",
		"L2.policy.matrix.001",
		"DT.smooth_short.beer_fruit_pipeline_base61.001",
		"DT.fail_zeus.contract_409.001",
		"DT.fail_client.missing_mini_schema.001",
		"DT.fail_llm.bad_layer_a.001",
		"DT.fail_control_plane.trigger_missing_data_ok.001",
	}
	for _, id := range need {
		if _, ok := ids[id]; !ok {
			t.Errorf("required case not passed: %s", id)
		}
	}
}

func failedCases(r Report) []CaseResult {
	var out []CaseResult
	for _, c := range r.Cases {
		if c.Status == "failed" || c.Status == "error" {
			out = append(out, c)
		}
	}
	return out
}

func TestSummaryHasSchemaFields(t *testing.T) {
	s := Summary{}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"total", "passed", "failed", "skipped"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
}

func TestWriteReportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	in := Report{
		SuiteVersion:   "conformance-0.2-dev",
		Language:       "go",
		Package:        packageModule,
		PackageVersion: "0.1.0",
		ClientFloor:    "client-floor-5",
		ClaimLevel:     "candidate",
		StartedAt:      "2026-01-01T00:00:00Z",
		FinishedAt:     "2026-01-01T00:00:01Z",
		Summary:        Summary{Total: 1, Passed: 1},
		Cases:          []CaseResult{{ID: "L0.catalog.load_mock.001", Status: "passed"}},
	}
	if err := WriteReport(path, in); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out Report
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.SuiteVersion != in.SuiteVersion || out.Summary.Passed != 1 {
		t.Fatalf("%+v", out)
	}
}
