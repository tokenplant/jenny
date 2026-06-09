package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ipy/jenny/jenny_test/harness"
)

func runPrintSystemPrompt(t *testing.T) harness.RunResult {
	t.Helper()
	// No ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN — the flag must work
	// without any network credentials.
	return harness.RunJenny(t, nil, "--print-system-prompt")
}

// TestPrintSystemPromptFlag verifies AC1 and AC2.
func TestPrintSystemPromptFlag(t *testing.T) {
	res := runPrintSystemPrompt(t)
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	text := strings.Join(res.Lines, "\n")
	if len(text) == 0 {
		t.Fatal("stdout is empty")
	}
}

// TestSystemPromptToolList verifies AC3 and AC4.
func TestSystemPromptToolList(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	for _, want := range []string{"Available tools:", "Bash", "Read"} {
		if !strings.Contains(text, want) {
			t.Errorf("system prompt does not contain %q", want)
		}
	}
}

// TestSystemPromptCwd verifies AC5.
func TestSystemPromptCwd(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "Cwd:") {
		t.Error("system prompt does not contain 'Cwd:'")
	}
}

// TestSystemPromptSubstantial verifies AC2.
func TestSystemPromptSubstantial(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if len(text) < 1000 {
		t.Errorf("system prompt length %d < 1000", len(text))
	}
}

// TestSystemPromptIdentity verifies AC1.
func TestSystemPromptIdentity(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "You are an AI assistant") {
		t.Error("system prompt does not contain 'You are an AI assistant'")
	}
}

// TestSystemPromptBashSafety verifies AC3.
func TestSystemPromptBashSafety(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "destructive") && !strings.Contains(text, "rm -rf") {
		t.Error("system prompt does not contain bash safety guidance ('destructive' or 'rm -rf')")
	}
}

// TestSystemPromptSearchToolGuidance verifies AC4.
func TestSystemPromptSearchToolGuidance(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "Glob") || !strings.Contains(text, "Grep") {
		t.Error("system prompt does not contain 'Glob' and 'Grep' guidance")
	}
}

// TestSystemPromptNoTemplatePlaceholders verifies AC5.
func TestSystemPromptNoTemplatePlaceholders(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if strings.Contains(text, "{{") || strings.Contains(text, "}}") {
		t.Error("system prompt contains template placeholders ('{{' or '}}')")
	}
}

// TestSystemPromptDate verifies AC12 and AC13.
func TestSystemPromptDate(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	if !strings.Contains(text, "date") && !strings.Contains(text, "Date") {
		t.Error("AC12: system prompt does not contain 'date' or 'Date'")
	}
	year := fmt.Sprintf("%d", time.Now().Year())
	if !strings.Contains(text, year) {
		t.Errorf("AC13: system prompt does not contain current year %q", year)
	}
}

// TestSystemPromptOSInfo verifies AC14.
func TestSystemPromptOSInfo(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	for _, want := range []string{"Platform", "darwin", "linux", "windows"} {
		if strings.Contains(text, want) {
			return
		}
	}
	t.Error("AC14: system prompt does not contain OS/platform info")
}

// TestSystemPromptGitBranchInGitRepo verifies AC15.
func TestSystemPromptGitBranchInGitRepo(t *testing.T) {
	res := runPrintSystemPrompt(t)
	text := strings.Join(res.Lines, "\n")
	for _, want := range []string{"Branch", "branch", "Git context"} {
		if strings.Contains(text, want) {
			return
		}
	}
	t.Error("AC15: system prompt does not contain git branch/context when run inside a git repo")
}

// TestSystemPromptNoGitSectionOutsideRepo verifies AC16.
func TestSystemPromptNoGitSectionOutsideRepo(t *testing.T) {
	tmpDir := t.TempDir()
	res := harness.RunJennyInDir(t, tmpDir, nil, "--print-system-prompt")
	if res.ExitCode != 0 {
		t.Fatalf("AC16: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	text := strings.Join(res.Lines, "\n")
	for _, absent := range []string{"Git context", "Branch:"} {
		if strings.Contains(text, absent) {
			t.Errorf("AC16: system prompt contains %q but should not (not in a git repo)", absent)
		}
	}
}
