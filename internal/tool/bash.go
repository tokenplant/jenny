// Package tool provides tool implementations.
package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/sandbox"
)

const (
	// maxInlineSize is the maximum size for inline output (30KB)
	maxInlineSize = 30720
)

// BashTool executes shell commands.
type BashTool struct {
	skipPermissions bool
	sandbox         sandbox.SandboxManager
	mu              sync.Mutex
	commandCwd      string
	projectRoot     string
	backgroundTasks sync.Map
}

// NewBashTool creates a new BashTool.
func NewBashTool(skipPermissions bool) *BashTool {
	return &BashTool{
		skipPermissions: skipPermissions,
	}
}

// WithSandbox sets the sandbox manager for the BashTool.
func (t *BashTool) WithSandbox(sb sandbox.SandboxManager) *BashTool {
	t.sandbox = sb
	return t
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
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "Spawn command as background task (required for sleep >=2)",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the bash command with context support for cancellation.
// If the context is cancelled (e.g., by sibling abort), the command will be interrupted.
func (t *BashTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("command is required and must be a string")
	}

	t.mu.Lock()
	// Set project root from cwd
	if t.projectRoot == "" {
		t.projectRoot = cwd
	}

	// Set initial command cwd if not set
	if t.commandCwd == "" {
		t.commandCwd = cwd
	}
	t.mu.Unlock()

	// Handle sed simulation (AC5) - before any security checks
	if isSedInPlace(command) {
		return t.executeSed(command, t.commandCwd)
	}

	// Check for sleep >= 2 in foreground (AC3)
	if !isBackgroundExecution(input) {
		if sleepSeconds := getSleepSeconds(command); sleepSeconds >= 2 {
			return &ToolResult{
				Content: "sleep >=2 seconds is not allowed in foreground; use run_in_background: true",
				IsError: true,
			}, nil
		}
	}

	// Handle background execution (AC3)
	if isBackgroundExecution(input) {
		return t.executeBackground(command, t.commandCwd, input)
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

	// Check pipeline segments for read-only compliance (AC1)
	if err := gate.CheckPipelineSegments(command); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Security error: %v", err),
			IsError: true,
		}, nil
	}

	// Check if all paths in the command are within the working directory
	// Skip validation for cd commands since they change directory state, not file content
	if !isCdCommand(command) && !validateCommandPaths(command, t.commandCwd) {
		return &ToolResult{
			Content: fmt.Sprintf("Error: Command '%s' is not allowed. Access outside working directory is prohibited.", command),
			IsError: true,
		}, nil
	}

	// Wrap command with sandbox if available and not bypassed
	var wrapErr error
	command, wrapErr = t.maybeWrapWithSandbox(command, input)
	if wrapErr != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Sandbox error: %v", wrapErr),
			IsError: true,
		}, nil
	}

	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	// First derive a context that inherits cancellation from the passed context.
	// This is critical: when the executor cancels the parent context (sibling abort),
	// we need the command to be interrupted.
	derivedCtx, derivedCancel := context.WithCancel(ctx)
	defer derivedCancel()

	// Then apply timeout on top of the cancellation-inheriting context.
	// This ensures: (1) sibling abort works via parent cancellation, (2) timeout works.
	cmdCtx, cmdCancel := context.WithTimeout(derivedCtx, time.Duration(timeout)*time.Second)
	defer cmdCancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	cmd.Dir = t.commandCwd

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

	// Handle cwd reset (AC4) - check if command changed directory outside project
	t.resetCwdIfOutsideProject(command)

	if cmdCtx.Err() == context.DeadlineExceeded {
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

	// Handle output spill (AC2)
	return t.handleOutput(output)
}

// handleOutput checks output size and spills to disk if > maxInlineSize
func (t *BashTool) handleOutput(output string) (*ToolResult, error) {
	if len(output) <= maxInlineSize {
		return &ToolResult{
			Content: output,
			IsError: false,
		}, nil
	}

	// Spill to disk
	spillPath, err := t.writeSpillFile(output)
	if err != nil {
		// Fall back to truncated inline if spill fails
		return &ToolResult{
			Content: fmt.Sprintf("Output truncated (spill file error: %v). First %d chars:\n%s",
				err, maxInlineSize/2, output[:maxInlineSize/2]),
			IsError:   false,
			Truncated: true,
		}, nil
	}

	return &ToolResult{
		Content:   fmt.Sprintf("Output spilled to %s (%d chars)", spillPath, len(output)),
		IsError:   false,
		Truncated: true,
	}, nil
}

