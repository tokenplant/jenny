// Package tool provides tool implementations.
package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// maxResultSizeChars is the maximum output size before truncation.
	maxResultSizeChars = 20000
	// defaultHeadLimit is the default maximum number of matches.
	defaultHeadLimit = 250
	// defaultTimeout is the default timeout in seconds.
	defaultTimeout = 30
)

// GrepTool searches file contents via ripgrep.
type GrepTool struct{}

// NewGrepTool creates a new GrepTool.
func NewGrepTool() *GrepTool {
	return &GrepTool{}
}

// Name returns the tool name.
func (t *GrepTool) Name() string {
	return "Grep"
}

// Description returns a description of the tool.
func (t *GrepTool) Description() string {
	return "Search file contents using regex patterns. Returns matches with context, sorted by relevance."
}

// InputSchema returns the JSON schema for tool input.
func (t *GrepTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regex pattern to search for",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search (default: cwd)",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "File filter glob pattern",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"description": "Output mode: content, files_with_matches, count",
				"enum":        []any{"content", "files_with_matches", "count"},
			},
			"head_limit": map[string]any{
				"type":        "number",
				"description": "Maximum matches (default: 250, 0 = unlimited)",
			},
			"offset": map[string]any{
				"type":        "number",
				"description": "Skip first N matches for pagination",
			},
			"i": map[string]any{
				"type":        "boolean",
				"description": "Case insensitive search",
			},
			"n": map[string]any{
				"type":        "boolean",
				"description": "Show line numbers",
			},
			"A": map[string]any{
				"type":        "number",
				"description": "Show N lines after match",
			},
			"B": map[string]any{
				"type":        "number",
				"description": "Show N lines before match",
			},
			"C": map[string]any{
				"type":        "number",
				"description": "Show N lines of context around match",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "Multiline mode",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Filter by file type (e.g., go, py, js)",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
		},
		"required": []any{"pattern"},
	}
}

// Execute searches for the pattern using ripgrep.
func (t *GrepTool) Execute(input map[string]any, cwd string) (*ToolResult, error) {
	pattern, ok := input["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required and must be a string")
	}

	// Check if ripgrep is available
	if _, err := exec.LookPath("rg"); err != nil {
		return &ToolResult{
			Content: "ripgrep (rg) not found. Install with: brew install ripgrep",
			IsError: true,
		}, nil
	}

	// Build ripgrep arguments
	args := []string{}

	// Always enable hidden files, set max columns, and explicitly exclude VCS dirs
	// (ripgrep should auto-exclude via .gitignore, but we add explicit exclusion for reliability)
	args = append(args, "--hidden", "--max-columns", "500", "--glob", "!.git", "--glob", "!.svn")

	// Pattern handling: use -e if pattern starts with -
	if strings.HasPrefix(pattern, "-") {
		args = append(args, "-e", pattern)
	} else {
		args = append(args, pattern)
	}

	// Path - use relative path "." when searching cwd to get relative output
	path := "."
	if pathVal, ok := input["path"].(string); ok && pathVal != "" {
		path = pathVal
		if !strings.HasPrefix(path, "/") {
			path = cwd + "/" + path
		}
	}
	args = append(args, path)

	// Glob
	if globVal, ok := input["glob"].(string); ok && globVal != "" {
		args = append(args, "-g", globVal)
	}

	// Output mode
	outputMode := "files_with_matches"
	if modeVal, ok := input["output_mode"].(string); ok && modeVal != "" {
		outputMode = modeVal
	}

	switch outputMode {
	case "content":
		// No extra flag needed
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	}

	// Flags
	if boolVal(input, "i") {
		args = append(args, "-i")
	}
	if boolVal(input, "n") {
		args = append(args, "-n")
	}
	if intVal, ok := input["A"].(float64); ok {
		args = append(args, "-A", fmt.Sprintf("%d", int(intVal)))
	}
	if intVal, ok := input["B"].(float64); ok {
		args = append(args, "-B", fmt.Sprintf("%d", int(intVal)))
	}
	if intVal, ok := input["C"].(float64); ok {
		args = append(args, "-C", fmt.Sprintf("%d", int(intVal)))
	}
	if boolVal(input, "multiline") {
		args = append(args, "-U", "--multiline-dotall")
	}
	if typeVal, ok := input["type"].(string); ok && typeVal != "" {
		args = append(args, "--type", typeVal)
	}

	// Timeout
	timeout := defaultTimeout
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return &ToolResult{
			Content: fmt.Sprintf("Search timed out after %d seconds", timeout),
			IsError: true,
		}, nil
	}

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			exitCode := exitErr.ExitCode()
			// Exit code 1 means no matches (not an error)
			if exitCode == 1 {
				return &ToolResult{
					Content: "No matches found",
					IsError: false,
				}, nil
			}
			// Surface ripgrep errors
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = stdout.String()
			}
			return &ToolResult{
				Content: fmt.Sprintf("ripgrep error (exit code %d): %s", exitCode, errMsg),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error executing ripgrep: %v", err),
			IsError: true,
		}, nil
	}

	output := stdout.String()

	// Apply head_limit and offset to output
	headLimit := defaultHeadLimit
	if limitVal, ok := input["head_limit"].(float64); ok {
		headLimit = int(limitVal)
	} else if limitVal, ok := input["head_limit"].(int); ok {
		headLimit = limitVal
	}
	offset := 0
	if offsetVal, ok := input["offset"].(float64); ok {
		offset = int(offsetVal)
	} else if offsetVal, ok := input["offset"].(int); ok {
		offset = offsetVal
	}

	// Process output by lines (applies to all modes)
	lines := strings.Split(output, "\n")
	if offset > 0 && offset < len(lines) {
		lines = lines[offset:]
	} else if offset >= len(lines) {
		lines = []string{}
	}
	if headLimit > 0 && len(lines) > headLimit {
		lines = lines[:headLimit]
		output = strings.Join(lines, "\n")
	} else {
		output = strings.Join(lines, "\n")
	}

	// Truncate if needed (20K char cap)
	truncated := false
	if len(output) > maxResultSizeChars {
		output = output[:maxResultSizeChars]
		truncated = true
		output += "\n[output truncated]"
	}

	return &ToolResult{
		Content:   output,
		IsError:   false,
		Truncated: truncated,
	}, nil
}

// boolVal safely extracts a boolean from a map.
func boolVal(input map[string]any, key string) bool {
	if val, ok := input[key].(bool); ok {
		return val
	}
	return false
}

// ExecuteWithContext runs the tool with context support. Grep operations use
// context for timeout handling, so this delegates to Execute which creates
// its own timeout context.
func (t *GrepTool) ExecuteWithContext(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	return t.Execute(input, cwd)
}
