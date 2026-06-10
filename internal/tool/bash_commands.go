package tool

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// resetCwdIfOutsideProject checks if command changed directory outside project and resets
func (t *BashTool) resetCwdIfOutsideProject(command string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	newCwd := parseCdTarget(command, t.commandCwd)
	if newCwd == "" {
		return
	}

	// Check if new cwd is outside project root
	if !isPathWithinCwd(newCwd, t.projectRoot) {
		t.commandCwd = t.projectRoot
	} else {
		t.commandCwd = newCwd
	}
}

// parseCdTarget extracts the target directory from a cd command
func parseCdTarget(command string, currentCwd string) string {
	command = strings.TrimSpace(command)

	// Simple cd detection
	if !strings.HasPrefix(command, "cd ") && !strings.HasPrefix(command, "cd	") {
		return ""
	}

	// Extract the path after cd
	rest := strings.TrimPrefix(command, "cd ")
	rest = strings.TrimPrefix(rest, "cd	")
	rest = strings.TrimSpace(rest)

	// Strip shell operators after the path
	rest = stripShellOperators(rest)

	if rest == "" || rest == "~" {
		home := os.Getenv("HOME")
		if home != "" {
			return home
		}
		return currentCwd
	}

	// Handle tilde expansion
	if strings.HasPrefix(rest, "~/") {
		home := os.Getenv("HOME")
		if home != "" {
			return filepath.Join(home, rest[2:])
		}
	}

	// Handle relative paths
	if filepath.IsAbs(rest) {
		return filepath.Clean(rest)
	}

	return filepath.Clean(filepath.Join(currentCwd, rest))
}

// stripShellOperators removes shell operators
func stripShellOperators(s string) string {
	shellOpRegex := regexp.MustCompile(`\s*(&&|\|\||[&|;<>]).*$`)
	return shellOpRegex.ReplaceAllString(s, "")
}

// getSleepSeconds extracts sleep duration from command
func getSleepSeconds(command string) int {
	re := regexp.MustCompile(`sleep\s+(\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(command)
	if len(matches) < 2 {
		return 0
	}

	if strings.Contains(matches[1], ".") {
		f, _ := strconv.ParseFloat(matches[1], 64)
		return int(f)
	}

	n, _ := strconv.Atoi(matches[1])
	return n
}

// dirExists checks if a directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// mkdirAll creates a directory and all parents
func mkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// isCdCommand checks if the command starts with cd
func isCdCommand(command string) bool {
	command = strings.TrimSpace(command)
	return strings.HasPrefix(command, "cd ") || strings.HasPrefix(command, "cd	")
}

// isPathWithinCwd checks if a path is within the working directory
func isPathWithinCwd(path string, cwd string) bool {
	if !filepath.IsAbs(path) {
		if strings.HasPrefix(path, "./") {
			path = filepath.Join(cwd, path[2:])
		} else if strings.HasPrefix(path, "../") {
			path = filepath.Join(cwd, path)
		} else {
			path = filepath.Join(cwd, path)
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	absPath = filepath.Clean(absPath)

	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		cwdAbs = cwd
	}
	cwdAbs = filepath.Clean(cwdAbs)

	if absPath == cwdAbs {
		return true
	}
	return strings.HasPrefix(absPath, cwdAbs+string(filepath.Separator))
}

// validateCommandPaths checks if all paths in the command are within cwd or scratchpadDir
func validateCommandPaths(command string, cwd string, scratchpadDir string) bool {
	tokens := strings.Fields(command)

	if len(tokens) > 0 {
		cmd := tokens[0]
		if cmd == "which" || cmd == "type" || cmd == "command" || cmd == "hash" || cmd == "whence" {
			return true
		}
	}

	for _, token := range tokens {
		if strings.HasPrefix(token, "-") || token == "|" || token == ">" || token == "<" {
			continue
		}

		if filepath.IsAbs(token) || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") || strings.Contains(token, "/") {
			if !isPathWithinCwd(token, cwd) && (scratchpadDir == "" || !isPathWithinCwd(token, scratchpadDir)) {
				return false
			}
		}
	}
	return true
}

// isReadOnlyCommand checks if a command is read-only
func isReadOnlyCommand(command string) bool {
	if strings.ContainsAny(command, ">|") {
		return false
	}

	readOnlyCommands := []string{
		"ls", "pwd", "whoami", "cat", "head", "tail", "grep", "find", "wc",
		"echo", "date", "which", "type", "file", "stat", "diff", "sleep",
	}
	cmd := strings.TrimSpace(command)
	for _, prefix := range readOnlyCommands {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}
