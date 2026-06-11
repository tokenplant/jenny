package tool

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCdTildeExpansion tests that parseCdTarget correctly handles tilde expansion
// using os.UserHomeDir().
func TestCdTildeExpansion(t *testing.T) {
	// Get expected home directory
	expectedHome, err := os.UserHomeDir()
	if err != nil {
		t.Skip("UserHomeDir not available, skipping test")
	}

	// Test cd ~ (go to home)
	result := parseCdTarget("cd ~", "/some/cwd")
	if result != expectedHome {
		t.Errorf("expected home directory %q, got %q", expectedHome, result)
	}

	// Test cd ~/path (tilde expansion with subpath)
	result = parseCdTarget("cd ~/Documents", "/some/cwd")
	expected := filepath.Join(expectedHome, "Documents")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}

	t.Log("AC4 PASS: parseCdTarget uses os.UserHomeDir() for tilde expansion")
}
