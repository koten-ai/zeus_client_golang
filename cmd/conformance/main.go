// SPDX-License-Identifier: BUSL-1.1

// Command conformance runs the offline P-Suite adapter against sibling
// zeus_client_design. Missing design repo is a hard fail (CHECKLIST E).
// go test ./conformance skips when the sibling is absent so GHA stays green.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/koten-ai/zeus_client_golang/conformance"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("conformance", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	levelsFlag := fs.String("levels", "L0,L1,L2,L_detective,L_rewind", "comma-separated levels")
	includeSeed := fs.Bool("include-seed", false, "include seed_assert_only cases")
	reportPath := fs.String("report", "", "write report JSON (default conformance/last_report.json)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := conformance.FindRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	out := *reportPath
	if out == "" {
		out = filepath.Join(root, "conformance", "last_report.json")
	}
	levels := map[string]struct{}{}
	for _, p := range strings.Split(*levelsFlag, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			levels[p] = struct{}{}
		}
	}
	report, err := conformance.RunSuite(conformance.RunOptions{
		RepoRoot:    root,
		Levels:      levels,
		IncludeSeed: *includeSeed,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := conformance.WriteReport(out, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println(conformance.FormatSummary(report))
	for _, c := range report.Cases {
		mark := map[string]string{
			"passed":  "OK",
			"failed":  "FAIL",
			"error":   "ERR",
			"skipped": "SKIP",
		}[c.Status]
		if mark == "" {
			mark = c.Status
		}
		msg := ""
		if c.Message != "" && c.Status != "passed" {
			msg = " — " + c.Message
		}
		fmt.Printf("  [%s] %s%s\n", mark, c.ID, msg)
	}
	fmt.Printf("report=%s\n", out)
	if report.Summary.Failed > 0 || report.Summary.Error > 0 {
		return 1
	}
	return 0
}
