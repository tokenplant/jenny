// Package ignore provides .gitignore / .jennyignore-aware filtering for
// file-discovery tools. Patterns from .gitignore are honored unconditionally.
// Patterns from .jennyignore are additive — they let a project extend the
// ignore set beyond what is committed to source control.
//
// This package is intentionally small. We re-implement the loader (instead
// of importing internal/git) so that the tool layer has no upstream
// dependency on git-specific semantics (e.g. parent-directory walking).
package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ipy/jenny/internal/constants"
)

// LoadPatterns reads .gitignore and .jennyignore from dir and returns the
// union of their patterns. Missing files are not an error.
func LoadPatterns(dir string) []string {
	var out []string
	out = append(out, readIgnoreFile(filepath.Join(dir, ".gitignore"))...)
	out = append(out, readIgnoreFile(filepath.Join(dir, constants.IgnoreFileName))...)
	return out
}

func readIgnoreFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Match reports whether relPath matches any of the patterns.
// Patterns use the same simple syntax as .gitignore (last match wins, ! for
// negation). Directory-only patterns (those ending with "/") match the
// directory and everything beneath it.
func Match(relPath string, patterns []string) bool {
	relPath = filepath.ToSlash(relPath)

	var lastNegated bool
	var hasMatch bool
	for _, p := range patterns {
		negated := strings.HasPrefix(p, "!")
		if negated {
			p = p[1:]
		}
		if matchOne(relPath, p) {
			hasMatch = true
			lastNegated = negated
		}
	}
	return hasMatch && !lastNegated
}

func matchOne(path, pattern string) bool {
	path = filepath.ToSlash(path)
	pattern = filepath.ToSlash(pattern)

	if strings.HasSuffix(pattern, "/") {
		// directory pattern
		prefix := strings.TrimSuffix(pattern, "/")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}

	if strings.Contains(pattern, "**") {
		// single-** prefix/suffix match
		parts := strings.SplitN(pattern, "**", 2)
		prefix, suffix := parts[0], parts[1]
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			return false
		}
		if suffix != "" && !strings.HasSuffix(path, suffix) {
			return false
		}
		return true
	}

	// Pattern without / matches the last path segment (filename)
	if !strings.Contains(pattern, "/") {
		parts := strings.Split(path, "/")
		return globMatch(parts[len(parts)-1], pattern)
	}
	return globMatch(path, pattern)
}

func globMatch(path, pattern string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			b.WriteString("[^/]*")
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '{', '}', '[', ']', '|', '^', '$':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	matched, _ := regexp.MatchString(b.String(), path)
	return matched
}
