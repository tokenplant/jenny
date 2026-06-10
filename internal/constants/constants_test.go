package constants

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScratchpadDir(t *testing.T) {
	// Override JennyHomeDir to use tmpDir
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return "/tmp/jenny-test" }
	defer func() { JennyHomeDirFunc = originalFunc }()

	// Test that ScratchpadDir returns correct path
	scratchpadDir := ScratchpadDir()
	expected := filepath.Join("/tmp/jenny-test", "scratchpad")
	if scratchpadDir != expected {
		t.Errorf("expected %s, got %s", expected, scratchpadDir)
	}
}

func TestScratchpadDir_WithHome(t *testing.T) {
	// Test with a mock home directory
	tmpDir := t.TempDir()
	originalFunc := JennyHomeDirFunc
	JennyHomeDirFunc = func() string { return tmpDir }
	defer func() { JennyHomeDirFunc = originalFunc }()

	scratchpadDir := ScratchpadDir()
	expected := filepath.Join(tmpDir, "scratchpad")
	if scratchpadDir != expected {
		t.Errorf("expected %s, got %s", expected, scratchpadDir)
	}
}

func TestJennyHomeDir(t *testing.T) {
	// Test that default JennyHomeDir returns a non-empty path
	homeDir := JennyHomeDir()
	if homeDir == "" {
		t.Error("expected non-empty JennyHomeDir")
	}

	// Test with home directory
	home, err := os.UserHomeDir()
	if err == nil {
		expected := filepath.Join(home, ".jenny")
		if JennyHomeDir() != expected {
			t.Errorf("expected %s, got %s", expected, JennyHomeDir())
		}
	}
}
