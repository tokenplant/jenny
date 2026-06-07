// Package git provides filesystem-based git introspection without spawning git subprocesses.
package git

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// maxCacheEntries is the maximum number of entries in the LRU cache.
const maxCacheEntries = 50

// Global caches
var (
	findGitRootCache = make(map[string]string)
	findGitRootMu    sync.RWMutex
	findGitRootOrder []string

	resolveGitDirCache = make(map[string]string)
	resolveGitDirMu    sync.RWMutex
	resolveGitDirOrder []string
)

// stateCache holds cached git state for a repository root.
type stateCache struct {
	mu          sync.RWMutex
	branch      string
	head        string
	remoteURL   string
	headMtime   int64
	configMtime int64
	branchMtime int64
}

// Per-root state caches
var (
	stateCaches   = make(map[string]*stateCache)
	stateCachesMu sync.RWMutex
)

func getStateCache(root string) *stateCache {
	stateCachesMu.Lock()
	defer stateCachesMu.Unlock()
	if c, ok := stateCaches[root]; ok {
		return c
	}
	c := &stateCache{}
	stateCaches[root] = c
	return c
}

// findGitRoot walks up from startPath looking for .git.
// Returns the repository root path and an error if not found.
func findGitRoot(startPath string) (string, error) {
	// Normalize and memoize
	absStart, err := filepath.EvalSymlinks(startPath)
	if err != nil {
		return "", err
	}
	absStart, err = filepath.Abs(absStart)
	if err != nil {
		return "", err
	}

	// Check cache
	findGitRootMu.RLock()
	if root, ok := findGitRootCache[absStart]; ok {
		findGitRootMu.RUnlock()
		return root, nil
	}
	findGitRootMu.RUnlock()

	// Walk up directories
	dir := absStart
	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			// Found .git
			var root string
			if info.IsDir() {
				root = dir
			} else {
				// .git is a file - resolve gitdir
				root = dir
			}

			// Normalize and cache
			normalized, err := filepath.EvalSymlinks(root)
			if err != nil {
				return "", err
			}
			normalized, err = filepath.Abs(normalized)
			if err != nil {
				return "", err
			}

			findGitRootMu.Lock()
			evictCacheIfNeeded()
			findGitRootCache[absStart] = normalized
			findGitRootOrder = append(findGitRootOrder, absStart)
			findGitRootMu.Unlock()

			return normalized, nil
		}

		// Move to parent
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// evictCacheIfNeeded removes the oldest entry when cache exceeds max.
func evictCacheIfNeeded() {
	if len(findGitRootOrder) > maxCacheEntries {
		oldest := findGitRootOrder[0]
		delete(findGitRootCache, oldest)
		findGitRootOrder = findGitRootOrder[1:]
	}
}

// resolveGitDir resolves the actual git directory path.
// When .git is a file (worktree/submodule), reads gitdir: <path> and resolves it.
// When .git is a directory, returns the .git path directly.
func resolveGitDir(startPath string) (string, error) {
	// Normalize
	absStart, err := filepath.EvalSymlinks(startPath)
	if err != nil {
		return "", err
	}
	absStart, err = filepath.Abs(absStart)
	if err != nil {
		return "", err
	}

	// Check cache
	resolveGitDirMu.RLock()
	if dir, ok := resolveGitDirCache[absStart]; ok {
		resolveGitDirMu.RUnlock()
		return dir, nil
	}
	resolveGitDirMu.RUnlock()

	gitPath := filepath.Join(absStart, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}

	var gitDir string
	if info.IsDir() {
		gitDir = gitPath
	} else {
		// .git is a file - parse gitdir line
		gitDir, err = parseGitdirFile(gitPath)
		if err != nil {
			return "", err
		}
	}

	// Normalize and cache
	normalized, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		return "", err
	}
	normalized, err = filepath.Abs(normalized)
	if err != nil {
		return "", err
	}

	resolveGitDirMu.Lock()
	evictResolveCacheIfNeeded()
	resolveGitDirCache[absStart] = normalized
	resolveGitDirOrder = append(resolveGitDirOrder, absStart)
	resolveGitDirMu.Unlock()

	return normalized, nil
}

