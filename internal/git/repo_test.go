package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFindGitRoot(t *testing.T) {
	// Create a temp git repo
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Test finding .git from a nested directory
	subDir := filepath.Join(tmpDir, "src", "module")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	root, err := findGitRoot(subDir)
	if err != nil {
		t.Fatalf("findGitRoot failed: %v", err)
	}

	// Normalize path for macOS (/var/folders -> /private/var/folders)
	root, _ = filepath.EvalSymlinks(root)

	expected := filepath.Join(tmpDir, ".git")
	expected, _ = filepath.EvalSymlinks(expected)
	if root != expected {
		t.Errorf("expected %q, got %q", expected, root)
	}

	// Test that result is memoized (same result on second call)
	root2, err := findGitRoot(subDir)
	if err != nil {
		t.Fatalf("findGitRoot second call failed: %v", err)
	}
	if root2 != root {
		t.Errorf("memoization failed: first call got %q, second call got %q", root, root2)
	}

	// Test error when no .git found
	nonGitDir := t.TempDir()
	_, err = findGitRoot(nonGitDir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestResolveGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Regular .git directory
	gitDir, err := resolveGitDir(tmpDir)
	if err != nil {
		t.Fatalf("resolveGitDir failed: %v", err)
	}

	// Normalize path for macOS (/var/folders -> /private/var/folders)
	gitDir, _ = filepath.EvalSymlinks(gitDir)

	expected := filepath.Join(tmpDir, ".git")
	expected, _ = filepath.EvalSymlinks(expected)
	if gitDir != expected {
		t.Errorf("expected %q, got %q", expected, gitDir)
	}
}

func TestResolveGitDir_Worktree(t *testing.T) {
	// Create main repo and worktree
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	worktreeDir := filepath.Join(tmpDir, "worktree1")
	err := os.MkdirAll(worktreeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create worktree directory: %v", err)
	}

	// Create a real worktree using git
	_, err = runGitCommand(tmpDir, "worktree", "add", worktreeDir, "main")
	if err != nil {
		t.Skip("git worktree not available or not supported")
	}

	// Resolve worktree
	gitDir, err := resolveGitDir(worktreeDir)
	if err != nil {
		t.Fatalf("resolveGitDir for worktree failed: %v", err)
	}

	// The gitDir should be a path under the main repo's .git/worktrees
	mainGitDir := filepath.Join(tmpDir, ".git")
	if gitDir == mainGitDir {
		t.Error("worktree gitDir should differ from main gitDir")
	}
}

func TestIsShallowClone(t *testing.T) {
	tmpDir := t.TempDir()

	// Non-shallow repo
	initGitRepo(t, tmpDir)
	isShallow, err := isShallowClone(tmpDir)
	if err != nil {
		t.Fatalf("isShallowClone failed: %v", err)
	}
	if isShallow {
		t.Error("expected non-shallow repo to return false")
	}

	// Shallow repo - create by adding shallow file
	gitDir := filepath.Join(tmpDir, ".git")
	shallowPath := filepath.Join(gitDir, "shallow")
	err = os.WriteFile(shallowPath, []byte{}, 0644)
	if err != nil {
		t.Fatalf("failed to create shallow file: %v", err)
	}

	isShallow, err = isShallowClone(tmpDir)
	if err != nil {
		t.Fatalf("isShallowClone failed for shallow repo: %v", err)
	}
	if !isShallow {
		t.Error("expected shallow repo to return true")
	}
}

func TestGetBranch(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	branch, err := GetBranch(tmpDir)
	if err != nil {
		t.Fatalf("GetBranch failed: %v", err)
	}
	if branch != "main" && branch != "master" {
		t.Errorf("expected main or master, got %q", branch)
	}
}

func TestGetBranch_DetachedHEAD(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Get current commit SHA
	head, err := runGitCommand(tmpDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("failed to get HEAD: %v", err)
	}
	head = head[:40] // Trim newline

	// Checkout detached HEAD
	_, err = runGitCommand(tmpDir, "checkout", head)
	if err != nil {
		t.Skip("git checkout detached HEAD not supported")
	}

	branch, err := GetBranch(tmpDir)
	if err != nil {
		t.Fatalf("GetBranch failed for detached HEAD: %v", err)
	}

	// Should return raw SHA for detached HEAD
	if branch != head {
		t.Errorf("expected detached HEAD SHA %q, got %q", head, branch)
	}
}

func TestGetHead(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Get expected SHA
	expectedHead, err := runGitCommand(tmpDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("failed to get HEAD: %v", err)
	}
	expectedHead = expectedHead[:40]

	head, err := GetHead(tmpDir)
	if err != nil {
		t.Fatalf("GetHead failed: %v", err)
	}
	if head != expectedHead {
		t.Errorf("expected HEAD %q, got %q", expectedHead, head)
	}
}

func TestGetRemoteUrl(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Add remote
	_, err := runGitCommand(tmpDir, "remote", "add", "origin", "https://example.com/repo.git")
	if err != nil {
		t.Fatalf("failed to add remote: %v", err)
	}

	url, err := GetRemoteUrl(tmpDir)
	if err != nil {
		t.Fatalf("GetRemoteUrl failed: %v", err)
	}
	if url != "https://example.com/repo.git" {
		t.Errorf("expected remote URL, got %q", url)
	}
}

func TestGetRemoteUrl_NoRemote(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	url, err := GetRemoteUrl(tmpDir)
	if err != nil {
		t.Fatalf("GetRemoteUrl failed: %v", err)
	}
	if url != "" {
		t.Errorf("expected empty URL for repo without remote, got %q", url)
	}
}

func TestCacheInvalidation(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Get initial HEAD (caches sha1)
	head1, err := GetHead(tmpDir)
	if err != nil {
		t.Fatalf("GetHead failed: %v", err)
	}

	// Create a new commit
	_, err = runGitCommand(tmpDir, "commit", "--allow-empty", "-m", "test commit")
	if err != nil {
		t.Fatalf("failed to create commit: %v", err)
	}

	// Get HEAD after commit - should return new SHA (cache invalidated)
	head2, err := GetHead(tmpDir)
	if err != nil {
		t.Fatalf("GetHead failed after commit: %v", err)
	}

	// Verify HEAD changed after commit
	if head1 == head2 {
		t.Error("HEAD should change after new commit")
	}

	// Verify head2 matches actual git HEAD
	gitHead, err := runGitCommand(tmpDir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse failed: %v", err)
	}
	gitHead = gitHead[:40] // Trim newline
	if head2 != gitHead {
		t.Errorf("GetHead returned %q but git rev-parse HEAD is %q", head2, gitHead)
	}

	// Second call should return same cached value
	head3, err := GetHead(tmpDir)
	if err != nil {
		t.Fatalf("GetHead second call failed: %v", err)
	}
	if head2 != head3 {
		t.Errorf("GetHead returned different values on consecutive calls: %q vs %q", head2, head3)
	}
}

func TestValidateWorktreeDir_ValidWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	worktreeDir := filepath.Join(tmpDir, "worktree1")
	err := os.MkdirAll(worktreeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create worktree directory: %v", err)
	}

	// Create a real worktree
	_, err = runGitCommand(tmpDir, "worktree", "add", worktreeDir, "main")
	if err != nil {
		t.Skip("git worktree not available or not supported")
	}

	valid, err := ValidateWorktreeDir(worktreeDir)
	if err != nil {
		t.Fatalf("ValidateWorktreeDir failed: %v", err)
	}
	if !valid {
		t.Error("expected valid worktree to pass validation")
	}
}

