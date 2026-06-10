package git

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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

	// Also load patterns from parent directories up to repo root (excluding root)
	// Deeper directory patterns are checked later (override shallower ones)
	dir := filepath.Dir(absPath)
	for dir != resolvedRoot {
		parentPatterns, err := loadGitignorePatterns(dir)
		if err == nil {
			patterns = append(patterns, parentPatterns...)
		}
		nextDir := filepath.Dir(dir)
		if nextDir == dir {
			break // reached filesystem root
		}
		dir = nextDir
	}

	return matchesGitignore(relPath, patterns), nil
}

// loadGitignorePatterns loads .gitignore patterns from a directory.
func loadGitignorePatterns(dir string) ([]string, error) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	file, err := os.Open(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// matchesGitignore checks if a path matches any of the gitignore patterns.
// Git's rule: the last matching pattern wins, unless negated by a later pattern.
func matchesGitignore(path string, patterns []string) bool {
	// Iterate forward, keep track of the last matching pattern and whether it was negated.
	// At the end, return "ignored" = hasMatch AND last match was NOT negated.
	var lastMatchNegated bool
	var hasMatch bool

	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = strings.TrimPrefix(pattern, "!")
		}

		matched := matchGitignorePattern(path, pattern)
		if matched {
			hasMatch = true
			lastMatchNegated = negated
		}
	}

	return hasMatch && !lastMatchNegated
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

		// Single ** - prefix/suffix matching
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

		// Multiple ** - not fully supported; reject with documented limitation
		// Multi-** patterns like a/**/b/**/c have complex semantics that require
		// tracking how many directories each ** matches. For correctness, we
		// explicitly reject these patterns rather than silently returning wrong results.
		return false
	}

	// Handle * glob (match any characters except /)
	if strings.Contains(pattern, "*") {
		// Simple glob matching - for gitignore, patterns without / in the pattern
		// (other than leading !) should match against the last path segment (filename)
		if !strings.Contains(pattern, "/") {
			// Match against last path segment (filename)
			segments := strings.Split(path, "/")
			filename := segments[len(segments)-1]
			return matchGlob(filename, pattern)
		}
		// Pattern contains / - match against full path
		return matchGlob(path, pattern)
	}

	// Exact match - for patterns without /, match against filename only
	if !strings.Contains(pattern, "/") {
		segments := strings.Split(path, "/")
		filename := segments[len(segments)-1]
		return filename == pattern
	}
	// Pattern contains / - match against full path
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