func evictResolveCacheIfNeeded() {
	if len(resolveGitDirOrder) > maxCacheEntries {
		oldest := resolveGitDirOrder[0]
		delete(resolveGitDirCache, oldest)
		resolveGitDirOrder = resolveGitDirOrder[1:]
	}
}

// parseGitdirFile reads the gitdir path from a .git file.
// The gitdir line is relative to the .git file's directory.
func parseGitdirFile(gitFilePath string) (string, error) {
	file, err := os.Open(gitFilePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "gitdir: "); ok {
			gitdir := after
			gitdir = strings.TrimSpace(gitdir)

			// Resolve relative path against the .git file's directory
			gitFileDir := filepath.Dir(gitFilePath)
			if filepath.IsAbs(gitdir) {
				return gitdir, nil
			}
			return filepath.Abs(filepath.Join(gitFileDir, gitdir))
		}
	}
	return "", os.ErrInvalid
}

// isShallowClone returns true if the repository has a shallow file.
func isShallowClone(rootPath string) (bool, error) {
	gitDir, err := resolveGitDir(rootPath)
	if err != nil {
		return false, err
	}

	// Check for shallow file in common git directory
	shallowPath := filepath.Join(gitDir, "shallow")
	_, err = os.Stat(shallowPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GetRoot returns the repository root path for a given start path.
// It walks up from startPath looking for .git and returns the repo root.
// Returns an error if not inside a git repository.
func GetRoot(startPath string) (string, error) {
	return findGitRoot(startPath)
}

// GetBranch returns the current branch name for the repository at rootPath.
// Returns raw SHA for detached HEAD.
func GetBranch(rootPath string) (string, error) {
	gitDir, err := resolveGitDir(rootPath)
	if err != nil {
		return "", err
	}

	cache := getStateCache(gitDir)
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Check if HEAD mtime changed (use nanoseconds for precision)
	headPath := filepath.Join(gitDir, "HEAD")
	headInfo, err := os.Stat(headPath)
	if err != nil {
		return "", err
	}

	headMtime := headInfo.ModTime().UnixNano()
	needsRefresh := headMtime != cache.headMtime
	if !needsRefresh {
		// Also check if branch ref mtime changed (if we're on a branch)
		if cache.branch != "" && !isDetachedHEAD(cache.branch) {
			branchPath := filepath.Join(gitDir, "refs", "heads", cache.branch)
			branchInfo, err := os.Stat(branchPath)
			if err == nil {
				if branchInfo.ModTime().UnixNano() != cache.branchMtime {
					needsRefresh = true
				}
			}
		}
	}

	if needsRefresh {
		branch, head, err := readBranchAndHead(gitDir)
		if err != nil {
			return "", err
		}
		cache.branch = branch
		cache.head = head
		cache.headMtime = headMtime

		// Update branch mtime
		if branch != "" && !isDetachedHEAD(branch) {
			branchPath := filepath.Join(gitDir, "refs", "heads", branch)
			branchInfo, err := os.Stat(branchPath)
			if err == nil {
				cache.branchMtime = branchInfo.ModTime().UnixNano()
			}
		}
	}

	return cache.branch, nil
}

// GetHead returns the current HEAD SHA for the repository at rootPath.
func GetHead(rootPath string) (string, error) {
	gitDir, err := resolveGitDir(rootPath)
	if err != nil {
		return "", err
	}

	cache := getStateCache(gitDir)
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Check if HEAD mtime changed (use nanoseconds for precision)
	headPath := filepath.Join(gitDir, "HEAD")
	headInfo, err := os.Stat(headPath)
	if err != nil {
		return "", err
	}

	headMtime := headInfo.ModTime().UnixNano()
	needsRefresh := headMtime != cache.headMtime

	// Also check branch ref mtime (if we're on a branch)
	if !needsRefresh && cache.branch != "" && !isDetachedHEAD(cache.branch) {
		branchPath := filepath.Join(gitDir, "refs", "heads", cache.branch)
		branchInfo, err := os.Stat(branchPath)
		if err == nil {
			if branchInfo.ModTime().UnixNano() != cache.branchMtime {
				needsRefresh = true
			}
		}
	}

	if !needsRefresh && cache.head != "" {
		return cache.head, nil
	}

	// Read fresh
	branch, head, err := readBranchAndHead(gitDir)
	if err != nil {
		return "", err
	}
	cache.branch = branch
	cache.head = head
	cache.headMtime = headMtime

	// Update branch mtime if on a branch
	if branch != "" && !isDetachedHEAD(branch) {
		branchPath := filepath.Join(gitDir, "refs", "heads", branch)
		branchInfo, err := os.Stat(branchPath)
		if err == nil {
			cache.branchMtime = branchInfo.ModTime().UnixNano()
		}
	}

	return head, nil
}

// GetRemoteUrl returns the remote origin URL for the repository at rootPath.
func GetRemoteUrl(rootPath string) (string, error) {
	gitDir, err := resolveGitDir(rootPath)
	if err != nil {
		return "", err
	}

	cache := getStateCache(gitDir)
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Check if config mtime changed
	configPath := filepath.Join(gitDir, "config")
	configInfo, err := os.Stat(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	needsRefresh := true
	if configInfo != nil && cache.configMtime != 0 {
		if configInfo.ModTime().UnixNano() == cache.configMtime {
			needsRefresh = false
		}
	}

	if needsRefresh {
		url, err := readRemoteUrl(gitDir)
		if err != nil {
			return "", err
		}
		cache.remoteURL = url
		if configInfo != nil {
			cache.configMtime = configInfo.ModTime().UnixNano()
		}
	}

	return cache.remoteURL, nil
}

// readBranchAndHead reads the current branch name and HEAD SHA.
func readBranchAndHead(gitDir string) (branch string, head string, err error) {
	headPath := filepath.Join(gitDir, "HEAD")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return "", "", err
	}

	lines := strings.SplitSeq(string(data), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Detached HEAD: raw SHA - return SHA as both branch and head
		if !strings.HasPrefix(line, "ref: ") {
			return line, line, nil
		}

		// Symbolic ref
		ref := strings.TrimPrefix(line, "ref: ")
		refPath := filepath.Join(gitDir, ref)

		// Try to read the ref file
		refData, err := os.ReadFile(refPath)
		if err != nil {
			// Ref file not found - could be a symref or packed
			// For now, return the ref name
			if after, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
				branch = after
			} else {
				branch = ref
			}
			return branch, "", nil
		}

		head = strings.TrimSpace(string(refData))
		if after, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
			branch = after
		} else {
			branch = ref
		}
		return branch, head, nil
	}

	return "", "", nil
}

