package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnterWorktreeTool_Name(t *testing.T) {
	tool := NewEnterWorktreeTool()
	if got := tool.Name(); got != "EnterWorktree" {
		t.Errorf("Name() = %v, want %v", got, "EnterWorktree")
	}
}

func TestEnterWorktreeTool_InputSchema(t *testing.T) {
	tool := NewEnterWorktreeTool()
	schema := tool.InputSchema()

	if schema["type"] != "object" {
		t.Errorf("InputSchema() type = %v, want object", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() properties not a map")
	}

	// Check name field exists
	name, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema() name property not found")
	}
	if name["type"] != "string" {
		t.Errorf("name type = %v, want string", name["type"])
	}
}

func TestEnterWorktreeTool_Execute_DoubleEntry(t *testing.T) {
	// Create temp git repo
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	tool := NewEnterWorktreeTool()
	ctx := context.Background()

	// First entry should succeed
	result, err := tool.Execute(ctx, map[string]any{}, tmpDir)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("first Execute() IsError = true, want false")
	}

	// Second entry should fail
	result, err = tool.Execute(ctx, map[string]any{}, tmpDir)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("second Execute() IsError = false, want true")
	}
	if result.Content != "already in a worktree session. Use ExitWorktree first." {
		t.Errorf("second Execute() Content = %q, want %q", result.Content, "already in a worktree session. Use ExitWorktree first.")
	}
}

func TestEnterWorktreeTool_Execute_InvalidSlug(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	tool := NewEnterWorktreeTool()
	ctx := context.Background()

	invalidSlugs := []string{
		"/starts-with-slash",
		"ends-with-slash/",
		"double//slash",
		"has space",
		"has\ttab",
		"segment-exceeds-64-characters-segment-exceeds-64-characters-segment-exceeds-64-",
	}

	for _, slug := range invalidSlugs {
		result, err := tool.Execute(ctx, map[string]any{"name": slug}, tmpDir)
		if err != nil {
			t.Errorf("Execute() with slug %q error = %v", slug, err)
			continue
		}
		if !result.IsError {
			t.Errorf("Execute() with slug %q IsError = false, want true", slug)
		}
	}
}

func TestEnterWorktreeTool_Execute_EmptySlugGeneratesRandom(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	tool := NewEnterWorktreeTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError = true, want false, content = %s", result.Content)
	}

	// Should contain path and branch
	if result.Content == "" {
		t.Error("Execute() returned empty content")
	}
}

func TestEnterWorktreeTool_Execute_ValidSlug(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepoForTest(t, tmpDir)

	tool := NewEnterWorktreeTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{"name": "test-branch"}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Errorf("Execute() IsError = true, want false, content = %s", result.Content)
	}
}

func TestEnterWorktreeTool_Execute_NotInGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	tool := NewEnterWorktreeTool()
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{}, tmpDir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Errorf("Execute() IsError = false, want true")
	}
}

func TestValidateSlug(t *testing.T) {
	tests := []struct {
		slug    string
		wantErr bool
	}{
		{"simple", false},
		{"with-dash", false},
		{"with_underscore", false},
		{"with.dot", false},
		{"path/segment", false},
		{"a/b/c", false},
		{"abc123", false},
		{"123abc", false},
		{"segment/with-dash_and.dot", false},
		{"1segment", false},
		// Invalid
		{"", true},
		{"/start", true},
		{"end/", true},
		{"double//slash", true},
		{"has space", true},
		{"has\ttab", true},
		{"has\nnewline", true},
	}

	for _, tc := range tests {
		err := validateSlug(tc.slug)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateSlug(%q) error = %v, wantErr %v", tc.slug, err, tc.wantErr)
		}
	}
}

func TestValidateSlug_SegmentLength(t *testing.T) {
	// 64 char segment - should pass
	longSegment := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	err := validateSlug(longSegment)
	if err != nil {
		t.Errorf("64-char segment should be valid, got error: %v", err)
	}

	// 65 char segment - should fail
	longSegment += "x"
	err = validateSlug(longSegment)
	if err == nil {
		t.Error("65-char segment should be invalid")
	}
}

func TestGenerateRandomSlug(t *testing.T) {
	slug := generateRandomSlug()
	if len(slug) != 8 {
		t.Errorf("generateRandomSlug() len = %d, want 8", len(slug))
	}

	// Should be hexadecimal
	for _, c := range slug {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("generateRandomSlug() contains non-hex char: %c", c)
		}
	}

	// Should generate different values
	slug2 := generateRandomSlug()
	if slug == slug2 {
		t.Error("generateRandomSlug() generated same slug twice")
	}
}

// initGitRepoForTest initializes a git repo with initial commit.
func initGitRepoForTest(t *testing.T, dir string) {
	t.Helper()

	_, err := runGitCommandTest(dir, "init")
	if err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	runGitCommandTest(dir, "checkout", "-b", "main")

	_, err = runGitCommandTest(dir, "commit", "--allow-empty", "-m", "initial commit")
	if err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
}

func runGitCommandTest(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CreateWorktreeForTest creates a worktree for testing purposes.
func CreateWorktreeForTest(t *testing.T, repoRoot, branch string) string {
	t.Helper()

	worktreePath := filepath.Join(repoRoot, ".claude", "worktrees", branch)
	err := os.MkdirAll(filepath.Dir(worktreePath), 0755)
	if err != nil {
		t.Fatalf("failed to create worktree parent directory: %v", err)
	}

	_, err = runGitCommandTest(repoRoot, "worktree", "add", "-b", branch, worktreePath)
	if err != nil {
		t.Skip("git worktree not available or not supported")
	}

	return worktreePath
}
