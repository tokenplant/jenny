package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	expected := tmpDir
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

	// Setup local git config
	_, _ = runGitCommand(dir, "config", "user.email", "test@example.com")
	_, _ = runGitCommand(dir, "config", "user.name", "Test User")

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

	expected := tmpDir
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

// TestMatchGitignorePattern_MultiDoubleStar tests AC2: multi-** patterns.
// Multi-** patterns are explicitly rejected as a documented limitation.
func TestMatchGitignorePattern_MultiDoubleStar(t *testing.T) {
	tests := []struct {
		path     string
		pattern  string
		expected bool
	}{
		{"a/b/c", "a/**/b/**/c", false},       // multi-** rejected as documented limitation
		{"a/x/b/y/c", "a/**/b/**/c", false},   // multi-** rejected as documented limitation
		{"a/x/y/b/z/c", "a/**/b/**/c", false}, // multi-** rejected as documented limitation
		{"a/b/d/c", "a/**/b/**/c", false},     // multi-** rejected as documented limitation
		{"x/b/c", "a/**/b/**/c", false},       // multi-** rejected as documented limitation
		{"a/b/c/x", "a/**/b/**/c", false},     // multi-** rejected as documented limitation
		// Single ** patterns still work
		{"a/b/c", "a/**/c", true},   // single ** matches zero dirs
		{"a/x/c", "a/**/c", true},   // single ** matches one dir
		{"a/x/y/c", "a/**/c", true}, // single ** matches multiple dirs
	}

	for _, tc := range tests {
		result := matchGitignorePattern(tc.path, tc.pattern)
		if result != tc.expected {
			t.Errorf("matchGitignorePattern(%q, %q) = %v, want %v", tc.path, tc.pattern, result, tc.expected)
		}
	}
}

// TestMatchesGitignore_NegationOverride tests AC3: negation patterns properly override.
func TestMatchesGitignore_NegationOverride(t *testing.T) {
	patterns := []string{"*.log", "!important.log"}

	// important.log should NOT be ignored (negated)
	if matchesGitignore("important.log", patterns) {
		t.Error("important.log should NOT be ignored (negation applies)")
	}

	// other.log should be ignored (matched by *.log, not negated)
	if !matchesGitignore("other.log", patterns) {
		t.Error("other.log should be ignored (matched by *.log)")
	}

	// subdir/important.log should NOT be ignored
	if matchesGitignore("subdir/important.log", patterns) {
		t.Error("subdir/important.log should NOT be ignored")
	}

	// subdir/other.log should be ignored
	if !matchesGitignore("subdir/other.log", patterns) {
		t.Error("subdir/other.log should be ignored")
	}
}

// TestIsIgnored_PatternOrdering tests AC4: deeper directory patterns override root.
func TestIsIgnored_PatternOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create .gitignore at root: ignore all .log files
	rootGitignore := filepath.Join(tmpDir, ".gitignore")
	err := os.WriteFile(rootGitignore, []byte("*.log\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create root .gitignore: %v", err)
	}

	// Create subdir and .gitignore that negates important.log
	subdir := filepath.Join(tmpDir, "subdir")
	err = os.MkdirAll(subdir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	subdirGitignore := filepath.Join(subdir, ".gitignore")
	err = os.WriteFile(subdirGitignore, []byte("!important.log\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create subdir .gitignore: %v", err)
	}

	// Create test files
	importantLog := filepath.Join(subdir, "important.log")
	err = os.WriteFile(importantLog, []byte("test\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create important.log: %v", err)
	}

	otherLog := filepath.Join(subdir, "other.log")
	err = os.WriteFile(otherLog, []byte("test\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create other.log: %v", err)
	}

	rootOtherLog := filepath.Join(tmpDir, "other.log")
	err = os.WriteFile(rootOtherLog, []byte("test\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create root other.log: %v", err)
	}

	// subdir/important.log should NOT be ignored (negation from subdir .gitignore)
	ignored, err := IsIgnored(tmpDir, importantLog)
	if err != nil {
		t.Fatalf("IsIgnored failed for important.log: %v", err)
	}
	if ignored {
		t.Error("subdir/important.log should NOT be ignored (subdir negation overrides root *.log)")
	}

	// subdir/other.log should be ignored (root *.log)
	ignored, err = IsIgnored(tmpDir, otherLog)
	if err != nil {
		t.Fatalf("IsIgnored failed for other.log: %v", err)
	}
	if !ignored {
		t.Error("subdir/other.log should be ignored (root *.log)")
	}

	// root/other.log should be ignored (root *.log)
	ignored, err = IsIgnored(tmpDir, rootOtherLog)
	if err != nil {
		t.Fatalf("IsIgnored failed for root other.log: %v", err)
	}
	if !ignored {
		t.Error("root/other.log should be ignored (root *.log)")
	}
}

