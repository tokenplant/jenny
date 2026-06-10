package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// RunResult captures the outcome of running the target binary.
type RunResult struct {
	// Lines is the raw stdout split by newline.
	Lines []string
	// Parsed is the subset of Lines that are valid JSON objects.
	Parsed []map[string]any
	// Stdout is the complete raw stdout.
	Stdout string
	// Stderr is the captured stderr.
	Stderr string
	// ExitCode is the process exit code.
	ExitCode int
	// Dir is the working directory of the process.
	Dir string
	// DurationMs is the duration in milliseconds.
	DurationMs int64
}

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// RunTarget builds the target binary (once per test binary) and runs it with
// the given args. env entries are merged with the parent process environment.
func RunTarget(t ParityTB, cfg *Config, env []string, args ...string) RunResult {
	return RunTargetInDir(t, cfg, "", env, args...)
}

// RunTargetInDir runs the target binary in the specified directory.
func RunTargetInDir(t ParityTB, cfg *Config, dir string, env []string, args ...string) RunResult {
	if t != nil {
		t.Helper()
	}

	bin, err := buildBinary()
	if err != nil {
		if t != nil {
			t.Fatalf("build target: %v", err)
		}
		return RunResult{}
	}

	if dir == "" {
		repoRoot, err := findRepoRoot()
		if err != nil {
			if t != nil {
				t.Fatalf("find repo root: %v", err)
			}
			return RunResult{}
		}
		dir = repoRoot
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = dir

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	if err := cmd.Start(); err != nil {
		if t != nil {
			t.Fatalf("start target: %v", err)
		}
		return RunResult{}
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
	duration := time.Since(start)

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
		Lines:      lines,
		Parsed:     parsed,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		Dir:        cmd.Dir,
		DurationMs: duration.Milliseconds(),
	}
}

// buildBinary compiles the target binary and returns its path.
func buildBinary() (string, error) {
	binaryOnce.Do(func() {
		tmpDir := filepath.Join(os.TempDir(), "parity_test_e2e")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			binaryErr = err
			return
		}
		path := filepath.Join(tmpDir, "target")

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

// findRepoRoot walks up from the current directory to find go.mod.
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

// RunJenny builds the jenny binary (once) and runs it with the given args.
// This is a convenience wrapper for imperative tests that don't use the
// declarative TestCase/SuiteRunner infrastructure.
func RunJenny(t testing.TB, env []string, args ...string) RunResult {
	return RunJennyInDir(t, "", env, args...)
}

// RunJennyInDir runs the jenny binary in the specified directory.
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

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
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
	duration := time.Since(start)

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(stdoutBuf.String()))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

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
		Lines:      lines,
		Parsed:     parsed,
		Stdout:     stdoutBuf.String(),
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		Dir:        cmd.Dir,
		DurationMs: duration.Milliseconds(),
	}
}

// uuidV4Re matches a lowercase UUID v4 string.
var uuidV4Re = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// IsValidUUID checks if a string is a valid UUID v4.
func IsValidUUID(s string) bool {
	return uuidV4Re.MatchString(s)
}
