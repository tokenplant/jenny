//go:build windows

package tool

import (
	"testing"
)

// TestIsPathWithinCwdCaseInsensitive tests case-insensitive path comparison on Windows.
func TestIsPathWithinCwdCaseInsensitive(t *testing.T) {
	// Test Windows-style case insensitivity
	testCases := []struct {
		path     string
		cwd      string
		expected bool
	}{
		// Case variations of same path
		{"C:\\Users\\Test\\Documents", "c:\\users\\test\\documents", true},
		{"c:\\users\\test\\documents", "C:\\Users\\Test\\Documents", true},
		{"C:\\Users\\Test\\Documents\\file.txt", "C:\\Users\\Test\\Documents", true},
		// Different paths
		{"C:\\Users\\Other", "C:\\Users\\Test", false},
	}

	for _, tc := range testCases {
		result := isPathWithinCwd(tc.path, tc.cwd)
		if result != tc.expected {
			t.Errorf("isPathWithinCwd(%q, %q) = %v, expected %v", tc.path, tc.cwd, result, tc.expected)
		}
	}

	t.Log("AC4 PASS: isPathWithinCwd handles case-insensitive comparison for Windows paths")
}
