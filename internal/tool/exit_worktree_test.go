package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExitWorktreeTool_Name(t *testing.T) {
	tool := NewExitWorktreeTool()
	if got := tool.Name(); got != "ExitWorktree" {
		t.Errorf("Name() = %v, want %v", got, "ExitWorktree")
	}
}

func TestExitWorktreeTool_InputSchema(t *testing.T) {
	tool := NewExitWorktreeTool()
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Errorf("InputSchema() type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() properties not a map")
	}

	// Check required fields
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("InputSchema() required not a slice")
	}

	hasAction := false
	for _, r := range required {
		if r == "action" {
			hasAction = true
		}
	}
	if !hasAction {
		t.Error("InputSchema() missing required field: action")
	}

	// Check action enum
	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() action property not found")
	}
	enum, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("InputSchema() action enum not found")
	}
	if len(enum) != 2 || enum[0] != "keep" || enum[1] != "remove" {
		t.Errorf("InputSchema() action enum = %v, want [keep, remove]", enum)
	}
}

func TestExitWorktreeTool_Execute_NotInWorktree(t *testing.T) {
	tool := NewExitWorktreeTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"action": "keep"}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() IsError = false, want true")
	}
	if result.Content != "not currently in a worktree session" {
		t.Errorf("Execute() Content = %q, want %q", result.Content, "not currently in a worktree session")
	}
}

func TestExitWorktreeTool_Execute_MissingAction(t *testing.T) {
	tool := NewExitWorktreeTool()
	ctx := context.Background()

	// Set up shared session
	session := &WorktreeSession{}
	session.SetWorktree("/some/path")
	tool.WithWorktreeSession(session)

	result, err := tool.Execute(ctx, map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() IsError = false, want true")
	}
	if result.Content != "action is required (keep or remove)" {
		t.Errorf("Execute() Content = %q, want %q", result.Content, "action is required (keep or remove)")
	}
}

func TestExitWorktreeTool_Execute_InvalidAction(t *testing.T) {
	tool := NewExitWorktreeTool()
	ctx := context.Background()

	// Set up shared session
	session := &WorktreeSession{}
	session.SetWorktree("/some/path")
	tool.WithWorktreeSession(session)

	result, err := tool.Execute(ctx, map[string]any{"action": "delete"}, "/tmp")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() IsError = false, want true")
	}
	if result.Content != "action must be 'keep' or 'remove'" {
		t.Errorf("Execute() Content = %q, want %q", result.Content, "action must be 'keep' or 'remove'")
	}
}

func TestExitWorktreeTool_Execute_KeepAction(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	// Create a worktree
	worktreePath := CreateWorktreeForTest(t, tmpDir, "keep-test-branch")

	tool := NewExitWorktreeTool()
	ctx := context.Background()

	// Set up shared session
	session := &WorktreeSession{}
	session.SetWorktree(worktreePath)
	tool.WithWorktreeSession(session)

	result, err := tool.Execute(ctx, map[string]any{"action": "keep"}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError = true, want false, content = %s", result.Content)
	}

	// Worktree should still exist (keep action)
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("worktree should still exist after keep action")
	}
}

func TestExitWorktreeTool_Execute_CleanRemove(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	// Create a worktree
	worktreePath := CreateWorktreeForTest(t, tmpDir, "remove-test-branch")

	tool := NewExitWorktreeTool()
	ctx := context.Background()

	// Set up shared session
	session := &WorktreeSession{}
	session.SetWorktree(worktreePath)
	tool.WithWorktreeSession(session)

	result, err := tool.Execute(ctx, map[string]any{"action": "remove"}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError = true, want false, content = %s", result.Content)
	}

	// Worktree should be removed
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree should be removed after remove action")
	}
}

func TestExitWorktreeTool_Execute_DirtyRemoveWithoutDiscard(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	// Create a worktree
	worktreePath := CreateWorktreeForTest(t, tmpDir, "dirty-test-branch")

	// Make the worktree dirty by adding a file
	dirtyFile := filepath.Join(worktreePath, "dirty.txt")
	err := os.WriteFile(dirtyFile, []byte("uncommitted changes"), 0644)
	if err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	tool := NewExitWorktreeTool()
	ctx := context.Background()

	// Set up shared session
	session := &WorktreeSession{}
	session.SetWorktree(worktreePath)
	tool.WithWorktreeSession(session)

	result, err := tool.Execute(ctx, map[string]any{"action": "remove"}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() IsError = false, want true")
	}
	if result.Content != "worktree has uncommitted changes. Set discard_changes=true to remove anyway." {
		t.Errorf("Execute() Content = %q, want %q", result.Content, "worktree has uncommitted changes. Set discard_changes=true to remove anyway.")
	}

	// Clean up the dirty file
	os.Remove(dirtyFile)
}

func TestExitWorktreeTool_Execute_DirtyRemoveWithDiscard(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	// Create a worktree
	worktreePath := CreateWorktreeForTest(t, tmpDir, "discard-test-branch")

	// Make the worktree dirty by adding a file
	dirtyFile := filepath.Join(worktreePath, "dirty.txt")
	err := os.WriteFile(dirtyFile, []byte("uncommitted changes"), 0644)
	if err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	tool := NewExitWorktreeTool()
	ctx := context.Background()

	// Set up shared session
	session := &WorktreeSession{}
	session.SetWorktree(worktreePath)
	tool.WithWorktreeSession(session)

	result, err := tool.Execute(ctx, map[string]any{"action": "remove", "discard_changes": true}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError = true, want false, content = %s", result.Content)
	}

	// Worktree should be removed
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree should be removed after remove action with discard_changes=true")
	}
}

func TestIsWorktreeDirty(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	worktreePath := CreateWorktreeForTest(t, tmpDir, "dirty-check-branch")

	// Clean worktree should not be dirty
	dirty, err := isWorktreeDirty(worktreePath)
	if err != nil {
		t.Fatalf("isWorktreeDirty() error = %v", err)
	}
	if dirty {
		t.Error("clean worktree should not be dirty")
	}

	// Add uncommitted file
	dirtyFile := filepath.Join(worktreePath, "dirty.txt")
	err = os.WriteFile(dirtyFile, []byte("changes"), 0644)
	if err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	dirty, err = isWorktreeDirty(worktreePath)
	if err != nil {
		t.Fatalf("isWorktreeDirty() error = %v", err)
	}
	if !dirty {
		t.Error("worktree with uncommitted file should be dirty")
	}
}
