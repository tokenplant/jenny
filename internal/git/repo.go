// Package git provides filesystem-based git introspection without spawning git subprocesses.
package git

import (
	"bufio"
	"os"
	"path/filepath"
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
