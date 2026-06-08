// Package tool provides the tool interface and implementations.
package tool

import (
	"context"
	"fmt"
	"os/exec"
	"sync"

	"github.com/ipy/jenny/internal/git"
)

// ExitWorktreeTool provides the ability to exit a git worktree session.
type ExitWorktreeTool struct {
	mu          sync.Mutex
	inWorktree  bool
	worktreeDir string
}

// NewExitWorktreeTool creates a new ExitWorktreeTool.
func NewExitWorktreeTool() *ExitWorktreeTool {
	return &ExitWorktreeTool{}
}

// Name returns the tool name.
func (t *ExitWorktreeTool) Name() string {
	return "ExitWorktree"
}

// Description returns a description of the tool.
func (t *ExitWorktreeTool) Description() string {
	return "Exit a git worktree session. Use 'keep' action to preserve worktree files, or 'remove' to delete the worktree and its branch."
}

// InputSchema returns the JSON schema for tool input.
func (t *ExitWorktreeTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform: 'keep' to preserve worktree files, or 'remove' to delete the worktree and branch",
				"enum":        []string{"keep", "remove"},
			},
			"discard_changes": map[string]any{
				"type":        "boolean",
				"description": "Required when removing a dirty worktree. Set to true to allow removal even if there are uncommitted changes.",
			},
		},
		"required": []string{"action"},
	}
}

// Execute exits a git worktree session.
func (t *ExitWorktreeTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if in a worktree session
	if !t.inWorktree {
		return &ToolResult{
			Content: "not currently in a worktree session",
			IsError: true,
		}, nil
	}

	// Get action (required)
	action, ok := input["action"].(string)
	if !ok || action == "" {
		return &ToolResult{
			Content: "action is required (keep or remove)",
			IsError: true,
		}, nil
	}

	if action != "keep" && action != "remove" {
		return &ToolResult{
			Content: "action must be 'keep' or 'remove'",
			IsError: true,
		}, nil
	}

	worktreePath := t.worktreeDir

	// For remove action, check if worktree is dirty
	if action == "remove" {
		// Check for uncommitted changes using git status --porcelain
		isDirty, err := isWorktreeDirty(worktreePath)
		if err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("failed to check worktree status: %v", err),
				IsError: true,
			}, nil
		}

		if isDirty {
			discardChanges, ok := input["discard_changes"].(bool)
			if !ok || !discardChanges {
				return &ToolResult{
					Content: "worktree has uncommitted changes. Set discard_changes=true to remove anyway.",
					IsError: true,
				}, nil
			}
		}

		// Remove the worktree
		if err := git.RemoveWorktree(worktreePath); err != nil {
			return &ToolResult{
				Content: fmt.Sprintf("failed to remove worktree: %v", err),
				IsError: true,
			}, nil
		}

		// Clear worktree state
		t.inWorktree = false
		t.worktreeDir = ""

		return &ToolResult{
			Content: fmt.Sprintf("worktree removed: %s", worktreePath),
			IsError: false,
		}, nil
	}

	// Keep action - just exit the session, leave worktree intact
	t.inWorktree = false
	t.worktreeDir = ""

	return &ToolResult{
		Content: fmt.Sprintf("exited worktree session: %s (worktree preserved)", worktreePath),
		IsError: false,
	}, nil
}

// isWorktreeDirty checks if the worktree has uncommitted changes.
func isWorktreeDirty(worktreePath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = worktreePath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git status failed: %w", err)
	}
	// If there's any output, there are changes
	return len(string(out)) > 0, nil
}
