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
	return `You are an AI assistant that can use tools to help answer user questions. When you use tools, carefully review the results and incorporate them into your response. You should always aim to provide accurate, helpful, and concise information. If a tool call is necessary to fulfill a user request, you must use the most appropriate tool available. After receiving tool results, you should synthesize the information and present it clearly to the user, ensuring that all their questions are addressed thoroughly. Your goal is to be a reliable partner in problem-solving and information retrieval, maintaining a professional and efficient tone throughout the interaction.`, true
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

// platformSection returns a section with platform and cwd context.
func platformSection(cwd string) (string, bool) {
	if cwd == "" {
		cwd, _ = os.Getwd()
		if cwd == "" {
			cwd = "/"
		}
	}
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	return fmt.Sprintf("Platform: %s\nCwd: %s", platform, cwd), true
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
