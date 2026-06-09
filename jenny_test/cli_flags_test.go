// Package e2e_test contains blackbox end-to-end tests for jenny.
//
// The tests in this package spawn the jenny binary as a subprocess and
// assert on its stdout, stderr, exit code, and (for stream-json tests)
// the HTTP traffic it emits against an in-process mock server.
package e2e_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

// versionFormatRe matches the full version format: semver + parenthesized product name.
var versionFormatRe = regexp.MustCompile(`^\d+\.\d+\.\d+\s*\([^)]+\)$`)

// versionRe matches a semver-like version string X.Y.Z.
var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// TestVersionFlag is the AC2 smoke test for the `--version` flag and
// its `-v` short alias. Both must exit 0 and emit a line matching the
// semver pattern on stdout; the binary exits before any session or
// API setup, so no network call is made.
func TestVersionFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "long", args: []string{"--version"}},
		{name: "short", args: []string{"-v"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := harness.RunJenny(t, nil, tc.args...)

			if res.ExitCode != 0 {
				t.Fatalf("%s: expected exit 0, got %d; stderr=%q", tc.args[0], res.ExitCode, res.Stderr)
			}

			if len(res.Lines) == 0 {
				t.Fatalf("%s: expected at least one line of stdout; got 0; stderr=%q", tc.args[0], res.Stderr)
			}
			if !versionRe.MatchString(res.Lines[0]) {
				t.Errorf(
					"%s: first stdout line %q does not match version pattern %q",
					tc.args[0], res.Lines[0], versionRe,
				)
			}
		})
	}
}

// TestVersionFormat verifies AC1/AC2: --version and -v output matches semver+(product) format.
func TestVersionFormat(t *testing.T) {
	for _, arg := range []string{"--version", "-v"} {
		res := harness.RunJenny(t, nil, arg)
		if res.ExitCode != 0 {
			t.Errorf("%s: exit %d", arg, res.ExitCode)
			continue
		}
		if len(res.Lines) == 0 {
			t.Errorf("%s: no stdout lines", arg)
			continue
		}
		if !versionFormatRe.MatchString(res.Lines[0]) {
			t.Errorf("%s: first line %q does not match %q", arg, res.Lines[0], versionFormatRe)
		}
	}
}

// TestUnknownFlag verifies AC3/AC4: unknown flag exits non-zero with an error on stderr.
func TestUnknownFlag(t *testing.T) {
	res := harness.RunJenny(t, nil, "--unknown-flag-xyz-sentinel-parity")
	if res.ExitCode == 0 {
		t.Fatal("AC3: expected non-zero exit for unknown flag, got 0")
	}
	lowStderr := strings.ToLower(res.Stderr)
	if !strings.Contains(lowStderr, "unknown") &&
		!strings.Contains(lowStderr, "flag provided but not defined") &&
		!strings.Contains(lowStderr, "unrecognized") {
		t.Errorf("AC4: stderr %q does not contain expected error signal", res.Stderr)
	}
}

// TestTextOutputMode verifies AC5: --output-format text produces plain prose, not NDJSON.
func TestTextOutputMode(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "text", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC5: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	if len(res.Lines) == 0 {
		t.Fatal("AC5: no stdout output in text mode")
	}
	for i, line := range res.Lines {
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			t.Errorf("AC5: line %d looks like NDJSON: %q", i, line)
		}
	}
}

// TestHelpFlag is the AC3 smoke test for the `--help` flag.
func TestHelpFlag(t *testing.T) {
	res := harness.RunJenny(t, nil, "--help")

	if res.ExitCode != 0 {
		t.Fatalf("--help exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	combined := strings.ToLower(strings.Join(res.Lines, "\n") + "\n" + res.Stderr)
	if !strings.Contains(combined, "usage") {
		t.Errorf(
			"expected combined stdout+stderr to contain 'usage' (case-insensitive); got: %q",
			combined,
		)
	}

	if !strings.Contains(combined, "print-system-prompt") {
		t.Errorf("--help output does not mention 'print-system-prompt'")
	}
}
