package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

const (
	cassetteDir = "fixtures/cassettes"
	targetPath  = "../cmd/jenny"
	timeoutMs   = 60000
)

func defaultConfig() *harness.Config {
	return &harness.Config{
		ProductName: "jenny",
		Target:      targetPath,
		CassetteDir: cassetteDir,
		TimeoutMs:   timeoutMs,
	}
}

func runE2ESuite(t *testing.T, tests []*harness.TestCase) {
	t.Helper()
	cfg := defaultConfig()
	runner := harness.NewSuiteRunner(cfg, tests)
	reporter := &harness.TextReporter{}
	results := runner.RunAll(reporter)

	for _, r := range results {
		switch r.Status {
		case "fail":
			t.Errorf("FAIL %s: %s", r.ID, r.Message)
			for _, d := range r.Diff {
				t.Logf("  %s: expected=%v actual=%v (%s)", d.Path, d.Expected, d.Actual, d.Message)
			}
			if r.Actual != nil {
				if len(r.Actual.Stderr) > 0 && len(r.Actual.Stderr) < 500 {
					t.Logf("  stderr: %s", r.Actual.Stderr)
				}
			}
		case "error":
			t.Errorf("ERROR %s: %s", r.ID, r.Message)
		}
		// Log reference diffs if present
		if len(r.ReferenceDiff) > 0 {
			t.Logf("  reference alignment diffs for %s:", r.ID)
			for _, d := range r.ReferenceDiff {
				t.Logf("  DIFF: %s", d)
			}
		}
	}
}
