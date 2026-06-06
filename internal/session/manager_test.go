package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/internal/constants"
)

func TestNewSessionID(t *testing.T) {
	id1, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID() error = %v", err)
	}
	if len(id1) != 5+32 { // "sess_" + 16 bytes hex
		t.Errorf("NewSessionID() = %v, want length 37", len(id1))
	}

	id2, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID() error = %v", err)
	}
	if id1 == id2 {
		t.Errorf("NewSessionID() generated duplicate IDs: %v and %v", id1, id2)
	}
}

func TestManager_AppendAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_test123"

	// Append first entry
	entry1 := TranscriptEntry{
		Type:    "user",
		Content: "Hello",
	}
	if err := m.AppendEntry(sessionID, entry1); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Append second entry
	entry2 := TranscriptEntry{
		Type:    "assistant",
		Content: "Hi there",
	}
	if err := m.AppendEntry(sessionID, entry2); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Load transcript
	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("LoadTranscript() returned %d entries, want 2", len(entries))
	}

	if entries[0].Content != "Hello" {
		t.Errorf("entries[0].Content = %v, want Hello", entries[0].Content)
	}
	if entries[1].Content != "Hi there" {
		t.Errorf("entries[1].Content = %v, want Hi there", entries[1].Content)
	}
}

func TestManager_LoadTranscriptNotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	_, err = m.LoadTranscript("nonexistent")
	if err == nil {
		t.Error("LoadTranscript() expected error for nonexistent session")
	}
}

func TestManager_SessionExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_exists_test"
	if m.SessionExists(sessionID) {
		t.Error("SessionExists() = true, want false before creation")
	}

	// Create a session
	entry := TranscriptEntry{Type: "user", Content: "test"}
	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	if !m.SessionExists(sessionID) {
		t.Error("SessionExists() = false, want true after creation")
	}
}

func TestSplitLines(t *testing.T) {
	input := "line1\nline2\nline3\n"
	lines := splitLines(input)
	if len(lines) != 3 {
		t.Errorf("splitLines() returned %d lines, want 3", len(lines))
	}
}

func TestManager_TranscriptPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_abc123"
	path := m.transcriptPath(sessionID)
	expected := filepath.Join(tmpDir, sessionID+".jsonl")
	if path != expected {
		t.Errorf("transcriptPath() = %v, want %v", path, expected)
	}
}