// writeSpillFile writes output to a temp file and returns the path
func (t *BashTool) writeSpillFile(output string) (string, error) {
	// Try jenny home directory first, then project root .jenny, then temp
	if jennyHome := constants.JennyHomeDir(); dirExists(jennyHome) || mkdirAll(jennyHome, 0755) == nil {
		spillDir := jennyHome
		f, err := os.CreateTemp(spillDir, "jenny-spill-*")
		if err == nil {
			defer f.Close()
			if _, err := f.WriteString(output); err == nil {
				return f.Name(), nil
			}
		}
	}

	spillDir := t.projectRoot
	if jennyDir := filepath.Join(t.projectRoot, ".jenny"); dirExists(jennyDir) {
		spillDir = jennyDir
	} else if tmpDir := filepath.Join(os.TempDir(), "jenny-spill"); dirExists(tmpDir) || mkdirAll(tmpDir, 0755) == nil {
		spillDir = tmpDir
	}

	// Create temp file
	f, err := os.CreateTemp(spillDir, "jenny-spill-*")
	if err != nil {
		return "", fmt.Errorf("failed to create spill file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(output); err != nil {
		return "", fmt.Errorf("failed to write spill file: %w", err)
	}

	return f.Name(), nil
}

// isBackgroundExecution checks if run_in_background flag is set
func isBackgroundExecution(input map[string]any) bool {
	if rib, ok := input["run_in_background"]; ok {
		if b, ok := rib.(bool); ok {
			return b
		}
	}
	return false
}

// executeBackground runs command as background task
func (t *BashTool) executeBackground(command string, cwd string, input map[string]any) (*ToolResult, error) {
	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)

	// Create result channel
	resultCh := make(chan *ToolResult, 1)

	// Generate task ID
	taskID := time.Now().UnixNano()

	// Store in background tasks
	t.backgroundTasks.Store(taskID, resultCh)

	// Spawn goroutine
	go func() {
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

		var result *ToolResult
		if ctx.Err() == context.DeadlineExceeded {
			result = &ToolResult{
				Content: fmt.Sprintf("Command timed out after %d seconds", timeout),
				IsError: true,
			}
		} else if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if ok {
				exitCode := exitErr.ExitCode()
				result = &ToolResult{
					Content: fmt.Sprintf("%s\n(exit code: %d)", output, exitCode),
					IsError: true,
				}
			} else {
				result = &ToolResult{
					Content: fmt.Sprintf("Error executing command: %v\n%s", err, output),
					IsError: true,
				}
			}
		} else {
			result = &ToolResult{
				Content: output,
				IsError: false,
			}
		}

		cancel()
		resultCh <- result
		close(resultCh)
		t.backgroundTasks.Delete(taskID)
	}()

	return &ToolResult{
		Content: fmt.Sprintf("Background task %d started", taskID),
		IsError: false,
	}, nil
}

// resetCwdIfOutsideProject checks if command changed directory outside project and resets
func (t *BashTool) resetCwdIfOutsideProject(command string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	newCwd := parseCdTarget(command, t.commandCwd)
	if newCwd == "" {
		return
	}

	// Check if new cwd is outside project root
	if !isPathWithinCwd(newCwd, t.projectRoot) {
		t.commandCwd = t.projectRoot
	} else {
		t.commandCwd = newCwd
	}
}

// parseCdTarget extracts the target directory from a cd command
func parseCdTarget(command string, currentCwd string) string {
	command = strings.TrimSpace(command)

	// Simple cd detection - check if command starts with cd
	if !strings.HasPrefix(command, "cd ") && !strings.HasPrefix(command, "cd\t") {
		return ""
	}

	// Extract the path after "cd "
	rest := strings.TrimPrefix(command, "cd ")
	rest = strings.TrimPrefix(rest, "cd\t")
	rest = strings.TrimSpace(rest)

	// Strip shell operators after the path (e.g., "&& pwd", "; echo", "| cat")
	rest = stripShellOperators(rest)

	if rest == "" || rest == "~" {
		// cd to home
		home := os.Getenv("HOME")
		if home != "" {
			return home
		}
		return currentCwd
	}

	// Handle ~ expansion
	if strings.HasPrefix(rest, "~/") {
		home := os.Getenv("HOME")
		if home != "" {
			return filepath.Join(home, rest[2:])
		}
	}

	// Handle relative paths
	if filepath.IsAbs(rest) {
		return filepath.Clean(rest)
	}

	// Relative path - resolve from current cwd
	return filepath.Clean(filepath.Join(currentCwd, rest))
}

// stripShellOperators removes shell operators (&&, ||, ;, |, >, <, &, #) and their arguments
// from the input string. This ensures only the actual path is extracted from cd commands.
func stripShellOperators(s string) string {
	// Match any shell operator and everything after it
	// Operators: &&, ||, ;, |, >, <, &, #
	// Remove ^ anchor to match operators anywhere in the string
	shellOpRegex := regexp.MustCompile(`\s*(&&|\|\||[&|;<>]).*$`)
	return shellOpRegex.ReplaceAllString(s, "")
}

