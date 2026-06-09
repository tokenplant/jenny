package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SuiteRunner orchestrates running parity test cases.
type SuiteRunner struct {
	Config     *Config
	Tests      []*TestCase
	mockServer *MockServer
}

// NewSuiteRunner creates a new test suite runner.
func NewSuiteRunner(cfg *Config, tests []*TestCase) *SuiteRunner {
	return &SuiteRunner{
		Config: cfg,
		Tests:  tests,
	}
}

// RunAll runs all test cases and reports results.
func (sr *SuiteRunner) RunAll(reporter Reporter) []TestResult {
	reporter.OnStart(len(sr.Tests))
	results := make([]TestResult, 0, len(sr.Tests))

	for _, tc := range sr.Tests {
		result := sr.RunOne(tc)
		results = append(results, result)
		reporter.OnResult(result)
	}

	reporter.OnEnd(results)
	sr.Close()
	return results
}

// Close cleans up resources held by the suite runner.
func (sr *SuiteRunner) Close() {
	if sr.mockServer != nil {
		sr.mockServer.Close()
		sr.mockServer = nil
	}
}

// RunOne runs a single test case.
func (sr *SuiteRunner) RunOne(tc *TestCase) TestResult {
	start := time.Now()

	// Check skip conditions
	if tc.Skip != nil {
		return TestResult{
			ID:         tc.ID,
			Category:   tc.Category,
			Status:     "skip",
			Duration:   time.Since(start).Milliseconds(),
			SkipReason: tc.Skip.Reason,
		}
	}

	// Setup work directory
	workDir, err := os.MkdirTemp(sr.Config.TempDir, "parity-test-")
	if err != nil {
		return TestResult{
			ID:       tc.ID,
			Category: tc.Category,
			Status:   "error",
			Message:  "failed to create temp dir: " + err.Error(),
		}
	}
	defer os.RemoveAll(workDir)

	// Build args and env based on invocation kind
	args, env := sr.buildArgs(tc), sr.buildEnv(tc)

	// Run the target
	res := RunTargetInDir(nil, sr.Config, workDir, env, args...)

	// Capture output
	actual := &CapturedOutput{
		ExitCode: res.ExitCode,
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
	}

	// If mock server was used, capture requests
	if tc.Target.Kind == "prompt" && sr.mockServer != nil {
		reqs := sr.mockServer.Requests()
		actual.Requests = make([]RecordedRequest, len(reqs))
		for i, r := range reqs {
			actual.Requests[i] = RecordedRequest{Body: r.Body}
		}
	}

	// Compare against expectations
	cmp := Compare(tc, actual)

	if cmp.Pass {
		return TestResult{
			ID:       tc.ID,
			Category: tc.Category,
			Status:   "pass",
			Duration: time.Since(start).Milliseconds(),
		}
	}

	return TestResult{
		ID:       tc.ID,
		Category: tc.Category,
		Status:   "fail",
		Duration: time.Since(start).Milliseconds(),
		Diff:     cmp.Diff,
		Actual:   actual,
	}
}

// buildArgs constructs CLI args from the invocation spec.
func (sr *SuiteRunner) buildArgs(tc *TestCase) []string {
	var args []string

	switch tc.Target.Kind {
	case "cli":
		args = tc.Target.Args
	case "prompt":
		args = []string{"--output-format", tc.Target.Format, "-p", tc.Target.Prompt}
	case "subprocess":
		args = tc.Target.Args
	default:
		args = tc.Target.Args
	}

	return args
}

// buildEnv constructs environment variables for the target invocation.
func (sr *SuiteRunner) buildEnv(tc *TestCase) []string {
	var env []string

	// For prompt-kind tests, start mock server and set base URL
	if tc.Target.Kind == "prompt" && tc.Target.Cassette != "" && sr.Config.CassetteDir != "" {
		// Start mock server if not already running
		if sr.mockServer == nil {
			sr.mockServer = NewMockServer(sr.Config.CassetteDir)
		}
		// URL includes /cassette/<id> prefix so jenny's /v1/messages calls
		// are routed to /cassette/<id>/v1/messages by the mock server
		env = append(env, "ANTHROPIC_BASE_URL="+sr.mockServer.URL()+"/cassette/"+tc.Target.Cassette)
	}

	return env
}

// LoadCasesFromDir discovers test cases from Go test files in the directory.
func LoadCasesFromDir(dir string) ([]*TestCase, error) {
	var cases []*TestCase

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subCases, err := LoadCasesFromDir(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			cases = append(cases, subCases...)
			continue
		}

		if strings.HasSuffix(entry.Name(), "_test.go") {
			// For now, we don't auto-discover - tests are registered explicitly
		}
	}

	return cases, nil
}
