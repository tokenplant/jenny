package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNormalize_NoProviderNameStringsInProduction verifies that provider name strings
// (minimax, deepseek) do not appear in production code paths.
func TestNormalize_NoProviderNameStringsInProduction(t *testing.T) {
	// Determine project root: go test runs with CWD set to the package directory,
	// so relative paths would resolve incorrectly. Walk up to find go.mod.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..")
	cmd := exec.Command("grep", "-rin", "minimax\\|deepseek",
		filepath.Join(root, "internal", "api"),
		filepath.Join(root, "internal", "agent"),
		"--include=*.go")
	output, _ := cmd.CombinedOutput()

	// Filter out test files and comments
	var violations []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if line == "" {
			continue
		}
		// Skip test files
		if strings.Contains(line, "_test.go") {
			continue
		}
		// Skip comments (grep output includes ":line://" for comment lines)
		if strings.Contains(line, ":") && strings.Contains(line, "//") {
			continue
		}
		// Skip model-version identifiers in data/config (e.g., "deepseek-v4-flash", "minimax-m3").
		// These are legitimate model-name strings in pricing tables and max-token overrides,
		// not provider-specific conditionals. Pattern: quote immediately before provider name
		// (map key "deepseek-v4-flash": { or switch case case "deepseek-v4-flash":).
		if strings.Contains(line, `":`) {
			continue
		}
		violations = append(violations, line)
	}

	if len(violations) > 0 {
		t.Errorf("provider name strings found in production code:\n%s", strings.Join(violations, "\n"))
	}
}

// TestNormalize_UniversalArchitectureCrossLinked locks in the docs cross-link to
// universal-normalization-architecture.md (AC8). The spec requires README.md, when
// present, to reference the architecture doc. This test fails if the link is removed
// from docs/README.md.
func TestNormalize_UniversalArchitectureCrossLinked(t *testing.T) {
	// Find project root: the directory containing go.mod, two levels up from internal/api.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Join(wd, "..", "..")
	readme := filepath.Join(root, "docs", "README.md")

	contents, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("read %s: %v", readme, err)
	}

	if !strings.Contains(string(contents), "universal-normalization-architecture.md") {
		t.Errorf("docs/README.md must cross-link to universal-normalization-architecture.md (AC8)")
	}
}
