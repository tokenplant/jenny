// Package tool provides tool implementations.
package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BashTool executes shell commands.
type BashTool struct {
	skipPermissions bool
}

// NewBashTool creates a new BashTool.
func NewBashTool(skipPermissions bool) *BashTool {
	return &BashTool{skipPermissions: skipPermissions}
}

// Name returns the tool name.
func (t *BashTool) Name() string {
	return "bash"
}

// Description returns a description of the tool.
func (t *BashTool) Description() string {
	return "Execute shell commands. Use this to run bash commands like ls, cat, pwd, etc."
}

// InputSchema returns the JSON schema for tool input.
func (t *BashTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the bash command.
func (t *BashTool) Execute(input map[string]any, cwd string) (*ToolResult, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("command is required and must be a string")
	}

	// Create command gate for security validation
	gate := NewCommandGate(t.skipPermissions)

	// Check command against blocked patterns
	if err := gate.CheckCommand(command); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Security error: %v", err),
			IsError: true,
		}, nil
	}

	// Check pipeline segments for read-only compliance
	if err := gate.CheckPipelineSegments(command); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Security error: %v", err),
			IsError: true,
		}, nil
	}

	// Check if all paths in the command are within the working directory
	if !validateCommandPaths(command, cwd) {
		return &ToolResult{
			Content: fmt.Sprintf("Error: Command '%s' is not allowed. Access outside working directory is prohibited.", command),
			IsError: true,
		}, nil
	}

	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "stderr: " + stderr.String()
	}

	if ctx.Err() == context.DeadlineExceeded {
		return &ToolResult{
			Content: fmt.Sprintf("Command timed out after %d seconds", timeout),
			IsError: true,
		}, nil
	}

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			exitCode := exitErr.ExitCode()
			return &ToolResult{
				Content: fmt.Sprintf("%s\n(exit code: %d)", output, exitCode),
				IsError: true,
			}, nil
		}
		return &ToolResult{
			Content: fmt.Sprintf("Error executing command: %v\n%s", err, output),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: output,
		IsError: false,
	}, nil
}

// isPathWithinCwd checks if a path is within the working directory.
// Handles absolute paths, relative paths (../, ./), and symlinks.
func isPathWithinCwd(path string, cwd string) bool {
	// Make path absolute
	if !filepath.IsAbs(path) {
		if strings.HasPrefix(path, "./") {
			path = filepath.Join(cwd, path[2:])
		} else if strings.HasPrefix(path, "../") {
			path = filepath.Join(cwd, path)
		} else {
			// Plain filename - relative to cwd
			path = filepath.Join(cwd, path)
		}
	}

	// Get absolute path (this resolves symlinks for the directory part)
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	absPath = filepath.Clean(absPath)

	// Get absolute path for cwd
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		cwdAbs = cwd
	}
	cwdAbs = filepath.Clean(cwdAbs)

	// Check if path is the cwd itself or a subdirectory of cwd
	if absPath == cwdAbs {
		return true
	}
	// Add trailing separator for prefix check to ensure we match directory boundary
	return strings.HasPrefix(absPath, cwdAbs+string(filepath.Separator))
}

// validateCommandPaths checks if all paths in the command are within cwd.
// Returns false if any path is outside cwd.
func validateCommandPaths(command string, cwd string) bool {
	// Split command into tokens to find potential paths
	tokens := strings.Fields(command)

	// Skip validation for command lookup commands that don't actually access files
	// These commands only query where other commands are located
	if len(tokens) > 0 {
		cmd := tokens[0]
		if cmd == "which" || cmd == "type" || cmd == "command" || cmd == "hash" || cmd == "whence" {
			return true
		}
	}

	for _, token := range tokens {
		// Skip common shell operators and flags
		if strings.HasPrefix(token, "-") || token == "|" || token == ">" || token == "<" {
			continue
		}

		// Check if token is a path: absolute, ./, ../, or contains path separator
		if filepath.IsAbs(token) || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") || strings.Contains(token, "/") {
			if !isPathWithinCwd(token, cwd) {
				return false
			}
		}
	}
	return true
}

// isReadOnlyCommand checks if a command is read-only (safe to execute without restrictions).
// This is a simple prefix-based check used for backwards compatibility.
func isReadOnlyCommand(command string) bool {
	// Check for redirection operators - these make the command non-read-only
	if strings.ContainsAny(command, ">|") {
		return false
	}

	readOnlyCommands := []string{
		"ls", "pwd", "whoami", "cat", "head", "tail", "grep", "find", "wc",
		"echo", "date", "which", "type", "file", "stat", "diff", "sleep",
	}
	cmd := strings.TrimSpace(command)
	for _, prefix := range readOnlyCommands {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}
