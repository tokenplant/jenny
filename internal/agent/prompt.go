// Package agent provides the core agent loop.
package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ipy/jenny/internal/git"
	"github.com/ipy/jenny/internal/redact"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

const (
	// maxGitStatusChars is the maximum length of git status output before truncation.
	maxGitStatusChars = 2000
)

// defaultIntroSection returns the default introduction section of the system prompt.
func defaultIntroSection() (string, bool) {
	return `You are an autonomous AI assistant with tools to search, read, write, and execute safe operations. You operate in a non-interactive mode.
Your mission: autonomously complete every assigned task to the best of your ability, using all available means.

**Core mandates:**
- Strictly obey all rules and instructions in the <system-reminder> block.
- Never ask the user for clarification, input, or permission mid-task. You are running non-interactively.
- Exhaust every available avenue on your own: search, read files, run diagnostics, reason step-by-step. Keep trying until the task is done or you have truly reached a dead end.
- Be thorough before acting. Gather all necessary context first. Verify assumptions from actual data; never guess about current implementation details.
- Do not execute destructive or irreversible actions (rm -rf, git clean -fd, etc.) unless the user explicitly requested them and you are certain of the impact.
- Be concise and accurate. Your final output must be a plain message (if JSON is required, output only the raw JSON, no extra commentary or fences).`, true
}

// toolListSection returns a section listing all available tools.
func toolListSection(tools []tool.Tool) (string, bool) {
	if len(tools) == 0 {
		return "", false
	}

	var names []string
	for _, t := range tools {
		names = append(names, t.Name())
	}
	return fmt.Sprintf("Available tools: %s", strings.Join(names, ", ")), true
}

// gitStatusSection returns a section with git status information if in a git repo.
func gitStatusSection(cwd string) (string, bool) {
	root, err := git.GetRoot(cwd)
	if err != nil {
		// Not in a git repository
		return "", false
	}

	branch, err := git.GetBranch(root)
	if err != nil {
		return "", false
	}

	head, err := git.GetHead(root)
	if err != nil {
		return "", false
	}

	// Get git status --short output
	statusOutput, _ := getGitStatusShort(cwd)

	var section strings.Builder
	section.WriteString("Git context:\n")
	fmt.Fprintf(&section, "  Branch: %s\n", branch)
	if head != "" {
		fmt.Fprintf(&section, "  HEAD: %s\n", head)
	}
	if statusOutput != "" {
		section.WriteString("  Status:\n")
		// Cap at maxGitStatusChars
		if len(statusOutput) > maxGitStatusChars {
			statusOutput = statusOutput[:maxGitStatusChars] + "\n... (truncated)"
		}
		// Indent each line
		for line := range strings.SplitSeq(statusOutput, "\n") {
			if line != "" {
				fmt.Fprintf(&section, "    %s\n", line)
			}
		}
	}

	return section.String(), true
}

