package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// RunResult captures the outcome of running the jenny binary.
type RunResult struct {
	// Lines is the raw stdout of the jenny process, one entry per line
	// (newline-separated). Empty lines are preserved.
	Lines []string
	// Parsed is the subset of Lines that are valid JSON objects; blank
	// lines and non-JSON lines are skipped. The order matches the order
	// they appeared in Lines.
	Parsed []map[string]any
	// Stdout is the complete raw stdout of the jenny process.
	Stdout string
	// Stderr is the captured stderr of the jenny process.
	Stderr string
	// ExitCode is the process exit code. 0 on success; non-zero on
	// failure (e.g. 2 for usage errors, 1 for runtime errors).
	ExitCode int
	// Dir is the working directory of the jenny process.
	Dir string
}

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// RunJenny builds the jenny binary (once per test binary) and runs it with
// the given args. env entries are merged with the parent process environment
// and override any conflicting values; tests use this to inject
// ANTHROPIC_BASE_URL pointing at the mock server and a sentinel
// ANTHROPIC_AUTH_TOKEN.
//
// The build is cached with sync.Once; it is performed lazily on the first
// call. If the source has changed since the previous build, delete
// os.TempDir()/jenny_test_e2e to force a rebuild.
func RunJenny(t testing.TB, env []string, args ...string) RunResult {
	return RunJennyInDir(t, "", env, args...)
}

// RunJennyInDir runs the jenny binary in the specified directory. If dir is
// empty, it defaults to the repo root.
func RunJennyInDir(t testing.TB, dir string, env []string, args ...string) RunResult {
	t.Helper()

	bin, err := buildBinary()
	if err != nil {
		t.Fatalf("build jenny: %v", err)
	}

	if dir == "" {
		repoRoot, err := findRepoRoot()
		if err != nil {
			t.Fatalf("find repo root: %v", err)
		}
		dir = repoRoot
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = dir

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start jenny: %v", err)
	}

	runErr := cmd.Wait()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Split stdout into lines.
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(stdoutBuf.String()))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Parse lines as JSON, skipping blanks and lines that fail to parse.
	var parsed []map[string]any
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			parsed = append(parsed, m)
		}
	}

	return RunResult{
		Lines:    lines,
		Parsed:   parsed,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
		Dir:      cmd.Dir,
	}
}

// buildBinary compiles the jenny binary and returns its path. The result
// is cached: subsequent calls return the same path.
//
// The build runs from the module root (the directory containing go.mod)
// rather than the test process's current working directory, because
// `go test ./jenny_test/...` changes the working directory to the
// package directory.
func buildBinary() (string, error) {
	binaryOnce.Do(func() {
		tmpDir := filepath.Join(os.TempDir(), "jenny_test_e2e")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			binaryErr = err
			return
		}
		path := filepath.Join(tmpDir, "jenny")

		repoRoot, err := findRepoRoot()
		if err != nil {
			binaryErr = err
			return
		}

		cmd := exec.Command("go", "build", "-o", path, "./cmd/jenny")
		cmd.Dir = repoRoot
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			binaryErr = err
			return
		}
		binaryPath = path
	})
	return binaryPath, binaryErr
}

// findRepoRoot walks up from the test process's current working directory
// to find the directory containing go.mod. This is the module root and
// is where `go build ./cmd/jenny` is expected to run from.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", cwd)
		}
		dir = parent
	}
}