// readRemoteUrl reads the remote.origin.url from git config.
func readRemoteUrl(gitDir string) (string, error) {
	configPath := filepath.Join(gitDir, "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	inRemote := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[remote ") && strings.HasSuffix(line, "]") {
			// Extract remote name from "[remote \"name\"]"
			inner := strings.TrimPrefix(line, "[remote \"")
			inner = strings.TrimSuffix(inner, "\"]")
			inRemote = inner == "origin"
			continue
		}

		if inRemote && strings.HasPrefix(line, "url = ") {
			return strings.TrimPrefix(line, "url = "), nil
		}
		if inRemote && strings.HasPrefix(line, "url=") {
			return strings.TrimPrefix(line, "url="), nil
		}
	}

	return "", nil
}

// isDetachedHEAD checks if the branch name looks like a detached HEAD (raw SHA).
func isDetachedHEAD(branch string) bool {
	// SHA is 40 hex characters
	if len(branch) == 40 {
		for _, c := range branch {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return false
			}
		}
		return true
	}
	return false
}

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

// IsIgnored returns true if the given path is ignored by git.
// It checks .gitignore patterns from the repository root.
func IsIgnored(repoRoot, path string) (bool, error) {
	// Resolve symlinks on repoRoot to ensure consistent path comparison
	// (on macOS /var/folders is a symlink to /private/var/folders)
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return false, err
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return false, err
	}

	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}

	// Try to resolve symlinks on absPath (may fail if file doesn't exist)
	// If it fails, try resolving the parent directory and rejoining
	if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolvedPath
	} else if resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(absPath)); err == nil {
		// File doesn't exist but directory does - resolve via directory
		absPath = filepath.Join(resolvedDir, filepath.Base(absPath))
	}

	// Get the relative path from repo root
	relPath, err := filepath.Rel(resolvedRoot, absPath)
	if err != nil {
		return false, err
	}

	// Load gitignore patterns from repo root
	patterns, err := loadGitignorePatterns(repoRoot)
	if err != nil {
		return false, err
	}

	// Also load patterns from parent directories up to repo root
	dir := filepath.Dir(absPath)
	resolvedRootBase := resolvedRoot
	for dir != resolvedRootBase && dir != filepath.Dir(dir) {
		parentPatterns, err := loadGitignorePatterns(dir)
		if err == nil {
			patterns = append(patterns, parentPatterns...)
		}
		dir = filepath.Dir(dir)
		if dir == resolvedRootBase {
			break
		}
	}

	return matchesGitignore(relPath, patterns), nil
}