// getGitStatusShort runs `git status --short` and returns the output.
func getGitStatusShort(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--short")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// platformSection returns a section with platform, date, and cwd context.
func platformSection(cwd string) (string, bool) {
	if cwd == "" {
		cwd, _ = os.Getwd()
		if cwd == "" {
			cwd, _ = os.UserHomeDir()
		}
	}
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	date := time.Now().Format("2006-01-02")
	section := fmt.Sprintf("Platform: %s\nDate: %s\nCwd: %s", platform, date, cwd)

	// Add Windows-specific hints when running on Windows
	if runtime.GOOS == "windows" {
		section += "\n\nYou are running on Windows. Use the PowerShell tool for system commands. Be aware of Windows file path conventions (e.g., C:\\path\\to\\file)."
	}

	return section, true
}

// appendSection returns the append prompt if set and not overridden.
func appendSection(appendPrompt string, override bool) (string, bool) {
	if override || appendPrompt == "" {
		return "", false
	}
	return appendPrompt, true
}

// AssembleSystemPrompt builds the system prompt from sections based on configuration.
// Each section is a function returning (content, shouldInclude).
// On the first call, the result should be frozen by the caller into cfg.CachedSystemPrompt
// so that subsequent calls return the identical string, protecting Anthropic's prompt
// caching from dynamic variation (date, git status).
func AssembleSystemPrompt(cfg StreamConfig, tools []tool.Tool, cwd string) string {
	// Return frozen prompt if already assembled
	if cfg.CachedSystemPrompt != "" {
		return cfg.CachedSystemPrompt
	}
	return buildSystemPrompt(cfg, tools, cwd)
}

// buildSystemPrompt assembles the system prompt sections (the actual builder).
// Exported for testing; use AssembleSystemPrompt in production.
func buildSystemPrompt(cfg StreamConfig, tools []tool.Tool, cwd string) string {
	// AC1: Custom prompt replaces all defaults
	if cfg.CustomSystemPrompt != "" {
		var result strings.Builder
		result.WriteString(cfg.CustomSystemPrompt)

		// AC5: Append section always checked last, independent of custom/default
		if !cfg.OverrideSystemPrompt && cfg.AppendSystemPrompt != "" {
			result.WriteString("\n\n")
			result.WriteString(cfg.AppendSystemPrompt)
		}

		return result.String()
	}

	// Assemble default sections: intro + tool list + git + platform
	var sections []string

	// Default intro first — this is the stable, cache-friendly part
	if intro, ok := defaultIntroSection(); ok {
		sections = append(sections, intro)
	}

	// AC1: Memory content injected as <system-reminder> block
	// Placed after intro so prompt caching can hit the stable prefix;
	// MemoryContent is per-session/per-conversation and would otherwise
	// bust the cache on every new session.
	if cfg.MemoryContent != "" {
		sections = append(sections, "<system-reminder>\n"+cfg.MemoryContent+"\n</system-reminder>")
	}

	// AC2: Tool list sync
	if toolList, ok := toolListSection(tools); ok {
		sections = append(sections, toolList)
	}

	// AC3: Git status injection (only inside repo) — captured once at session start
	if gitStatus, ok := gitStatusSection(cwd); ok {
		sections = append(sections, gitStatus)
	}

	// AC4: Platform/cwd context — date is captured once at session start
	if platform, ok := platformSection(cwd); ok {
		sections = append(sections, platform)
	}

	// Skills manifest (AC2)
	if len(cfg.Skills) > 0 {
		if manifest := skills.SkillsManifest(cfg.Skills); manifest != "" {
			sections = append(sections, manifest)
		}
	}

	// AC9: Redaction instruction in system prompt when enabled
	if cfg.RedactMode != redact.ModeDisabled {
		prompt := "This session has secret redaction enabled. Tool results may contain `[REDACTED:<hex>]` placeholders (e.g. `[REDACTED:a3f1b2c9]`). Copy them verbatim — including the full hex suffix — and never simplify, abbreviate, or otherwise modify them."
		if redact.ParseRedactMode(string(cfg.RedactMode)) == redact.ModeRecover {
			prompt += " They will be automatically recovered when you use them in tool calls, so you can refer to them directly as needed."
		}
		sections = append(sections, prompt)
	}

	// AC5: Append section (only if not overridden)
	if appendContent, ok := appendSection(cfg.AppendSystemPrompt, cfg.OverrideSystemPrompt); ok {
		sections = append(sections, appendContent)
	}

	// AC1: Trailing newline so the shell prompt does not run onto the last line.
	// The caller of --print-system-prompt used to print with fmt.Print and rely on
	// any trailing whitespace the joined sections happened to have — there was none.
	return strings.Join(sections, "\n\n") + "\n"
}

// DynamicSystemSuffix is intentionally empty. All dynamic content (active skills,
// cwd changes, date changes) is communicated through virtual user messages in the
// message chain instead of via the system prompt. This ensures the system prompt
// prefix is byte-stable across turns, preventing cache invalidation of the entire
// message chain when the suffix changes.
func DynamicSystemSuffix(cfg StreamConfig, cwd string) string {
	return ""
}

// activeSkillsSection returns the "Active Skills" section for the system prompt.
// Returns empty string if no skills are active.
func activeSkillsSection(activeSkills []ActivatedSkill) string {
	if len(activeSkills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "Active Skills:")
	for _, skill := range activeSkills {
		lines = append(lines, fmt.Sprintf("- %s: %s", skill.Name, skill.RootPath))
	}
	return strings.Join(lines, "\n")
}
