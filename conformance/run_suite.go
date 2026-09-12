// SPDX-License-Identifier: BUSL-1.1

package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	languageGo     = "go"
	packageModule  = "github.com/koten-ai/zeus_client_golang"
	defaultFloor   = "client-floor-5"
	defaultClaim   = "candidate"
	statusRequired = "required"
	statusRewind   = "required_rewind"
	statusSeed     = "seed_assert_only"
)

// RunOptions selects suite filters. Empty Levels uses pins required_levels
// plus detective/rewind (Python candidate offline set).
type RunOptions struct {
	RepoRoot    string
	Levels      map[string]struct{}
	IncludeSeed bool
}

// Summary is the report tally (schema total/passed/failed/skipped; error extra).
type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Error   int `json:"error"`
}

// CaseResult is one suite row.
type CaseResult struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"`
	Message    string         `json:"message,omitempty"`
	DurationMS int            `json:"duration_ms,omitempty"`
	ExpectDiff map[string]any `json:"expect_diff,omitempty"`
}

// Report matches conformance_report.schema.json (plus claim_level / error).
type Report struct {
	SuiteVersion   string       `json:"suite_version"`
	Language       string       `json:"language"`
	Package        string       `json:"package"`
	PackageVersion string       `json:"package_version"`
	ClientFloor    string       `json:"client_floor"`
	ClaimLevel     string       `json:"claim_level"`
	DesignRepo     string       `json:"design_repo,omitempty"`
	StartedAt      string       `json:"started_at"`
	FinishedAt     string       `json:"finished_at"`
	Summary        Summary      `json:"summary"`
	Cases          []CaseResult `json:"cases"`
}

func defaultLevels(pins map[string]any) map[string]struct{} {
	out := map[string]struct{}{}
	suite := childMap(pins, "suite")
	raw, _ := suite["required_levels"].([]any)
	if len(raw) == 0 {
		out["L0"] = struct{}{}
		out["L1"] = struct{}{}
		out["L2"] = struct{}{}
	} else {
		for _, v := range raw {
			if s := strings.TrimSpace(asString(v)); s != "" {
				out[s] = struct{}{}
			}
		}
	}
	out["L_detective"] = struct{}{}
	out["L_rewind"] = struct{}{}
	return out
}

func runOne(designRoot string, entry map[string]any) (out CaseResult) {
	caseID := asString(entry["id"])
	rel := asString(entry["path"])
	caseDir := filepath.Join(designRoot, "conformance", rel)
	casePath := filepath.Join(caseDir, "case.json")
	t0 := time.Now()
	defer func() {
		if rec := recover(); rec != nil {
			out = CaseResult{
				ID:         caseID,
				Status:     "error",
				Message:    fmt.Sprint(rec),
				DurationMS: int(time.Since(t0).Milliseconds()),
			}
		}
	}()
	if !fileExists(casePath) {
		return CaseResult{ID: caseID, Status: "error", Message: "missing " + casePath}
	}
	caseDoc, err := loadJSONMap(casePath)
	if err != nil {
		return CaseResult{ID: caseID, Status: "error", Message: err.Error(), DurationMS: int(time.Since(t0).Milliseconds())}
	}
	fn, ok := HANDLERS[caseID]
	if !ok {
		if strings.HasPrefix(caseID, "DT.") {
			return CaseResult{
				ID:         caseID,
				Status:     "error",
				Message:    "no Go handler",
				DurationMS: int(time.Since(t0).Milliseconds()),
			}
		}
		return CaseResult{
			ID:         caseID,
			Status:     "skipped",
			Message:    "no Go handler",
			DurationMS: int(time.Since(t0).Milliseconds()),
		}
	}
	observe, err := fn(designRoot, caseDir, caseDoc)
	ms := int(time.Since(t0).Milliseconds())
	if err != nil {
		return CaseResult{ID: caseID, Status: "error", Message: err.Error(), DurationMS: ms}
	}
	want := childMap(caseDoc, "expect")
	diffs := AssertCase(observe, want)
	if len(diffs) > 0 {
		n := len(diffs)
		if n > 8 {
			n = 8
		}
		return CaseResult{
			ID:         caseID,
			Status:     "failed",
			Message:    strings.Join(diffs[:n], "; "),
			DurationMS: ms,
			ExpectDiff: map[string]any{"diffs": diffs, "observe": safeJSON(observe)},
		}
	}
	return CaseResult{ID: caseID, Status: "passed", DurationMS: ms}
}

