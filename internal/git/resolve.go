package git

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func findGitRoot(startPath string) (string, error) {
	absStart, err := filepath.EvalSymlinks(startPath)
	if err != nil {
		return "", err
	}
	absStart, err = filepath.Abs(absStart)
	if err != nil {
		return "", err
	}

	findGitRootMu.RLock()
	if root, ok := findGitRootCache[absStart]; ok {
		findGitRootMu.RUnlock()
		return root, nil
	}
	findGitRootMu.RUnlock()

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

// evictResolveCacheIfNeeded removes the oldest entry when cache exceeds max.
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