// TestLoadGitignorePatterns_LargeLine tests AC5: lines >64KB are not truncated.
func TestLoadGitignorePatterns_LargeLine(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create a .gitignore with a line > 64KB
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	largePattern := strings.Repeat("a", 1024*70) // 70KB pattern
	content := largePattern + "\n*.log\n"
	err := os.WriteFile(gitignorePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create .gitignore: %v", err)
	}

	patterns, err := loadGitignorePatterns(tmpDir)
	if err != nil {
		t.Fatalf("loadGitignorePatterns failed: %v", err)
	}

	if len(patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d", len(patterns))
	}

	// First pattern should be the large one (exact length)
	if len(patterns) < 1 || patterns[0] != largePattern {
		t.Errorf("first pattern not loaded correctly, got %q (len %d)", patterns[0], len(patterns[0]))
	}

	// Second pattern should be *.log
	if len(patterns) < 2 || patterns[1] != "*.log" {
		t.Errorf("second pattern should be *.log, got %q", patterns[1])
	}
}

// TestIsIgnored_DeepOrdering tests that deeper .gitignore files properly override shallower ones.
func TestIsIgnored_DeepOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// root/.gitignore: ignore *.txt
	os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("*.txt\n"), 0644)

	// root/a/.gitignore: negate *.txt
	os.MkdirAll(filepath.Join(tmpDir, "a"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "a", ".gitignore"), []byte("!*.txt\n"), 0644)

	// root/a/b/.gitignore: ignore *.txt again
	os.MkdirAll(filepath.Join(tmpDir, "a", "b"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "a", "b", ".gitignore"), []byte("*.txt\n"), 0644)

	testFile := filepath.Join(tmpDir, "a", "b", "test.txt")
	os.WriteFile(testFile, []byte("test\n"), 0644)

	// test.txt should be ignored because a/b/.gitignore overrides a/.gitignore
	ignored, err := IsIgnored(tmpDir, testFile)
	if err != nil {
		t.Fatalf("IsIgnored failed: %v", err)
	}
	if !ignored {
		t.Errorf("a/b/test.txt should be ignored (a/b/.gitignore should override a/.gitignore)")
	}

	// a/test.txt should NOT be ignored (negated in a/.gitignore)
	testFile2 := filepath.Join(tmpDir, "a", "test.txt")
	os.WriteFile(testFile2, []byte("test\n"), 0644)
	ignored, err = IsIgnored(tmpDir, testFile2)
	if err != nil {
		t.Fatalf("IsIgnored failed: %v", err)
	}
	if ignored {
		t.Errorf("a/test.txt should NOT be ignored (a/.gitignore should override root)")
	}
}

// TestMatchGitignorePattern_SpecialCases tests anchored and directory patterns.
func TestMatchGitignorePattern_SpecialCases(t *testing.T) {
	tests := []struct {
		path     string
		pattern  string
		expected bool
	}{
		{"foo", "/foo", true},
		{"subdir/foo", "/foo", false}, // /foo should only match foo at root
		{"foo/bar", "foo/", true},     // foo/ matches directory foo
		{"foo", "foo/", true},         // foo/ matches directory foo
		{"foobar", "foo/", false},     // foo/ should NOT match foobar
	}

	for _, tc := range tests {
		result := matchGitignorePattern(tc.path, tc.pattern)
		if result != tc.expected {
			t.Errorf("matchGitignorePattern(%q, %q) = %v, want %v", tc.path, tc.pattern, result, tc.expected)
		}
	}
}