func safeJSON(obj any) any {
	if _, err := json.Marshal(obj); err != nil {
		return fmt.Sprint(obj)
	}
	return obj
}

// RunSuite loads the design manifest, runs required (+ rewind) cases, and
// returns a report. Missing design repo is an error (CLI hard-fail). go test
// skips before calling this when the sibling is absent.
func RunSuite(opts RunOptions) (Report, error) {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		var err error
		repoRoot, err = FindRepoRoot()
		if err != nil {
			return Report{}, err
		}
	}
	pins, err := LoadPins(repoRoot)
	if err != nil {
		return Report{}, err
	}
	designRoot, err := ResolveDesignRoot(repoRoot, pins)
	if err != nil {
		return Report{}, err
	}
	if !fileExists(ManifestPath(designRoot)) {
		return Report{}, fmt.Errorf("conformance manifest missing under %s", designRoot)
	}
	manifest, err := LoadManifest(designRoot)
	if err != nil {
		return Report{}, err
	}
	suiteVersion := asString(manifest["suite_version"])
	if suiteVersion == "" {
		suiteVersion = asString(childMap(pins, "suite")["suite_version"])
	}
	claim := childMap(pins, "claim")
	levels := opts.Levels
	if len(levels) == 0 {
		levels = defaultLevels(pins)
	} else {
		levels = cloneLevelSet(levels)
		levels["L_detective"] = struct{}{}
		levels["L_rewind"] = struct{}{}
	}

	started := time.Now().UTC().Format(time.RFC3339Nano)
	var casesOut []CaseResult
	entries, _ := manifest["cases"].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if entry == nil {
			continue
		}
		st := asString(entry["status"])
		if st == "" {
			st = statusRequired
		}
		if st == statusSeed && !opts.IncludeSeed {
			casesOut = append(casesOut, CaseResult{
				ID:      asString(entry["id"]),
				Status:  "skipped",
				Message: "seed_assert_only (pass --include-seed)",
			})
			continue
		}
		level := asString(entry["level"])
		if _, ok := levels[level]; !ok {
			continue
		}
		if st != statusRequired && st != statusRewind {
			casesOut = append(casesOut, CaseResult{
				ID:      asString(entry["id"]),
				Status:  "skipped",
				Message: "status=" + st,
			})
			continue
		}
		casesOut = append(casesOut, runOne(designRoot, entry))
	}
	finished := time.Now().UTC().Format(time.RFC3339Nano)
	sum := Summary{Total: len(casesOut)}
	for _, c := range casesOut {
		switch c.Status {
		case "passed":
			sum.Passed++
		case "failed":
			sum.Failed++
		case "skipped":
			sum.Skipped++
		case "error":
			sum.Error++
		}
	}
	floor := asString(claim["client_floor"])
	if floor == "" {
		floor = defaultFloor
	}
	claimLevel := asString(claim["claim_level"])
	if claimLevel == "" {
		claimLevel = defaultClaim
	}
	return Report{
		SuiteVersion:   suiteVersion,
		Language:       languageGo,
		Package:        packageModule,
		PackageVersion: packageVersion(repoRoot),
		ClientFloor:    floor,
		ClaimLevel:     claimLevel,
		DesignRepo:     designRoot,
		StartedAt:      started,
		FinishedAt:     finished,
		Summary:        sum,
		Cases:          casesOut,
	}, nil
}

func cloneLevelSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in)+2)
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

// WriteReport writes indented JSON plus a trailing newline.
func WriteReport(path string, report Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

// FormatSummary is the CLI / test one-liner.
func FormatSummary(r Report) string {
	s := r.Summary
	return fmt.Sprintf(
		"suite_version=%s passed=%d failed=%d error=%d skipped=%d total=%d",
		r.SuiteVersion, s.Passed, s.Failed, s.Error, s.Skipped, s.Total,
	)
}
