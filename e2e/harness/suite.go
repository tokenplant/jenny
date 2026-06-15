package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// SuiteRunner orchestrates running e2e test cases.
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
	workDir, err := os.MkdirTemp(sr.Config.TempDir, "e2e-test-")
	if err != nil {
		return TestResult{
			ID:       tc.ID,
			Category: tc.Category,
			Status:   "error",
			Message:  "failed to create temp dir: " + err.Error(),
		}
	}
	defer os.RemoveAll(workDir)

	// Create WorkDirFiles if specified
	for relPath, content := range tc.WorkDirFiles {
		fullPath := filepath.Join(workDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return TestResult{
				ID:       tc.ID,
				Category: tc.Category,
				Status:   "error",
				Message:  "failed to create dir for " + relPath + ": " + err.Error(),
			}
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return TestResult{
				ID:       tc.ID,
				Category: tc.Category,
				Status:   "error",
				Message:  "failed to write " + relPath + ": " + err.Error(),
			}
		}
	}

	// Apply mock behavior and clear recorded requests
	if sr.mockServer != nil {
		sr.mockServer.ClearRequests()
		sr.mockServer.SetMockBehavior(tc.Target.MockBehavior)
	}

	// Build args and env based on invocation kind, expanding ${WORK_DIR}
	args, env := sr.buildArgs(tc), sr.buildEnv(tc, workDir)

	// Run the target (jenny)
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

	// Run reference binary comparison if requested
	var refResult RunResult
	if tc.Expected.StreamJSON != nil && tc.Expected.StreamJSON.CompareToReference {
		refResult = RunReferenceTargetInDir(nil, sr.Config, workDir, env, args...)
	}

	// Compare against expectations
	cmp := Compare(tc, actual, workDir)

	if cmp.Pass {
		result := TestResult{
			ID:       tc.ID,
			Category: tc.Category,
			Status:   "pass",
			Duration: time.Since(start).Milliseconds(),
		}
		// If reference comparison was requested, compute and store diffs
		if refResult.Parsed != nil {
			result.ReferenceDiff = CompareJSONLines(res, refResult)
		}
		return result
	}

	result := TestResult{
		ID:       tc.ID,
		Category: tc.Category,
		Status:   "fail",
		Duration: time.Since(start).Milliseconds(),
		Diff:     cmp.Diff,
		Actual:   actual,
	}
	// If reference comparison was requested, compute and store diffs
	if refResult.Parsed != nil {
		result.ReferenceDiff = CompareJSONLines(res, refResult)
	}
	return result
}

// buildArgs constructs CLI args from the invocation spec.
func (sr *SuiteRunner) buildArgs(tc *TestCase) []string {
	var args []string

	switch tc.Target.Kind {
	case "cli":
		args = tc.Target.Args
	case "prompt":
		args = []string{"--output-format", tc.Target.Format}
		// stream-json requires --verbose on some binaries (e.g. claude)
		if tc.Target.Format == "stream-json" {
			args = append(args, "--verbose")
		}
		args = append(args, "-p", tc.Target.Prompt)
		args = append(args, tc.Target.Args...)
	case "subprocess":
		args = tc.Target.Args
	default:
		args = tc.Target.Args
	}

	return args
}

// buildEnv constructs environment variables for the target invocation.
// workDir is substituted for ${WORK_DIR} in per-case env values.
func (sr *SuiteRunner) buildEnv(tc *TestCase, workDir string) []string {
	var env []string

	// Set JENNY_HOME to a subdirectory within the work directory to ensure isolation.
	env = append(env, "JENNY_HOME="+constants.ProjectJennyDir(workDir))

	// For prompt-kind tests, start mock server and set base URL
	if tc.Target.Kind == "prompt" && sr.Config.CassetteDir != "" {
		if sr.mockServer == nil {
			sr.mockServer = mockapi.NewMockServer(mockapi.WithCassetteDir(sr.Config.CassetteDir))
		}

		// Register multi-turn sequence if specified
		if len(tc.Target.CassetteSequence) > 0 {
			// If Cassette is empty, use the first sequence element as the base ID or a default
			cassetteID := tc.Target.Cassette
			if cassetteID == "" {
				cassetteID = "tool-use-req"
			}
			sr.mockServer.SetSequence(cassetteID, tc.Target.CassetteSequence)
		}

		if tc.Target.Cassette != "" {
			env = append(env, "ANTHROPIC_BASE_URL="+sr.mockServer.URL()+"/cassette/"+tc.Target.Cassette)
			env = append(env, "ANTHROPIC_AUTH_TOKEN=dummy-token")
		}
	}

	// Append per-case environment variables with ${WORK_DIR} and ${MOCK_URL} expansion
	mockURL := ""
	if sr.mockServer != nil {
		mockURL = sr.mockServer.URL()
	}
	for _, e := range tc.Target.Env {
		e = strings.ReplaceAll(e, "${WORK_DIR}", workDir)
		if mockURL != "" {
			e = strings.ReplaceAll(e, "${MOCK_URL}", mockURL)
		}
		env = append(env, e)
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