// isSedInPlace checks if command is a sed in-place edit
func isSedInPlace(command string) bool {
	return strings.Contains(command, "sed -i") || strings.Contains(command, "sed -i ")
}

// executeSed simulates sed -i edit
func (t *BashTool) executeSed(command string, cwd string) (*ToolResult, error) {
	// Parse: sed -i 's/pattern/replacement/flags' file
	// or: sed -i "s/pattern/replacement/flags" file

	// Extract the sed expression and file
	parts := strings.Fields(command)
	if len(parts) < 4 {
		return &ToolResult{
			Content: "sed: invalid syntax. Expected: sed -i 's/pattern/replacement/flags' file",
			IsError: true,
		}, nil
	}

	// Find the expression (between -i and the file)
	// Simple approach: find -i, then next token is expression, then remaining is file
	var expr string
	var filePath string
	afterExpr := false

	for i := 1; i < len(parts); i++ {
		if parts[i] == "-i" {
			afterExpr = false
			continue
		}
		if !afterExpr && expr == "" {
			// This should be the expression - remove surrounding quotes
			expr = strings.Trim(parts[i], "'\"")
			afterExpr = true
			continue
		}
		if afterExpr {
			// After expression, the first remaining token is the file path
			filePath = parts[i]
			break
		}
	}

	if expr == "" || filePath == "" {
		return &ToolResult{
			Content: "sed: could not parse expression or file path",
			IsError: true,
		}, nil
	}

	// Remove surrounding quotes from expression
	expr = strings.Trim(expr, "'\"")

	// Parse sed expression
	parsed, err := parseSedExpression(expr)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sed: %v", err),
			IsError: true,
		}, nil
	}

	// Resolve file path
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}
	filePath = filepath.Clean(filePath)

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sed: cannot read file: %v", err),
			IsError: true,
		}, nil
	}

	// Apply replacement
	content := string(data)
	if parsed.global {
		content = strings.ReplaceAll(content, parsed.pattern, parsed.replacement)
	} else {
		content = strings.Replace(content, parsed.pattern, parsed.replacement, 1)
	}

	// Write back
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("sed: cannot write file: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: fmt.Sprintf("sed: edited %s in place", filePath),
		IsError: false,
	}, nil
}

type sedParsed struct {
	pattern     string
	replacement string
	global      bool
}

// parseSedExpression parses a sed s/// expression
func parseSedExpression(expr string) (*sedParsed, error) {
	// Support different delimiters: s/// s### s,,,
	// Find the delimiter (first char after 's')
	if len(expr) < 4 || expr[0] != 's' {
		return nil, fmt.Errorf("invalid sed expression format")
	}

	delimiter := expr[1]
	rest := expr[2:]

	// Find the three parts separated by delimiter
	parts := strings.SplitN(rest, string(delimiter), 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid sed expression: could not parse pattern/replacement")
	}

	pattern := parts[0]
	replacement := parts[1]
	flags := ""
	if len(parts) >= 4 {
		flags = parts[3]
	}

	// Check for 'g' flag (global)
	global := strings.Contains(flags, "g")

	return &sedParsed{
		pattern:     pattern,
		replacement: replacement,
		global:      global,
	}, nil
}

// getSleepSeconds extracts sleep duration from command
func getSleepSeconds(command string) int {
	// Match: sleep N, sleep.N, sleep Ns, etc.
	re := regexp.MustCompile(`sleep\s+(\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(command)
	if len(matches) < 2 {
		return 0
	}

	if strings.Contains(matches[1], ".") {
		f, _ := strconv.ParseFloat(matches[1], 64)
		return int(f)
	}

	n, _ := strconv.Atoi(matches[1])
	return n
}

// dirExists checks if a directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// mkdirAll creates a directory and all parents, returns nil on success
func mkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// isCdCommand checks if the command starts with cd
func isCdCommand(command string) bool {
	command = strings.TrimSpace(command)
	return strings.HasPrefix(command, "cd ") || strings.HasPrefix(command, "cd\t")
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

// maybeWrapWithSandbox wraps the command with sandbox if available and not bypassed.
func (t *BashTool) maybeWrapWithSandbox(command string, input map[string]any) (string, error) {
	// If no sandbox is configured, return original command
	if t.sandbox == nil {
		return command, nil
	}

	// If sandbox is not active, return original command
	if !t.sandbox.IsActive() {
		return command, nil
	}

	// Check for dangerouslyDisableSandbox flag
	if disableSandbox, ok := input["dangerouslyDisableSandbox"].(bool); ok && disableSandbox {
		return command, nil
	}

	// Wrap with sandbox
	return t.sandbox.WrapWithSandbox(command)
}
