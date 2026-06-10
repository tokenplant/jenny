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
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

const (
	// maxGitStatusChars is the maximum length of git status output before truncation.
	maxGitStatusChars = 2000
)

// defaultIntroSection returns the default introduction section of the system prompt.
func defaultIntroSection() (string, bool) {
	return `You are an AI assistant with access to powerful tools for software engineering. You are an expert programmer with access to various tools that allow you to read, write, and analyze code. Your goal is to help users solve complex engineering tasks efficiently and safely.

When performing tasks, you should follow these principles:
1. **Instruction Adherence:** Always prioritize and strictly follow the instructions and rules found in the <system-reminder> block at the beginning of this prompt. These are foundational mandates for the current project.
2. Thoroughly investigate the codebase before making changes. Use tools like Glob and Grep to find relevant files, understand patterns, and ensure you have all necessary context.
3. Always verify your assumptions by reading the actual source code and documentation. Never guess about implementation details.
4. Be extremely cautious with destructive operations. Avoid running commands like "rm -rf", "git clean -fd", or other potentially harmful bash commands unless you are absolutely certain of their impact and the user has explicitly requested such an action.
5. Provide clear, concise, and accurate information. When you have finished a task, synthesize the results of your tool calls to give a direct and helpful answer.
6. Maintain a professional, efficient, and objective tone. Act as a reliable and proactive partner in problem-solving.

Your capabilities include searching the filesystem, reading and editing files, running shell commands, and integrating with external tools. You should always use the most appropriate tool for each step of your workflow, and explain your reasoning when it helps the user understand your progress.`, true
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
	section.WriteString(fmt.Sprintf("  Branch: %s\n", branch))
	if head != "" {
		section.WriteString(fmt.Sprintf("  HEAD: %s\n", head))
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
				section.WriteString("    " + line + "\n")
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
			cwd = "/"
		}
	}
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	date := time.Now().Format("2006-01-02")
	return fmt.Sprintf("Platform: %s\nDate: %s\nCwd: %s", platform, date, cwd), true
}

// customPromptSection returns the custom system prompt if set.
func customPromptSection(customPrompt string) (string, bool) {
	if customPrompt == "" {
		return "", false
	}
	return customPrompt, true
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
func AssembleSystemPrompt(cfg StreamConfig, tools []tool.Tool, cwd string) string {
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

	// AC1: Memory content injected as <system-reminder> block (at start)
	if cfg.MemoryContent != "" {
		sections = append(sections, "<system-reminder>\n"+cfg.MemoryContent+"\n</system-reminder>")
	}

	// Default intro
	if intro, ok := defaultIntroSection(); ok {
		sections = append(sections, intro)
	}

	// AC2: Tool list sync
	if toolList, ok := toolListSection(tools); ok {
		sections = append(sections, toolList)
	}

	// AC3: Git status injection (only inside repo)
	if gitStatus, ok := gitStatusSection(cwd); ok {
		sections = append(sections, gitStatus)
	}

	// AC4: Platform/cwd context
	if platform, ok := platformSection(cwd); ok {
		sections = append(sections, platform)
	}

	// Skills manifest (AC2)
	if len(cfg.Skills) > 0 {
		if manifest := skills.SkillsManifest(cfg.Skills); manifest != "" {
			sections = append(sections, manifest)
		}
	}

	// AC5: Append section (only if not overridden)
	if appendContent, ok := appendSection(cfg.AppendSystemPrompt, cfg.OverrideSystemPrompt); ok {
		sections = append(sections, appendContent)
	}

	return strings.Join(sections, "\n\n")
}