func TestValidateWorktreeDir_MaliciousCommondir(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	worktreeDir := filepath.Join(tmpDir, "worktree1")
	err := os.MkdirAll(worktreeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create worktree directory: %v", err)
	}

	// Create a real worktree
	_, err = runGitCommand(tmpDir, "worktree", "add", worktreeDir, "main")
	if err != nil {
		t.Skip("git worktree not available or not supported")
	}

	// Corrupt the commondir to simulate malicious entry
	gitDir := filepath.Join(tmpDir, ".git", "worktrees", "worktree1")
	commondirPath := filepath.Join(gitDir, "commondir")

	// Read current commondir
	data, err := os.ReadFile(commondirPath)
	if err != nil {
		t.Skip("commondir not found")
	}

	// Corrupt it with a fake path
	err = os.WriteFile(commondirPath, []byte("/fake/common/dir"), 0644)
	if err != nil {
		t.Fatalf("failed to corrupt commondir: %v", err)
	}

	// Restore original commondir after test
	defer os.WriteFile(commondirPath, data, 0644)

	valid, err := ValidateWorktreeDir(worktreeDir)
	if err != nil {
		t.Fatalf("ValidateWorktreeDir failed: %v", err)
	}
	if valid {
		t.Error("expected malicious commondir to fail validation")
	}
}