// loadGitignorePatterns loads .gitignore patterns from a directory.
func loadGitignorePatterns(dir string) ([]string, error) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var patterns []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, nil
}

// matchesGitignore checks if a path matches any of the gitignore patterns.
func matchesGitignore(path string, patterns []string) bool {
	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = strings.TrimPrefix(pattern, "!")
		}

		matched := matchGitignorePattern(path, pattern)
		if negated {
			if matched {
				// Negated pattern matches - path is NOT ignored
				return false
			}
		} else if matched {
			return true
		}
	}
	return false
}

// matchGitignorePattern checks if a path matches a single gitignore pattern.
func matchGitignorePattern(path, pattern string) bool {
	// Normalize path separators
	path = filepath.ToSlash(path)
	pattern = filepath.ToSlash(pattern)

	// Directory pattern: match the directory and its contents
	if strings.HasSuffix(pattern, "/") {
		if strings.HasPrefix(path, pattern[:len(pattern)-1]) {
			return true
		}
		// Also match if path IS the directory
		if path == pattern[:len(pattern)-1] {
			return true
		}
		pattern = pattern[:len(pattern)-1]
	}

	// Handle ** glob (match any number of directories)
	if strings.Contains(pattern, "**") {
		// Split by **
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := parts[0]
			suffix := parts[1]

			// prefix must match the start of path
			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}
			// suffix must match the end of path
			if suffix != "" {
				return strings.HasSuffix(path, suffix)
			}
			return true
		}
	}

	// Handle * glob (match any characters except /)
	if strings.Contains(pattern, "*") {
		// Simple glob matching
		return matchGlob(path, pattern)
	}

	// Exact match
	return path == pattern
}

// matchGlob matches a path against a glob pattern with * wildcards.
func matchGlob(path, pattern string) bool {
	// Convert glob pattern to regex
	var buf strings.Builder
	buf.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			buf.WriteString("[^/]*")
		case '?':
			buf.WriteString(".")
		case '.', '+', '^', '$', '(', ')', '{', '}', '[', ']', '|':
			buf.WriteString("\\")
			buf.WriteRune(rune(c))
		default:
			buf.WriteRune(rune(c))
		}
	}
	buf.WriteString("$")

	matched, _ := regexp.MatchString(buf.String(), path)
	return matched
}
