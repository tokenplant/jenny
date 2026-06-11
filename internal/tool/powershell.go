package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PowerShellTool executes PowerShell commands.
type PowerShellTool struct {
	skipPermissions bool
	mu              sync.Mutex
	commandCwd      string
	projectRoot     string
	backgroundTasks sync.Map
	taskManager     *TaskManager
}

// NewPowerShellTool creates a new PowerShellTool.
func NewPowerShellTool(skipPermissions bool) *PowerShellTool {
	return &PowerShellTool{
		skipPermissions: skipPermissions,
	}
}

// WithTaskManager sets the task manager for background task tracking.
func (t *PowerShellTool) WithTaskManager(tm *TaskManager) *PowerShellTool {
	t.taskManager = tm
	return t
}

// Name returns the tool name.
func (t *PowerShellTool) Name() string {
	return "PowerShell"
}

// Description returns a description of the tool.
func (t *PowerShellTool) Description() string {
	return "Execute PowerShell commands on Windows. Use this to run PowerShell commands like Get-ChildItem, Get-Content, etc."
}

// InputSchema returns the JSON schema for tool input.
func (t *PowerShellTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The PowerShell command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Timeout in seconds (default: 30)",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "Spawn command as background task",
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the PowerShell command.
func (t *PowerShellTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	command, ok := input["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("command is required and must be a string")
	}

	t.mu.Lock()
	if t.projectRoot == "" {
		t.projectRoot = cwd
	}
	if t.commandCwd == "" {
		t.commandCwd = cwd
	}
	t.mu.Unlock()

	if isBackgroundExecution(input) {
		return t.executeBackground(command, t.commandCwd, input)
	}

	// Windows Command Gate
	if runtime.GOOS == "windows" {
		gate := NewWindowsCommandGate(t.skipPermissions)
		if err := gate.CheckCommand(command); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("Security error: %v", err),
				IsError: true,
			}, nil
		}
	}

	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	derivedCtx, derivedCancel := context.WithCancel(ctx)
	defer derivedCancel()

	cmdCtx, cmdCancel := context.WithTimeout(derivedCtx, time.Duration(timeout)*time.Second)
	defer cmdCancel()

	// Prepend UTF-8 encoding command
	fullCommand := "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); " + command
	
	cmd := exec.CommandContext(cmdCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", fullCommand)
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

	if cmdCtx.Err() == context.DeadlineExceeded {
		return &ToolResult{
			Content: fmt.Sprintf("Command timed out after %d seconds", timeout),
			IsError: true,
		}, nil
	}

	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok {
			return &ToolResult{
				Content: fmt.Sprintf("%s\n(exit code: %d)", output, exitErr.ExitCode()),
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

func (t *PowerShellTool) executeBackground(command string, cwd string, input map[string]any) (*ToolResult, error) {
	timeout := 30
	if timeoutVal, ok := input["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	ctx := context.Background()
	resultCh := make(chan *ToolResult, 1)
	taskID := fmt.Sprintf("task_ps_%d", time.Now().UnixNano())

	if t.taskManager == nil {
		t.taskManager = NewTaskManager()
	}
	if t.projectRoot != "" {
		t.taskManager.WithProjectRoot(t.projectRoot)
	}

	outputFile, _ := t.taskManager.TaskOutputPath(taskID)
	t.taskManager.Store(taskID, &TaskInfo{
		TaskID:     taskID,
		State:      TaskStateRunning,
		OutputFile: outputFile,
		StartTime:  time.Now(),
		Command:    command,
	})

	t.backgroundTasks.Store(taskID, resultCh)
	var cmdDone int32 = 0
	done := make(chan struct{})
	startTime := time.Now()
	var outputMu sync.Mutex
	var output strings.Builder

	go func() {
		fullCommand := "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); " + command
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", fullCommand)
		cmd.Dir = cwd

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		go func() {
			err := cmd.Start()
			if err != nil {
				outputMu.Lock()
				output.WriteString(fmt.Sprintf("failed to start command: %v", err))
				outputMu.Unlock()
				atomic.StoreInt32(&cmdDone, 1)
				close(done)
				return
			}

			if t.taskManager != nil && cmd.Process != nil {
				t.taskManager.UpdateProcess(taskID, cmd.Process)
			}

			err = cmd.Wait()
			outputMu.Lock()
			output.WriteString(stdout.String())
			if stderr.Len() > 0 {
				if output.Len() > 0 {
					output.WriteString("\n")
				}
				output.WriteString("stderr: " + stderr.String())
			}
			outputMu.Unlock()

			exitCode := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = -1
				}
			}

			duration := time.Since(startTime).Seconds()
			outputMu.Lock()
			outputSnapshot := output.String()
			outputMu.Unlock()

			if t.taskManager != nil {
				_ = t.taskManager.WriteTaskResult(taskID, outputSnapshot, exitCode, duration)
				t.taskManager.CancelKillTimer(taskID)
				t.taskManager.UpdateState(taskID, TaskStateCompleted)
				t.taskManager.EnqueueCompletion(TaskCompletion{
					TaskID:          taskID,
					DurationSeconds: duration,
					ExitCode:        exitCode,
					Output:          outputSnapshot,
				})
			}

			result := &ToolResult{
				Content: outputSnapshot,
				IsError: exitCode != 0,
			}
			resultCh <- result
			atomic.StoreInt32(&cmdDone, 1)
			close(done)
		}()

		timeoutChan := time.After(time.Duration(timeout) * time.Second)
	outer:
		for {
			select {
			case <-done:
				break outer
			case <-timeoutChan:
				if cmd.Process != nil {
					_ = signalProcess(cmd.Process, true)
				}
				break outer
			}
		}

		for atomic.LoadInt32(&cmdDone) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		close(resultCh)
		t.backgroundTasks.Delete(taskID)
	}()

	return &ToolResult{
		Content:    fmt.Sprintf("Background task %s started", taskID),
		OutputFile: outputFile,
		IsError:    false,
	}, nil
}
