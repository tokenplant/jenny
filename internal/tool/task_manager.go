// Package tool provides the tool interface and implementations.
package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	Cancel     func()
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
}

// NewTaskManager creates a new TaskManager.
func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks: make(map[string]*TaskInfo),
	}
}

// TasksDir returns the directory for task output files.
// Creates the directory if it doesn't exist.
func (tm *TaskManager) TasksDir() (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.tasksDir != "" {
		return tm.tasksDir, nil
	}

	// Use .jenny/tasks in home directory
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = "."
	}
	jennyHome := filepath.Join(homeDir, ".jenny")
	tasksDir := filepath.Join(jennyHome, "tasks")

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

// Stop terminates a running task.
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

	// Call cancel function if available
	if info.Cancel != nil {
		info.Cancel()
	}

	info.State = TaskStateStopped
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
