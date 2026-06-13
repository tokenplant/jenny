package git

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

func IsIgnored(repoRoot, path string) (bool, error) {
	// Normalize both paths to absolute + forward-slash form for consistent
	// comparison on Windows where filepath.EvalSymlinks may return different
	// formats for short-name vs long-name paths.
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return false, err
	}
	repoRoot = filepath.ToSlash(repoRoot)

	path, err = filepath.Abs(path)
	if err != nil {
		return false, err
	}
	path = filepath.ToSlash(path)

	// Try to resolve symlinks on path (mirrors repoRoot resolution).
	// On Windows, filepath.EvalSymlinks returns long names for short-name
	// paths (e.g. RUNNER~1 → LongName). Normalizing first ensures both
	// inputs are in comparable form.
	if resolvedPath, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.ToSlash(resolvedPath)
	} else if resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.ToSlash(filepath.Join(resolvedDir, filepath.Base(path)))
	}

	// resolvedRoot: resolve symlinks then normalize to forward slashes.
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRoot = repoRoot
	} else {
		resolvedRoot = filepath.ToSlash(resolvedRoot)
	}

	// relPath uses forward slashes for gitignore pattern matching.
	relPath, err := filepath.Rel(resolvedRoot, path)
	if err != nil {
		return false, err
	}
	relPath = filepath.ToSlash(relPath)

	// Load gitignore patterns from repo root.
	patterns, err := loadGitignorePatterns(repoRoot)
	if err != nil {
		return false, err
	}

	// Also load patterns from parent directories up to repo root (excluding root).
	// We collect subdirectories from the file's directory up to the repo root,
	// then process them in reverse order (root-most first) to ensure correct
	// override behavior where deeper .gitignore files win.
	var subdirs []string
	curr := filepath.Dir(path)
	for {
		// Use forward slashes for consistent comparison with resolvedRoot.
		// filepath.Dir on Windows may return backslashes even if input had forward slashes.
		currSlash := filepath.ToSlash(curr)
		if currSlash == resolvedRoot || currSlash == "." || currSlash == "/" || currSlash == "" {
			break
		}
		subdirs = append(subdirs, curr)
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	// Load patterns from subdirs in root-to-file order.
	for i := len(subdirs) - 1; i >= 0; i-- {
		parentPatterns, err := loadGitignorePatterns(subdirs[i])
		if err == nil {
			patterns = append(patterns, parentPatterns...)
		}
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

	// Leading slash means the pattern is relative to the directory of the .gitignore file.
	// Since path is already relative to the repository root (for root .gitignore),
	// we strip the leading slash and ensure we match against the full path.
	isRelative := strings.HasPrefix(pattern, "/")
	if isRelative {
		pattern = pattern[1:]
	}

	// Directory pattern: match the directory and its contents
	if strings.HasSuffix(pattern, "/") {
		base := pattern[:len(pattern)-1]
		// Match if path is the directory itself or a file/subdir within it
		if path == base || strings.HasPrefix(path, pattern) {
			return true
		}
		pattern = base
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
		return false
	}

	// Handle * glob (match any characters except /)
	if strings.Contains(pattern, "*") {
		// Simple glob matching - for gitignore, patterns without / in the pattern
		// (other than leading /) should match against the last path segment (filename)
		if !isRelative && !strings.Contains(pattern, "/") {
			// Match against last path segment (filename)
			segments := strings.Split(path, "/")
			filename := segments[len(segments)-1]
			return matchGlob(filename, pattern)
		}
		// Pattern contains / or is anchored via leading / - match against full path
		return matchGlob(path, pattern)
	}

	// Exact match - for patterns without /, match against filename only
	if !isRelative && !strings.Contains(pattern, "/") {
		segments := strings.Split(path, "/")
		filename := segments[len(segments)-1]
		return filename == pattern
	}
	// Pattern contains / or is anchored via leading / - match against full path
	return path == pattern
}

// globCache caches compiled regexps for glob patterns.
var globCache sync.Map // map[string]*regexp.Regexp

// matchGlob matches a path against a glob pattern with * wildcards.
func matchGlob(path, pattern string) bool {
	// Check cache first
	if cached, ok := globCache.Load(pattern); ok {
		return cached.(*regexp.Regexp).MatchString(path)
	}

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

	re, err := regexp.Compile(buf.String())
	if err != nil {
		return false
	}
	globCache.Store(pattern, re)
	return re.MatchString(path)
}