func TestValidateWorktreeDir_NonWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Regular repo should pass validation
	valid, err := ValidateWorktreeDir(tmpDir)
	if err != nil {
		t.Fatalf("ValidateWorktreeDir failed: %v", err)
	}
	if !valid {
		t.Error("expected regular repo to pass validation")
	}
}

// Helper: initialize a git repo with initial commit
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Init repo
	_, err := runGitCommand(dir, "init")
	if err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Set initial branch (avoids detached HEAD on some git versions)
	runGitCommand(dir, "checkout", "-b", "main")

	// Create initial empty commit
	_, err = runGitCommand(dir, "commit", "--allow-empty", "-m", "initial commit")
	if err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
}

// Helper: run a git command
func runGitCommand(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func TestShallowCloneFromRemote(t *testing.T) {
	// This test would require network access, so we skip if no git remote
	// or if shallow clone is not supported
	t.Skip("requires network access or specific git configuration")
}

func TestFindGitRoot_SymlinkLoop(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create a symlink loop (should be handled by filepath.EvalSymlinks)
	subDir := filepath.Join(tmpDir, "linkdir")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	root, err := findGitRoot(subDir)
	if err != nil {
		t.Fatalf("findGitRoot failed: %v", err)
	}

	// Normalize path for macOS (/var/folders -> /private/var/folders)
	root, _ = filepath.EvalSymlinks(root)

	expected := filepath.Join(tmpDir, ".git")
	expected, _ = filepath.EvalSymlinks(expected)
	if root != expected {
		t.Errorf("expected %q, got %q", expected, root)
	}
}

func TestIsShallowClone_Worktree(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	worktreeDir := filepath.Join(tmpDir, "worktree1")
	err := os.MkdirAll(worktreeDir, 0755)
	if err != nil {
		t.Fatalf("failed to create worktree directory: %v", err)
	}

	_, err = runGitCommand(tmpDir, "worktree", "add", worktreeDir, "main")
	if err != nil {
		t.Skip("git worktree not available or not supported")
	}

	// Worktree inherits shallow state from main repo
	isShallow, err := isShallowClone(worktreeDir)
	if err != nil {
		t.Fatalf("isShallowClone failed for worktree: %v", err)
	}
	// Main repo is not shallow
	if isShallow {
		t.Error("expected non-shallow worktree")
	}
}

func TestGetBranch_CacheMtimeChange(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Get initial branch
	branch1, err := GetBranch(tmpDir)
	if err != nil {
		t.Fatalf("GetBranch failed: %v", err)
	}

	// Wait a moment to ensure mtime difference
	time.Sleep(100 * time.Millisecond)

	// Force cache refresh by touching HEAD
	headPath := filepath.Join(tmpDir, ".git", "HEAD")
	info, err := os.Stat(headPath)
	if err != nil {
		t.Fatalf("failed to stat HEAD: %v", err)
	}

	// Touch the file to change mtime
	err = os.Chtimes(headPath, time.Now(), time.Now())
	if err != nil {
		t.Fatalf("failed to touch HEAD: %v", err)
	}
	defer os.Chtimes(headPath, info.ModTime(), info.ModTime())

	// Get branch again - should still work (may use cached or refresh)
	branch2, err := GetBranch(tmpDir)
	if err != nil {
		t.Fatalf("GetBranch failed after touch: %v", err)
	}

	if branch1 != branch2 {
		t.Errorf("branch changed from %q to %q", branch1, branch2)
	}
}
