package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}

func TestClaudeMdInjected(t *testing.T) { // AC1
	dir := t.TempDir()
	writeFile(t, dir, "CLAUDE.md", "# Rules\nCLAUDE_MD_SENTINEL_ABC123")
	res := harness.RunJennyInDir(t, dir, nil, "--print-system-prompt")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(strings.Join(res.Lines, "\n"), "CLAUDE_MD_SENTINEL_ABC123") {
		t.Error("AC1: CLAUDE.md content not found in system prompt")
	}
}

func TestAgentsMdFallback(t *testing.T) { // AC2
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "# Rules\nAGENTS_MD_SENTINEL_XYZ789")
	res := harness.RunJennyInDir(t, dir, nil, "--print-system-prompt")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(strings.Join(res.Lines, "\n"), "AGENTS_MD_SENTINEL_XYZ789") {
		t.Error("AC2: AGENTS.md content not found in system prompt")
	}
}

func TestClaudeMdPrecedenceOverAgentsMd(t *testing.T) { // AC3
	dir := t.TempDir()
	writeFile(t, dir, "CLAUDE.md", "CLAUDE_WINS_SENTINEL")
	writeFile(t, dir, "AGENTS.md", "AGENTS_LOSES_SENTINEL")
	res := harness.RunJennyInDir(t, dir, nil, "--print-system-prompt")
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "CLAUDE_WINS_SENTINEL") {
		t.Error("AC3: CLAUDE.md content not found")
	}
	if strings.Contains(text, "AGENTS_LOSES_SENTINEL") {
		t.Error("AC3: AGENTS.md content present but should not be")
	}
}

func TestNoInstructionFileNoInjection(t *testing.T) { // AC4
	dir := t.TempDir()
	res := harness.RunJennyInDir(t, dir, nil, "--print-system-prompt")
	text := strings.Join(res.Lines, "\n")
	for _, absent := range []string{"CLAUDE_MD_SENTINEL_ABC123", "AGENTS_MD_SENTINEL_XYZ789"} {
		if strings.Contains(text, absent) {
			t.Errorf("AC4: unexpected sentinel %q in prompt", absent)
		}
	}
}

func TestSubdirClaudeMdNotLoaded(t *testing.T) { // AC5
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	os.MkdirAll(subdir, 0755)
	writeFile(t, subdir, "CLAUDE.md", "SUBDIR_SENTINEL")
	res := harness.RunJennyInDir(t, dir, nil, "--print-system-prompt")
	if strings.Contains(strings.Join(res.Lines, "\n"), "SUBDIR_SENTINEL") {
		t.Error("AC5: subdir CLAUDE.md content found in system prompt but should not be")
	}
}
