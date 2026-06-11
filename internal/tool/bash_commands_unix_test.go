//go:build !windows

package tool

import (
	"runtime"
	"testing"
)

// TestIsPathWithinCwdCaseSensitiveUnix verifies that path comparison is case-sensitive on Unix.
// This confirms the fix for unix_path_security_violation.
func TestIsPathWithinCwdCaseSensitiveUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping Unix-specific test on Windows")
	}

	// On Unix, these should be different directories
	cwd := "/home/user/Project"
	path := "/home/user/project/file.txt"

	if isPathWithinCwd(path, cwd) {
		t.Errorf("isPathWithinCwd(%q, %q) = true, expected false (case-sensitive Unix)", path, cwd)
	}

	// Same case should still work
	cwd = "/home/user/Project"
	path = "/home/user/Project/file.txt"
	if !isPathWithinCwd(path, cwd) {
		t.Errorf("isPathWithinCwd(%q, %q) = false, expected true", path, cwd)
	}

	t.Log("PASS: isPathWithinCwd is case-sensitive on Unix")
}