func TestTranscriptEntry_JSON(t *testing.T) {
	entry := TranscriptEntry{
		Type:    "user",
		Content: "Hello, world!",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var parsed TranscriptEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if parsed.Type != entry.Type {
		t.Errorf("parsed.Type = %v, want %v", parsed.Type, entry.Type)
	}
	if parsed.Content != entry.Content {
		t.Errorf("parsed.Content = %v, want %v", parsed.Content, entry.Content)
	}
}

func TestContainsPathTraversal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"valid session id", "sess_abc123def456", false},
		{"absolute path", "/etc/passwd", true},
		{"absolute path windows", "\\windows\\system32", true},
		{"relative parent", "./foo", true},
		{"relative parent windows", ".\\foo", true},
		{"double dot parent", "../foo", true},
		{"double dot parent windows", "..\\foo", true},
		{"embedded parent", "foo/../bar", true},
		{"embedded parent windows", "foo\\..\\bar", true},
		{"parent in middle", "foo/../../bar", true},
		{"just dots", "...", false},
		{"dots without slash", "..foo", false},
		{"normal relative", "foo/bar", false},
		{"normal relative windows", "foo\\bar", false},
		{"session id with underscores", "sess_abc123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsPathTraversal(tt.input)
			if got != tt.want {
				t.Errorf("containsPathTraversal(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateSessionID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"empty string", "", true},
		{"valid session id", "sess_abc123def456", false},
		{"valid with numbers", "sess_1234567890123456", false},
		{"absolute path", "/etc/passwd", true},
		{"absolute path windows", "\\windows\\system32", true},
		{"relative parent", "./foo", true},
		{"relative parent windows", ".\\foo", true},
		{"double dot parent", "../foo", true},
		{"double dot parent windows", "..\\foo", true},
		{"embedded parent", "foo/../bar", true},
		{"just dots", "...", false},
		{"dots without slash", "..foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSessionID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestManager_EmptySessionID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Empty session ID should error on append
	err = m.AppendEntry("", TranscriptEntry{Type: "user"})
	if err == nil {
		t.Error("AppendEntry() expected error for empty session ID")
	}

	// Empty session ID should error on load
	_, err = m.LoadTranscript("")
	if err == nil {
		t.Error("LoadTranscript() expected error for empty session ID")
	}

	// SessionExists should return false for empty
	if m.SessionExists("") {
		t.Error("SessionExists() = true, want false for empty session ID")
	}
}

func TestManager_CheckRewriteSize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_rewrite_test"

	// CheckRewriteSize should return nil for nonexistent file
	err = m.CheckRewriteSize(sessionID)
	if err != nil {
		t.Errorf("CheckRewriteSize() for nonexistent file = %v, want nil", err)
	}

	// Append entries to create a transcript file
	for i := 0; i < 10; i++ {
		entry := TranscriptEntry{Type: "user", Content: fmt.Sprintf("Hello %d", i)}
		if err := m.AppendEntry(sessionID, entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// CheckRewriteSize should return nil for small file
	err = m.CheckRewriteSize(sessionID)
	if err != nil {
		t.Errorf("CheckRewriteSize() for small file = %v, want nil", err)
	}
}

func TestManager_CheckRewriteSize_TooLarge(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_too_large"

	// Create a file larger than MaxTombstoneRewriteBytes
	path := m.transcriptPath(sessionID)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// Write enough data to exceed 50 MiB
	data := make([]byte, constants.MaxTombstoneRewriteBytes+1)
	for i := range data {
		data[i] = 'x'
	}
	if _, err := f.Write(data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	f.Close()

	// CheckRewriteSize should return error for large file
	err = m.CheckRewriteSize(sessionID)
	if err == nil {
		t.Error("CheckRewriteSize() for large file = nil, want error")
	}
}

func TestManager_CheckRewriteSize_EmptySessionID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Empty session ID should error
	err = m.CheckRewriteSize("")
	if err == nil {
		t.Error("CheckRewriteSize() for empty session ID = nil, want error")
	}
}

func TestManager_CheckRewriteSize_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, true) // disabled
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// When disabled, CheckRewriteSize should return nil (no file operations)
	err = m.CheckRewriteSize("sess_any")
	if err != nil {
		t.Errorf("CheckRewriteSize() when disabled = %v, want nil", err)
	}
}

func TestManager_UserMessageExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_userexists_test"

	// Initially no user message exists
	exists, err := m.UserMessageExists(sessionID, "Hello")
	if err != nil {
		t.Fatalf("UserMessageExists() error = %v", err)
	}
	if exists {
		t.Error("UserMessageExists() = true, want false for nonexistent message")
	}

	// Append a user message
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: "Hello"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Now the message should exist
	exists, err = m.UserMessageExists(sessionID, "Hello")
	if err != nil {
		t.Fatalf("UserMessageExists() error = %v", err)
	}
	if !exists {
		t.Error("UserMessageExists() = false, want true for existing message")
	}

	// A different message should not exist
	exists, err = m.UserMessageExists(sessionID, "Goodbye")
	if err != nil {
		t.Fatalf("UserMessageExists() error = %v", err)
	}
	if exists {
		t.Error("UserMessageExists() = true, want false for nonexistent message")
	}

	// Only user messages should match
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "assistant", Content: "Hello"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}
	exists, err = m.UserMessageExists(sessionID, "Hello")
	if err != nil {
		t.Fatalf("UserMessageExists() error = %v", err)
	}
	if !exists {
		t.Error("UserMessageExists() = false, want true (assistant message should not affect user message check)")
	}
}

func TestManager_LoadTranscript_FiltersProgressTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_progress_filter_test"

	// Append a mix of chain participants and progress types
	entries := []TranscriptEntry{
		{Type: "user", Content: "Hello"},
		{Type: "progress", Content: "Thinking..."},
		{Type: "assistant", Content: "Hi there"},
		{Type: "bash_progress", Content: "Running command"},
		{Type: "mcp_progress", Content: "MCP tool running"},
		{Type: "powershell_progress", Content: "PowerShell running"},
		{Type: "attachment", Content: "file://foo.txt"},
		{Type: "system", Content: "system prompt"},
	}

	for _, e := range entries {
		if err := m.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	// Should only have4 chain participants: user, assistant, attachment, system
	if len(loaded) != 4 {
		t.Errorf("LoadTranscript() returned %d entries, want 4", len(loaded))
	}

	// Verify all returned entries are chain participants
	for _, e := range loaded {
		if progressTypes[e.Type] {
			t.Errorf("LoadTranscript() returned progress type %q, want only chain participants", e.Type)
		}
	}

	// Verify specific types are present
	types := make(map[string]bool)
	for _, e := range loaded {
		types[e.Type] = true
	}
	wantTypes := []string{"user", "assistant", "attachment", "system"}
	for _, wt := range wantTypes {
		if !types[wt] {
			t.Errorf("LoadTranscript() missing type %q", wt)
		}
	}
}

func TestManager_LoadTranscript_SkipsMalformedLines(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_malformed_test"

	// Append valid entries
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: "Hello"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "assistant", Content: "Hi"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Manually append malformed JSON
	path := m.transcriptPath(sessionID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := fmt.Fprintln(f, "this is not json{"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	f.Close()

	// LoadTranscript should skip malformed line and return valid entries
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("LoadTranscript() returned %d entries, want 2 (malformed line skipped)", len(loaded))
	}
}

func TestManager_Disabled_NoFilesCreated(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, true) // disabled
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_disabled_test"

	// AppendEntry should return nil (no-op) when disabled
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: "Hello"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// SessionExists should return false when disabled
	if m.SessionExists(sessionID) {
		t.Error("SessionExists() = true, want false when disabled")
	}

	// No transcript file should be created
	path := m.transcriptPath(sessionID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf(" transcript file should not exist when disabled, got err = %v", err)
	}
}
