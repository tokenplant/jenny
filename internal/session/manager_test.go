package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

	m, err := NewManager(tmpDir)
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

	m, err := NewManager(tmpDir)
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

	m, err := NewManager(tmpDir)
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

	m, err := NewManager(tmpDir)
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

	m, err := NewManager(tmpDir)
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

func TestManager_UserMessageExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir)
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
