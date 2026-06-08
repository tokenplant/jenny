// Package tool provides the tool interface and implementations.
package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// TaskState represents the state of a background task.
type TaskState string

const (
	TaskStateRunning   TaskState = "running"
	TaskStateCompleted TaskState = "completed"
	TaskStateStopped   TaskState = "stopped"
)

// TaskInfo holds information about a background task.
type TaskInfo struct {
	TaskID     string
	State      TaskState
	OutputFile string
	StartTime  time.Time
	Command    string
	Process    *os.Process
	KillTimer  *time.Timer
}

// TaskCompletion holds a completion notification for a background task.
type TaskCompletion struct {
	TaskID          string
	DurationSeconds float64
	ExitCode        int
	Output          string
}

// TaskManager manages background tasks with thread-safe tracking.
type TaskManager struct {
	mu              sync.Mutex
	tasks           map[string]*TaskInfo
	completionQueue []TaskCompletion
	tasksDir        string
	projectRoot     string
}

// NewTaskManager creates a new TaskManager.
func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks: make(map[string]*TaskInfo),
	}
}

// WithProjectRoot sets the project root directory for task output files.
func (tm *TaskManager) WithProjectRoot(root string) *TaskManager {
	tm.projectRoot = root
	return tm
}

// TasksDir returns the directory for task output files.
// Creates the directory if it doesn't exist.
// Uses projectRoot/.jenny/tasks if projectRoot is set, otherwise falls back to ~/.jenny/tasks.
func (tm *TaskManager) TasksDir() (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.tasksDir != "" {
		return tm.tasksDir, nil
	}

	var tasksDir string
	if tm.projectRoot != "" {
		// Use project-relative .jenny/tasks directory
		tasksDir = filepath.Join(tm.projectRoot, ".jenny", "tasks")
	} else {
		// Fall back to ~/.jenny/tasks
		homeDir, _ := os.UserHomeDir()
		if homeDir == "" {
			homeDir = "."
		}
		tasksDir = filepath.Join(homeDir, ".jenny", "tasks")
	}

	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return "", fmt.Errorf("creating tasks directory: %w", err)
	}

	tm.tasksDir = tasksDir
	return tasksDir, nil
}

// TaskOutputPath returns the output file path for a task.
func (tm *TaskManager) TaskOutputPath(taskID string) (string, error) {
	dir, err := tm.TasksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, taskID+".output"), nil
}

// Store saves a task info to the manager.
func (tm *TaskManager) Store(taskID string, info *TaskInfo) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tasks[taskID] = info
}

// Load retrieves a task info from the manager.
func (tm *TaskManager) Load(taskID string) (*TaskInfo, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	info, ok := tm.tasks[taskID]
	return info, ok
}

// Delete removes a task from the manager.
func (tm *TaskManager) Delete(taskID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.tasks, taskID)
}

// UpdateState updates the state of a task.
func (tm *TaskManager) UpdateState(taskID string, state TaskState) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if info, ok := tm.tasks[taskID]; ok {
		info.State = state
	}
}

// UpdateProcess updates the process reference for a task.
func (tm *TaskManager) UpdateProcess(taskID string, process *os.Process) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if info, ok := tm.tasks[taskID]; ok {
		info.Process = process
	}
}

// CancelKillTimer cancels any pending SIGKILL timer for a task.
// Called when a task exits gracefully before the 5s escalation timeout.
func (tm *TaskManager) CancelKillTimer(taskID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if info, ok := tm.tasks[taskID]; ok && info.KillTimer != nil {
		info.KillTimer.Stop()
		info.KillTimer = nil
	}
}

// Stop terminates a running task using SIGTERM then SIGKILL after 5s.
// AC1+AC2: SIGKILL timer is stored and cancellable; state remains Running
// until timer fires so the escalation path is not blocked.
func (tm *TaskManager) Stop(taskID string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	info, ok := tm.tasks[taskID]
	if !ok {
		return fmt.Errorf("task not found or already completed")
	}

	if info.State != TaskStateRunning {
		return fmt.Errorf("task not found or already completed")
	}

	// Use SIGTERM for graceful shutdown first
	if info.Process != nil {
		_ = info.Process.Signal(syscall.SIGTERM)
	}

	// Schedule SIGKILL after 5 seconds if process doesn't exit gracefully.
	// State remains TaskStateRunning so the timer closure can verify the
	// process is still alive before sending SIGKILL.
	info.KillTimer = time.AfterFunc(5*time.Second, func() {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		if info, ok := tm.tasks[taskID]; ok && info.State == TaskStateRunning {
			if info.Process != nil {
				_ = info.Process.Signal(syscall.SIGKILL)
			}
			info.State = TaskStateStopped
		}
	})

	return nil
}

// GetRunningTasks returns all running task IDs.
func (tm *TaskManager) GetRunningTasks() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var running []string
	for id, info := range tm.tasks {
		if info.State == TaskStateRunning {
			running = append(running, id)
		}
	}
	return running
}

// EnqueueCompletion adds a completion notification to the queue.
func (tm *TaskManager) EnqueueCompletion(completion TaskCompletion) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.completionQueue = append(tm.completionQueue, completion)
}

// DrainCompletions returns and clears all pending completion notifications.
func (tm *TaskManager) DrainCompletions() []TaskCompletion {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	completions := tm.completionQueue
	tm.completionQueue = nil
	return completions
}

// WriteTaskResult writes a task result to the output file as JSONL.
func (tm *TaskManager) WriteTaskResult(taskID string, output string, exitCode int, durationSeconds float64) error {
	path, err := tm.TaskOutputPath(taskID)
	if err != nil {
		return err
	}

	entry := TaskResultEntry{
		Type:            "task_result",
		TaskID:          taskID,
		Output:          output,
		ExitCode:        exitCode,
		DurationSeconds: durationSeconds,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling task result: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing task result: %w", err)
	}

	return nil
}

// FlushPartialOutput writes accumulated partial output to the task's output file.
// This is called periodically during task execution to ensure partial output is
// available if the task is interrupted.
func (tm *TaskManager) FlushPartialOutput(taskID string, output string, durationSeconds float64) error {
	path, err := tm.TaskOutputPath(taskID)
	if err != nil {
		return err
	}

	entry := TaskResultEntry{
		Type:            "task_result",
		TaskID:          taskID,
		Output:          output,
		ExitCode:        -1, // -1 indicates task still running
		DurationSeconds: durationSeconds,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling partial result: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing partial result: %w", err)
	}

	return nil
}

// TaskResultEntry represents a JSONL entry in the task output file.
type TaskResultEntry struct {
	Type            string  `json:"type"`
	TaskID          string  `json:"task_id"`
	Output          string  `json:"output"`
	ExitCode        int     `json:"exit_code"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// EmitTaskProgress emits a progress event for a running task.
// Outputs to stdout as stream-json format.
func EmitTaskProgress(taskID string, durationSeconds float64, outputPreview string) {
	// Truncate preview to 200 chars
	if len(outputPreview) > 200 {
		outputPreview = outputPreview[:200]
	}

	msg := StreamMessage{
		Type:            "task_progress",
		TaskID:          taskID,
		DurationSeconds: durationSeconds,
		OutputPreview:   outputPreview,
	}

	data, _ := json.Marshal(msg)
	fmt.Fprintln(os.Stdout, string(data))
}

// StreamMessage represents a message in the stream-json output.
type StreamMessage struct {
	Type            string  `json:"type"`
	TaskID          string  `json:"task_id,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	OutputPreview   string  `json:"output_preview,omitempty"`
}
