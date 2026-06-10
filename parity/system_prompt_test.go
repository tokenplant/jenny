package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestSystemPromptDefault verifies default system prompt assembly.
func TestSystemPromptDefault(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "system-prompt.default.contains-identity",
			Category:    "system-prompt",
			Description: "default prompt starts with AI assistant identity",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"AI assistant"},
				},
			},
		},
		{
			ID:          "system-prompt.default.mentions-bash-safety",
			Category:    "system-prompt",
			Description: "default prompt includes bash safety language",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"destructive", "rm -rf", "careful", "caution"},
				},
			},
		},
		{
			ID:          "system-prompt.default.mentions-search-tools",
			Category:    "system-prompt",
			Description: "default prompt names Glob and Grep",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Glob", "Grep"},
				},
			},
		},
		{
			ID:          "system-prompt.default.minimum-length",
			Category:    "system-prompt",
			Description: "assembled prompt is at least 1000 chars",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Length: &harness.LengthExpectation{Min: 1000},
				},
			},
		},
		{
			ID:          "system-prompt.default.no-unfilled-templates",
			Category:    "system-prompt",
			Description: "no unfilled template placeholders in output",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"{{", "}}"},
				},
			},
		},
	})
}

// TestSystemPromptContext verifies dynamic context injection.
func TestSystemPromptContext(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "system-prompt.context.has-date",
			Category:    "system-prompt",
			Description: "prompt includes current date",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Date", "date", "2026"},
				},
			},
		},
		{
			ID:          "system-prompt.context.has-platform",
			Category:    "system-prompt",
			Description: "prompt includes OS/platform info",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Platform", "platform", "darwin", "linux", "windows"},
				},
			},
		},
		{
			ID:          "system-prompt.context.has-cwd",
			Category:    "system-prompt",
			Description: "prompt includes working directory",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"/"},
				},
			},
		},
	})
}

// TestSystemPromptCustom verifies --system-prompt replacement.
func TestSystemPromptCustom(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "system-prompt.custom.replaces-default",
			Category:    "system-prompt",
			Description: "--system-prompt replaces all default sections",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--system-prompt", "You are a custom test agent.", "--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains:    []string{"custom test agent"},
					NotContains: []string{"AI assistant"},
				},
			},
		},
	})
}

// TestSystemPromptAppend verifies --append-system-prompt.
func TestSystemPromptAppend(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "system-prompt.append.added-after-default",
			Category:    "system-prompt",
			Description: "--append-system-prompt appends after default sections",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--append-system-prompt", "APPENDED_CUSTOM_TEXT_XYZ", "--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"AI assistant", "APPENDED_CUSTOM_TEXT_XYZ"},
				},
			},
		},
		{
			ID:          "system-prompt.append.with-custom-prompt",
			Category:    "system-prompt",
			Description: "--append-system-prompt works with --system-prompt",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{
					"--system-prompt", "Custom base prompt.",
					"--append-system-prompt", "APPEND_AFTER_CUSTOM",
					"--print-system-prompt",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Custom base prompt", "APPEND_AFTER_CUSTOM"},
				},
			},
		},
	})
}

// TestSystemPromptInstructionFiles verifies CLAUDE.md / AGENTS.md loading.
func TestSystemPromptInstructionFiles(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "system-prompt.instruction.loads-claude-md",
			Category:    "system-prompt",
			Description: "CLAUDE.md from cwd appears in system prompt",
			WorkDirFiles: map[string]string{
				"CLAUDE.md": "## Project Rules\nAlways use gofmt.\n",
			},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Always use gofmt"},
				},
			},
		},
		{
			ID:          "system-prompt.instruction.agents-md-fallback",
			Category:    "system-prompt",
			Description: "AGENTS.md used when CLAUDE.md absent",
			WorkDirFiles: map[string]string{
				"AGENTS.md": "## Agent Rules\nBe concise.\n",
			},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Be concise"},
				},
			},
		},
		{
			ID:          "system-prompt.instruction.claude-md-wins",
			Category:    "system-prompt",
			Description: "CLAUDE.md takes precedence when both exist",
			WorkDirFiles: map[string]string{
				"CLAUDE.md": "## Claude Rules\nUse tabs.\n",
				"AGENTS.md": "## Agent Rules\nUse spaces.\n",
			},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains:    []string{"Use tabs"},
					NotContains: []string{"Use spaces"},
				},
			},
		},
		{
			ID:          "system-prompt.instruction.subdir-not-loaded",
			Category:    "system-prompt",
			Description: "subdirectory CLAUDE.md is not loaded",
			WorkDirFiles: map[string]string{
				"subdir/CLAUDE.md": "## Subdir Rules\nSubdir content.\n",
			},
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"Subdir content"},
				},
			},
		},
	})
}

// TestSystemPromptToolList verifies tool list matches registered tools.
func TestSystemPromptToolList(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "system-prompt.tools.has-available-tools-line",
			Category:    "system-prompt",
			Description: "system prompt has 'Available tools:' line",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Available tools"},
				},
			},
		},
		{
			ID:          "system-prompt.tools.mentions-read",
			Category:    "system-prompt",
			Description: "system prompt tool list includes Read",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Read"},
				},
			},
		},
		{
			ID:          "system-prompt.tools.mentions-bash",
			Category:    "system-prompt",
			Description: "system prompt tool list includes Bash",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Bash"},
				},
			},
		},
		{
			ID:          "system-prompt.tools.mentions-glob",
			Category:    "system-prompt",
			Description: "system prompt tool list includes Glob",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Glob"},
				},
			},
		},
		{
			ID:          "system-prompt.tools.mentions-grep",
			Category:    "system-prompt",
			Description: "system prompt tool list includes Grep",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Grep"},
				},
			},
		},
		{
			ID:          "system-prompt.tools.mentions-coding-capabilities",
			Category:    "system-prompt",
			Description: "system prompt describes read/write/edit capabilities",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"read", "write", "edit", "editing"},
				},
			},
		},
	})
}
