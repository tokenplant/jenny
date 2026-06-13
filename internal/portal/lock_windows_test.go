//go:build windows

package portal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAC1_WindowsFlock(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	f, err := flock(lockPath)
	if err != nil {
		t.Fatalf("flock() on Windows should succeed: %v", err)
	}
	defer f.Close()

	// Verify file exists
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Error("lock file should exist after flock")
	}

	// Second flock on same path should fail
	_, err = flock(lockPath)
	if err == nil {
		t.Error("second flock() on same path should fail")
	}
}