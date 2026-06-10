package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInstructionFile_Symlink(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	content := "test content"
	if err := os.WriteFile(agentsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if err := os.Symlink("AGENTS.md", claudePath); err != nil {
		t.Fatal(err)
	}

	got := LoadInstructionFile(tmpDir)
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}
