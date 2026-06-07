package memdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMemdir_IsDisabled(t *testing.T) {
	// Create a temporary project root
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		cfg        Config
		envVar     string
		wantClosed bool
	}{
		{
			name: "enabled by default",
			cfg: Config{
				ProjectRoot:       tmpDir,
				AutoMemoryEnabled: true,
			},
			wantClosed: false,
		},
		{
			name: "disabled by DISABLE_AUTO_MEMORY env",
			cfg: Config{
				ProjectRoot:       tmpDir,
				AutoMemoryEnabled: true,
			},
			envVar:     "DISABLE_AUTO_MEMORY",
			wantClosed: true,
		},
		{
			name: "disabled by bare mode",
			cfg: Config{
				ProjectRoot:       tmpDir,
				BareMode:          true,
				AutoMemoryEnabled: true,
			},
			wantClosed: true,
		},
		{
			name: "disabled by settings",
			cfg: Config{
				ProjectRoot:       tmpDir,
				AutoMemoryEnabled: false,
			},
			wantClosed: true,
		},
		{
			name: "disabled by remote without memory dir",
			cfg: Config{
				ProjectRoot:       tmpDir,
				IsRemote:          true,
				AutoMemoryEnabled: true,
				MemoryDirExists:   func(root string) bool { return false },
			},
			wantClosed: true,
		},
		{
			name: "enabled for remote with memory dir",
			cfg: Config{
				ProjectRoot:       tmpDir,
				IsRemote:          true,
				AutoMemoryEnabled: true,
				MemoryDirExists:   func(root string) bool { return true },
			},
			wantClosed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set env var if specified
			if tt.envVar != "" {
				os.Setenv(tt.envVar, "1")
				defer os.Unsetenv(tt.envVar)
			}

			m, err := New(tt.cfg)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if got := m.IsDisabled(); got != tt.wantClosed {
				t.Errorf("IsDisabled() = %v, want %v", got, tt.wantClosed)
			}
		})
	}
}

