package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ValidateWorktreeDir validates a worktree's commondir structure.
// Returns an error if the worktree appears malicious.
func ValidateWorktreeDir(worktreePath string) (bool, error) {
	gitDir, err := resolveGitDir(worktreePath)
	if err != nil {
		return false, err
	}

	// Check if this is a worktree by looking for commondir
	commondirPath := filepath.Join(gitDir, "commondir")
	data, err := os.ReadFile(commondirPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Not a worktree, regular repo
			return true, nil
		}
		return false, err
	}

	commonDir := strings.TrimSpace(string(data))

	// Resolve commonDir relative to worktreeGitDir before use
	commonDir = filepath.Join(gitDir, commonDir)

	// Validate: worktreeGitDir parent must be {commonDir}/worktrees
	worktreeGitDir := gitDir
	parentDir := filepath.Dir(worktreeGitDir)
	expectedParent := filepath.Join(commonDir, "worktrees")

	parentReal, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return false, err
	}
	expectedParentReal, err := filepath.EvalSymlinks(expectedParent)
	if err != nil {
		return false, err
	}

	if parentReal != expectedParentReal {
		// Malicious or invalid worktree structure
		return false, nil
	}

	// Validate: {worktreeGitDir}/gitdir realpath must match {realpath(gitRoot)}/.git
	gitdirFilePath := filepath.Join(worktreeGitDir, "gitdir")
	gitdirTarget, err := os.ReadFile(gitdirFilePath)
	if err != nil {
		return false, err
	}

	gitdirTargetStr := strings.TrimSpace(string(gitdirTarget))
	gitdirReal, err := filepath.EvalSymlinks(gitdirTargetStr)
	if err != nil {
		return false, err
	}

	// The gitdir should point to the main repo's .git
	gitRoot, err := findGitRoot(worktreePath)
	if err != nil {
		return false, err
	}

	gitRootReal, err := filepath.EvalSymlinks(gitRoot)
	if err != nil {
		return false, err
	}

	expectedGitdir := filepath.Join(gitRootReal, ".git")
	expectedGitdirReal, err := filepath.EvalSymlinks(expectedGitdir)
	if err != nil {
		return false, err
	}

	if gitdirReal != expectedGitdirReal {
		return false, nil
	}

	return true, nil
}

// CreateWorktree creates a new git worktree at the specified path with a new branch.
// The worktree is created at .claude/worktrees/<branch> relative to the repo root.
func CreateWorktree(repoRoot, branch string) (string, error) {
	worktreePath := filepath.Join(repoRoot, ".claude", "worktrees", branch)

	// Check if repo root is valid git repository
	if _, err := findGitRoot(repoRoot); err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		return "", fmt.Errorf("creating worktree parent directory: %w", err)
	}

	// Use git worktree add -b to create worktree with new branch
	// We need to use shell for git command since git worktree add -b requires interactive git config
	cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath)
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %w", err)
	}

	return worktreePath, nil
}

// RemoveWorktree removes a git worktree and its branch.
func RemoveWorktree(worktreePath string) error {
	// Get the repo root from the worktree path
	repoRoot, err := findGitRoot(worktreePath)
	if err != nil {
		return fmt.Errorf("finding repo root: %w", err)
	}

	// Get branch name from worktree
	gitDir, err := resolveGitDir(worktreePath)
	if err != nil {
		return fmt.Errorf("resolving gitdir: %w", err)
	}

	// Read the HEAD to get the branch name
	headPath := filepath.Join(gitDir, "HEAD")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return fmt.Errorf("reading HEAD: %w", err)
	}

	branch := strings.TrimSpace(string(data))
	if after, ok := strings.CutPrefix(branch, "ref: "); ok {
		branch = after
		// Extract branch name
		if after, ok := strings.CutPrefix(branch, "refs/heads/"); ok {
			branch = after
		}
	}

	// Remove the worktree using git worktree remove
	cmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		// If removal fails, try to remove the directory manually
		if rmErr := os.RemoveAll(worktreePath); rmErr != nil {
			return fmt.Errorf("git worktree remove failed and manual removal failed: %w, %v", err, rmErr)
		}
	}

	// Prune the branch if it exists
	if branch != "" && !isDetachedHEAD(branch) {
		pruneCmd := exec.Command("git", "branch", "-D", branch)
		pruneCmd.Dir = repoRoot
		_ = pruneCmd.Run() // Ignore errors here
	}

	return nil
}