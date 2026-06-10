// Package tool provides tool implementations.
package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
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
	taskManager     *TaskManager
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

// WithTaskManager sets the task manager for background task tracking.
func (t *BashTool) WithTaskManager(tm *TaskManager) *BashTool {
	t.taskManager = tm
	return t
}

// GetTaskManager returns the task manager for sharing with other tools.
func (t *BashTool) GetTaskManager() *TaskManager {
	return t.taskManager
}

// Name returns the tool name.
func (t *BashTool) Name() string {
	return "Bash"
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

	// Track start time for auto-background hint (AC4)
	startTime := time.Now()

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

	// AC4: Auto-background hint for long-running commands (>120s)
	duration := time.Since(startTime)
	if duration > 120*time.Second {
		output += "\n(Tip: long-running commands work better with run_in_background: true)"
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

	// Create a background context (NOT for timeout - we handle timeout manually for AC5)
	ctx := context.Background()

	// Create result channel (buffered to prevent deadlock when outer select is stuck)
	resultCh := make(chan *ToolResult, 1)

	// Generate string task ID
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())

	// Initialize task manager if nil
	if t.taskManager == nil {
		t.taskManager = NewTaskManager()
	}

	// Set project root on task manager for project-relative paths
	if t.projectRoot != "" {
		t.taskManager.WithProjectRoot(t.projectRoot)
	}

	// Get output file path
	outputFile := ""
	if tm := t.taskManager; tm != nil {
		path, err := tm.TaskOutputPath(taskID)
		if err == nil {
			outputFile = path
			// Store task info (Process will be set after cmd.Start())
			tm.Store(taskID, &TaskInfo{
				TaskID:     taskID,
				State:      TaskStateRunning,
				OutputFile: outputFile,
				StartTime:  time.Now(),
				Command:    command,
			})
		}
	}

	// Store in background tasks (using string key for compatibility)
	t.backgroundTasks.Store(taskID, resultCh)

	// Track if command completed (for synchronization)
	var cmdDone int32 = 0

	// Channel to signal command completion
	done := make(chan struct{})

	// Start time for duration tracking
	startTime := time.Now()

	// Track output for progress events (shared between goroutines)
	var outputMu sync.Mutex
	var output strings.Builder

	// Spawn goroutine
	go func() {
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir = cwd

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		// Inner goroutine runs the command
		go func() {
			err := cmd.Start()
			if err != nil {
				// Command failed to start
				outputMu.Lock()
				output.WriteString(fmt.Sprintf("failed to start command: %v", err))
				outputMu.Unlock()
				atomic.StoreInt32(&cmdDone, 1)
				close(done)
				return
			}

			// Store process reference for TaskStop (AC5)
			if t.taskManager != nil && cmd.Process != nil {
				t.taskManager.UpdateProcess(taskID, cmd.Process)
			}

			// Wait for command completion
			err = cmd.Wait()

			// Capture output
			outputMu.Lock()
			output.WriteString(stdout.String())
			if stderr.Len() > 0 {
				if output.Len() > 0 {
					output.WriteString("\n")
				}
				output.WriteString("stderr: " + stderr.String())
			}
			outputMu.Unlock()

			// Determine exit code
			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = -1
				}
			}

			// Calculate duration
			duration := time.Since(startTime).Seconds()

			// Get output snapshot for file writing
			outputMu.Lock()
			outputSnapshot := output.String()
			outputMu.Unlock()

			// Write final result to output file (AC1)
			if t.taskManager != nil {
				_ = t.taskManager.WriteTaskResult(taskID, outputSnapshot, exitCode, duration)

				// Cancel any pending SIGKILL timer since task completed gracefully
				t.taskManager.CancelKillTimer(taskID)

				// Update task state
				t.taskManager.UpdateState(taskID, TaskStateCompleted)

				// Queue completion notification (AC3)
				t.taskManager.EnqueueCompletion(TaskCompletion{
					TaskID:          taskID,
					DurationSeconds: duration,
					ExitCode:        exitCode,
					Output:          outputSnapshot,
				})
			}

			// Store result for parent
			cmdOutput := outputSnapshot
			var result *ToolResult
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				resultExitCode := cmd.ProcessState.ExitCode()
				result = &ToolResult{
					Content: fmt.Sprintf("%s\n(exit code: %d)", cmdOutput, resultExitCode),
					IsError: resultExitCode != 0,
				}
			} else if cmd.ProcessState != nil {
				// Process was killed by a signal (e.g., SIGTERM from TaskStop or timeout)
				// AC3: Extract signal information for meaningful exit code
				if waitStatus, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
					signal := waitStatus.Signal()
					signalExitCode := 128 + int(signal)
					result = &ToolResult{
						Content: fmt.Sprintf("%s\n(exit code: %d, signal: %s)", cmdOutput, signalExitCode, signal),
						IsError: true,
					}
				} else {
					result = &ToolResult{
						Content: cmdOutput,
						IsError: false,
					}
				}
			} else {
				result = &ToolResult{
					Content: cmdOutput,
					IsError: false,
				}
			}

			// AC4: Race-free channel ordering - send result BEFORE signaling completion.
			// This establishes Go memory model happens-before: resultCh send happens-before
			// atomic store, which happens-before outer loop reads cmdDone, which happens-before
			// outer loop closes resultCh.
			resultCh <- result
			atomic.StoreInt32(&cmdDone, 1)
			close(done)
		}()

		// AC2: Progress timer created BEFORE cmd.Start() so it fires concurrently with command
		progressTimer := time.NewTimer(2 * time.Second)
		flushTicker := time.NewTicker(5 * time.Second)
		defer progressTimer.Stop()
		defer flushTicker.Stop()

		// AC5: Manual timeout handling with SIGTERM then SIGKILL (not context timeout)
		timeoutChan := time.After(time.Duration(timeout) * time.Second)
		var killSent bool
		var killMu sync.Mutex

		// AC1: Loop to allow periodic flushes while waiting for command completion
	outer:
		for {
			select {
			case <-progressTimer.C:
				// Task ran for more than 2 seconds - emit progress
				// Fix ac2-progress-always-emits: emit progress only if still running
				outputMu.Lock()
				outputSnapshot := output.String()
				outputMu.Unlock()
				if t.taskManager != nil {
					EmitTaskProgress(taskID, 2.0, outputSnapshot)
				}
				// Continue waiting - don't re-arm timer, progress is one-shot
				continue
			case <-flushTicker.C:
				// Periodic flush of partial output (AC1)
				outputMu.Lock()
				outputSnapshot := output.String()
				outputMu.Unlock()
				if t.taskManager != nil {
					duration := time.Since(startTime).Seconds()
					_ = t.taskManager.FlushPartialOutput(taskID, outputSnapshot, duration)
				}
			case <-done:
				// Command completed before either timer fired - no progress event
				break outer
			case <-timeoutChan:
				// Timeout - send SIGTERM first (AC5)
				killMu.Lock()
				if !killSent {
					killSent = true
					if cmd.Process != nil {
						_ = cmd.Process.Signal(syscall.SIGTERM)
					}
					// Schedule SIGKILL after 5s if process doesn't exit
					go func() {
						time.Sleep(5 * time.Second)
						killMu.Lock()
						defer killMu.Unlock()
						if cmd.Process != nil {
							_ = cmd.Process.Signal(syscall.SIGKILL)
						}
					}()
				}
				killMu.Unlock()
			}
		}

		// Wait for inner goroutine to finish sending result before closing
		for atomic.LoadInt32(&cmdDone) == 0 {
			time.Sleep(10 * time.Millisecond)
		}

		close(resultCh)
		t.backgroundTasks.Delete(taskID)

		// Clean up task from manager
		if t.taskManager != nil {
			t.taskManager.Delete(taskID)
		}
	}()

	return &ToolResult{
		Content:    fmt.Sprintf("Background task %s started", taskID),
		OutputFile: outputFile,
		IsError:    false,
	}, nil
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