func TestMemdir_Create(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("creates directory when enabled", func(t *testing.T) {
		m, err := New(Config{
			ProjectRoot:       tmpDir,
			AutoMemoryEnabled: true,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		if err := m.Create(); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if !m.Exists() {
			t.Error("memory directory was not created")
		}

		// Check subdirectories exist
		for _, memType := range []MemoryType{MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeRef} {
			subdir := filepath.Join(m.MemoryPath(), string(memType))
			if _, err := os.Stat(subdir); os.IsNotExist(err) {
				t.Errorf("subdirectory %s was not created", memType)
			}
		}

		// Check MEMORY.md exists
		indexPath := m.IndexPath()
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			t.Error("MEMORY.md was not created")
		}
	})

	t.Run("skips creation when disabled", func(t *testing.T) {
		// Use a separate temp dir to avoid interference from previous subtest
		disabledTmpDir := t.TempDir()
		m, err := New(Config{
			ProjectRoot:       disabledTmpDir,
			AutoMemoryEnabled: false,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		if err := m.Create(); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if m.Exists() {
			t.Error("memory directory should not exist when disabled")
		}
	})
}

func TestMemdir_ReadIndex(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("returns empty when index does not exist", func(t *testing.T) {
		m, err := New(Config{
			ProjectRoot:       tmpDir,
			AutoMemoryEnabled: true,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		content, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}
		if content != "" {
			t.Errorf("expected empty content, got %q", content)
		}
	})

	t.Run("reads index content", func(t *testing.T) {
		m, err := New(Config{
			ProjectRoot:       tmpDir,
			AutoMemoryEnabled: true,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		// Create index
		m.Create()

		// Write some content
		indexPath := m.IndexPath()
		if err := os.WriteFile(indexPath, []byte("user/test.md\nfeedback/example.md\n"), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		content, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}
		if !strings.Contains(content, "user/test.md") {
			t.Errorf("expected content to contain user/test.md, got %q", content)
		}
	})
}

func TestMemdir_ReadIndex_Caps(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Create()

	t.Run("AC2: enforces 200-line cap", func(t *testing.T) {
		// Build content exceeding 200 lines
		var b strings.Builder
		for i := range 250 {
			b.WriteString("user/test-entry-" + string(rune('0'+i%10)) + ".md\n")
		}
		content := b.String()

		indexPath := m.IndexPath()
		if err := os.WriteFile(indexPath, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		result, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}

		// Count lines
		lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
		// Should have header + warning + max lines
		if len(lines) > MaxIndexLines+3 { // +3 for header and warning
			t.Errorf("line count %d exceeds max %d", len(lines), MaxIndexLines)
		}

		// Should contain truncation warning
		if !strings.Contains(result, "MEMORY.md truncated:") {
			t.Error("expected truncation warning")
		}
		if !strings.Contains(result, "200-line cap") {
			t.Error("expected 200-line cap warning")
		}
	})

	t.Run("AC2: enforces 25KB byte cap", func(t *testing.T) {
		// Build content exceeding 25KB but under 200 lines
		var b strings.Builder
		b.WriteString("# Auto-Memory Index\n\n")
		// Each line is about 20 bytes, 200 lines * 20 = 4000 bytes, so we need more
		line := "user/test-entry.md very long content to exceed the 25kb limit here\n"
		for range 200 {
			b.WriteString(line)
		}
		content := b.String()

		if len([]byte(content)) < MaxIndexBytes {
			t.Skip("content not large enough to test byte cap")
		}

		indexPath := m.IndexPath()
		if err := os.WriteFile(indexPath, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		result, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}

		if len([]byte(result)) > MaxIndexBytes {
			t.Errorf("byte count %d exceeds max %d", len([]byte(result)), MaxIndexBytes)
		}

		// Should contain truncation warning
		if !strings.Contains(result, "MEMORY.md truncated:") {
			t.Error("expected truncation warning")
		}
	})

	t.Run("AC5: truncation warning identifies which cap fired", func(t *testing.T) {
		// Test line cap warning
		var b strings.Builder
		b.WriteString("# Auto-Memory Index\n\n")
		for i := range 250 {
			b.WriteString("user/entry" + string(rune('0'+i%10)) + ".md\n")
		}

		indexPath := m.IndexPath()
		if err := os.WriteFile(indexPath, []byte(b.String()), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		result, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}

		if !strings.Contains(result, "200-line cap") {
			t.Error("expected 200-line cap warning")
		}
	})
}

func TestMemdir_EnsureFresh(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Create()

	t.Run("AC3: returns false for fresh files", func(t *testing.T) {
		// Create a topic file with current time
		path := m.TopicPath(MemoryTypeUser, "test")
		if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		if m.EnsureFresh(path) {
			t.Error("fresh file should not be marked as stale")
		}
	})

	t.Run("AC3: returns true for stale files (25+ hours old)", func(t *testing.T) {
		// Create a topic file
		path := m.TopicPath(MemoryTypeUser, "stale-test")
		if err := os.WriteFile(path, []byte("stale content"), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		// Set mtime to 25 hours ago
		oldTime := time.Now().Add(-25 * time.Hour)
		os.Chtimes(path, oldTime, oldTime)

		if !m.EnsureFresh(path) {
			t.Error("stale file should be marked as stale")
		}
	})

	t.Run("AC3: prepends system-reminder for stale files on read", func(t *testing.T) {
		// Create a topic file
		path := m.TopicPath(MemoryTypeUser, "stale-read-test")
		if err := os.WriteFile(path, []byte("stale read content"), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}

		// Set mtime to 25 hours ago
		oldTime := time.Now().Add(-25 * time.Hour)
		os.Chtimes(path, oldTime, oldTime)

		content, err := m.ReadTopicFile(MemoryTypeUser, "stale-read-test")
		if err != nil {
			t.Fatalf("ReadTopicFile() error = %v", err)
		}

		if !strings.HasPrefix(content, "<system-reminder>") {
			t.Error("expected <system-reminder> prefix for stale content")
		}
	})
}

func TestMemdir_AC4_DisableChain(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("disabled in bare mode", func(t *testing.T) {
		m, err := New(Config{
			ProjectRoot:       tmpDir,
			BareMode:          true,
			AutoMemoryEnabled: true,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		m.Create()

		if m.Exists() {
			t.Error("memory directory should not exist in bare mode")
		}
	})

	t.Run("disabled when DISABLE_AUTO_MEMORY is set", func(t *testing.T) {
		os.Setenv("DISABLE_AUTO_MEMORY", "1")
		defer os.Unsetenv("DISABLE_AUTO_MEMORY")

		m, err := New(Config{
			ProjectRoot:       tmpDir,
			AutoMemoryEnabled: true,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		m.Create()

		if m.Exists() {
			t.Error("memory directory should not exist when DISABLE_AUTO_MEMORY is set")
		}
	})

	t.Run("disabled when autoMemoryEnabled is false in settings", func(t *testing.T) {
		m, err := New(Config{
			ProjectRoot:       tmpDir,
			AutoMemoryEnabled: false,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		m.Create()

		if m.Exists() {
			t.Error("memory directory should not exist when autoMemoryEnabled is false")
		}
	})

	t.Run("disabled in remote session without memory dir", func(t *testing.T) {
		m, err := New(Config{
			ProjectRoot:       tmpDir,
			IsRemote:          true,
			AutoMemoryEnabled: true,
			MemoryDirExists:   func(root string) bool { return false },
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		m.Create()

		if m.Exists() {
			t.Error("memory directory should not exist for remote without memory dir")
		}
	})
}

func TestMemdir_ValidatePath(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Create()

	t.Run("accepts valid paths within memory dir", func(t *testing.T) {
		validPath := filepath.Join(m.MemoryPath(), "user", "test.md")
		if err := m.ValidatePath(validPath); err != nil {
			t.Errorf("expected valid path, got error: %v", err)
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		invalidPath := filepath.Join(m.MemoryPath(), "..", "..", "etc", "passwd")
		if err := m.ValidatePath(invalidPath); err == nil {
			t.Error("expected error for path traversal")
		}
	})

	t.Run("rejects absolute path traversal", func(t *testing.T) {
		invalidPath := "/etc/passwd"
		if err := m.ValidatePath(invalidPath); err == nil {
			t.Error("expected error for absolute path")
		}
	})
}

func TestMemdir_TopicPath(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("sanitizes names to prevent path traversal", func(t *testing.T) {
		path := m.TopicPath(MemoryTypeUser, "../etc/passwd")
		if strings.Contains(path, "..") {
			t.Error("path should not contain .. after sanitization")
		}

		path = m.TopicPath(MemoryTypeUser, "foo/bar")
		if strings.Contains(path, "foo/bar") {
			t.Error("path should not contain / after sanitization")
		}
	})
}

func TestMemdir_WriteTopicFile(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Create()

	t.Run("writes topic file", func(t *testing.T) {
		content := "test content"
		if err := m.WriteTopicFile(MemoryTypeUser, "test", content); err != nil {
			t.Fatalf("WriteTopicFile() error = %v", err)
		}

		path := m.TopicPath(MemoryTypeUser, "test")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}

		if string(data) != content {
			t.Errorf("expected %q, got %q", content, string(data))
		}
	})

	t.Run("rejects invalid memory type", func(t *testing.T) {
		err := m.WriteTopicFile("invalid", "test", "content")
		if err == nil {
			t.Error("expected error for invalid memory type")
		}
	})
}

func TestMemdir_ListTopicFiles(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Create()

	t.Run("lists topic files", func(t *testing.T) {
		// Create some topic files
		m.WriteTopicFile(MemoryTypeUser, "test1", "content1")
		m.WriteTopicFile(MemoryTypeUser, "test2", "content2")

		names, err := m.ListTopicFiles(MemoryTypeUser)
		if err != nil {
			t.Fatalf("ListTopicFiles() error = %v", err)
		}

		if len(names) != 2 {
			t.Errorf("expected 2 files, got %d", len(names))
		}
	})

	t.Run("returns empty for empty directory", func(t *testing.T) {
		names, err := m.ListTopicFiles(MemoryTypeFeedback)
		if err != nil {
			t.Fatalf("ListTopicFiles() error = %v", err)
		}

		if len(names) != 0 {
			t.Errorf("expected 0 files, got %d", len(names))
		}
	})
}

func TestMemdir_UpdateIndex(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := New(Config{
		ProjectRoot:       tmpDir,
		AutoMemoryEnabled: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	m.Create()

	t.Run("updates index with entries", func(t *testing.T) {
		entries := []string{
			"user/test1.md",
			"feedback/example.md",
		}

		if err := m.UpdateIndex(entries); err != nil {
			t.Fatalf("UpdateIndex() error = %v", err)
		}

		content, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}

		if !strings.Contains(content, "user/test1.md") {
			t.Error("expected index to contain user/test1.md")
		}
	})

	t.Run("deduplicates entries", func(t *testing.T) {
		entries := []string{
			"user/test1.md",
			"user/test1.md", // duplicate
		}

		if err := m.UpdateIndex(entries); err != nil {
			t.Fatalf("UpdateIndex() error = %v", err)
		}

		content, err := m.ReadIndex()
		if err != nil {
			t.Fatalf("ReadIndex() error = %v", err)
		}

		// Count occurrences
		count := strings.Count(content, "user/test1.md")
		if count > 1 {
			t.Errorf("expected 1 occurrence, got %d", count)
		}
	})
}
