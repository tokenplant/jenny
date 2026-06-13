package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/log"
)

var uuidV4Re = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestNewSessionID(t *testing.T) {
	id1, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID() error = %v", err)
	}
	if !uuidV4Re.MatchString(id1) {
		t.Errorf("NewSessionID() = %q, want UUID v4 format", id1)
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
	expected := filepath.Join(tmpDir, "sessions", sessionID, "transcript.jsonl")
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
		{"valid session id", "550e8400-e29b-41d4-a716-446655440000", false},
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
		{"plain string", "abc123", false},
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
		{"valid session id", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid with numbers", "12345678-1234-1234-1234-123456789012", false},
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
	for i := range 10 {
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
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

func TestManager_Flush(t *testing.T) {
	m, _ := NewManager("", false)
	if err := m.Flush(); err != nil {
		t.Errorf("Flush() error = %v, want nil", err)
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

func TestManager_ListSessions_EmptyDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if sessions != nil {
		t.Errorf("ListSessions() = %v, want nil for empty dir", sessions)
	}
}

func TestManager_ListSessions_NonExistentDir(t *testing.T) {
	m, err := NewManager("/tmp/jenny-nonexistent-xxxxxx", false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if sessions != nil {
		t.Errorf("ListSessions() = %v, want nil for nonexistent dir", sessions)
	}
}

func TestManager_ListSessions_Ordering(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Create sessions with deterministically different mtimes
	sessionIDs := []string{"sess_oldest", "sess_middle", "sess_newest"}
	for _, id := range sessionIDs {
		if err := m.AppendEntry(id, TranscriptEntry{Type: "user", Content: "test"}); err != nil {
			t.Fatalf("AppendEntry(%s) error = %v", id, err)
		}
		// Ensure distinct mtimes by sleeping
		time.Sleep(10 * time.Millisecond)
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if len(sessions) != 3 {
		t.Fatalf("ListSessions() returned %d sessions, want 3", len(sessions))
	}

	// Most recent first
	if sessions[0] != "sess_newest" {
		t.Errorf("ListSessions()[0] = %s, want sess_newest (most recent first)", sessions[0])
	}
	if sessions[1] != "sess_middle" {
		t.Errorf("ListSessions()[1] = %s, want sess_middle", sessions[1])
	}
	if sessions[2] != "sess_oldest" {
		t.Errorf("ListSessions()[2] = %s, want sess_oldest", sessions[2])
	}
}

func TestManager_ListSessions_FiltersNonJsonl(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Create a valid session
	if err := m.AppendEntry("sess_valid", TranscriptEntry{Type: "user", Content: "test"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Create a subdirectory inside sessions/ that has no transcript.jsonl (AC11)
	emptyDir := filepath.Join(tmpDir, "sessions", "sess_no_transcript")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create a non-directory file inside sessions/ (AC12)
	nonDirFile := filepath.Join(tmpDir, "sessions", "not_a_dir.txt")
	if err := os.WriteFile(nonDirFile, []byte("not a session"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("ListSessions() returned %d sessions, want 1 (filtered non-transcript and non-dir)", len(sessions))
	}
	if sessions[0] != "sess_valid" {
		t.Errorf("ListSessions()[0] = %s, want sess_valid", sessions[0])
	}
}

// TestAC8_ListSessions_Ordering tests AC8: sessions sorted by most recent transcript.jsonl mtime first.
func TestAC8_ListSessions_Ordering(t *testing.T) {
	TestManager_ListSessions_Ordering(t)
}

// TestAC9_ListSessions_EmptyDir tests AC9: empty sessions/ directory returns nil.
func TestAC9_ListSessions_EmptyDir(t *testing.T) {
	TestManager_ListSessions_EmptyDir(t)
}

// TestAC10_ListSessions_NonExistentDir tests AC10: non-existent sessions/ directory returns nil.
func TestAC10_ListSessions_NonExistentDir(t *testing.T) {
	TestManager_ListSessions_NonExistentDir(t)
}

// TestAC11_ListSessions_ExcludesDirsWithoutTranscript tests AC11: directories without transcript.jsonl excluded.
func TestAC11_ListSessions_ExcludesDirsWithoutTranscript(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Create a valid session with transcript.jsonl
	if err := m.AppendEntry("sess_valid", TranscriptEntry{Type: "user", Content: "test"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Create empty session directories inside sessions/ (no transcript.jsonl)
	for _, empty := range []string{"sess_empty1", "sess_empty2"} {
		emptyDir := filepath.Join(tmpDir, "sessions", empty)
		if err := os.MkdirAll(emptyDir, 0755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", empty, err)
		}
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("ListSessions() returned %d sessions, want 1 (empty dirs without transcript.jsonl excluded)", len(sessions))
	}
	if sessions[0] != "sess_valid" {
		t.Errorf("ListSessions()[0] = %s, want sess_valid", sessions[0])
	}
}

// TestAC12_ListSessions_IgnoresNonDirEntries tests AC12: non-directory entries in sessions/ are ignored.
func TestAC12_ListSessions_IgnoresNonDirEntries(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Create a valid session
	if err := m.AppendEntry("sess_valid", TranscriptEntry{Type: "user", Content: "test"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Create non-directory entries directly inside sessions/
	sessionsDir := filepath.Join(tmpDir, "sessions")
	for _, name := range []string{"sess_notjson.txt", "sess_notes.md", "random_file"} {
		path := filepath.Join(sessionsDir, name)
		if err := os.WriteFile(path, []byte("not a session"), 0644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("ListSessions() returned %d sessions, want 1 (non-directory entries should be ignored)", len(sessions))
	}
	if sessions[0] != "sess_valid" {
		t.Errorf("ListSessions()[0] = %s, want sess_valid", sessions[0])
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

	// AppendSystemPrompt should also be a no-op when disabled
	if err := m.AppendSystemPrompt(sessionID, "system prompt"); err != nil {
		t.Fatalf("AppendSystemPrompt() error = %v when disabled", err)
	}
	// SessionExists should still be false (no file written)
	if m.SessionExists(sessionID) {
		t.Error("SessionExists() = true, want false when disabled after AppendSystemPrompt")
	}
}

func TestManager_AppendAndLoadSystemPrompt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "test-sp-1"

	// Append a system prompt
	if err := m.AppendSystemPrompt(sessionID, "You are an AI assistant."); err != nil {
		t.Fatalf("AppendSystemPrompt() error = %v", err)
	}

	// Load and verify
	loaded, err := m.LoadSystemPrompt(sessionID)
	if err != nil {
		t.Fatalf("LoadSystemPrompt() error = %v", err)
	}
	if loaded != "You are an AI assistant." {
		t.Errorf("LoadSystemPrompt() = %q, want %q", loaded, "You are an AI assistant.")
	}

	// Overwrite with a new system prompt
	if err := m.AppendSystemPrompt(sessionID, "You are a different assistant."); err != nil {
		t.Fatalf("AppendSystemPrompt() overwrite error = %v", err)
	}

	// Should return the latest
	loaded, err = m.LoadSystemPrompt(sessionID)
	if err != nil {
		t.Fatalf("LoadSystemPrompt() overwrite error = %v", err)
	}
	if loaded != "You are a different assistant." {
		t.Errorf("LoadSystemPrompt() = %q, want %q", loaded, "You are a different assistant.")
	}
}

func TestManager_LoadSystemPrompt_EmptySession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Session doesn't exist - should return error
	prompt, err := m.LoadSystemPrompt("nonexistent-session")
	if err == nil {
		t.Error("LoadSystemPrompt() error = nil, want error for nonexistent session")
	}
	if prompt != "" {
		t.Errorf("LoadSystemPrompt() = %q, want empty", prompt)
	}

	// Empty session ID returns empty string without error
	prompt, err = m.LoadSystemPrompt("")
	if err != nil {
		t.Errorf("LoadSystemPrompt() error = %v, want nil for empty session ID", err)
	}
	if prompt != "" {
		t.Errorf("LoadSystemPrompt() = %q, want empty for empty session ID", prompt)
	}
}

func TestManager_AppendSystemPrompt_NilChecks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	sessionID := "test-sp-nil"

	// Empty system prompt should not create an entry
	if err := m.AppendSystemPrompt(sessionID, ""); err != nil {
		t.Errorf("AppendSystemPrompt() with empty prompt error = %v, want nil", err)
	}

	// Empty session ID should not create an entry
	if err := m.AppendSystemPrompt("", "some prompt"); err != nil {
		t.Errorf("AppendSystemPrompt() with empty session ID error = %v, want nil", err)
	}

	// Verify no state entries with system_prompt were written.
	// Session may not exist yet, that's fine.
	if m.SessionExists(sessionID) {
		entries, err := m.LoadTranscript(sessionID)
		if err != nil {
			t.Fatalf("LoadTranscript() error = %v", err)
		}
		for _, e := range entries {
			if e.Type == "state" && e.SystemPrompt != "" {
				t.Errorf("unexpected state entry with system_prompt = %q", e.SystemPrompt)
			}
		}
	}
}

// TestMalformedJSONLogging verifies that when LoadTranscript encounters a
// non-JSON line, it emits a structured log.Warn message (visible via log
// capture) and continues parsing remaining lines (AC7).
func TestMalformedJSONLogging(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_malformed_logging"

	// Append a valid entry first
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: "valid 1"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Manually append a malformed JSON line
	path := m.transcriptPath(sessionID)
	malformedLine := `{"type":"user","content":"missing brace"`
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := fmt.Fprintln(f, malformedLine); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	f.Close()

	// Append another valid entry after the malformed one
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "assistant", Content: "valid 2"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Capture log output by redirecting the package logger.
	prevOutput := log.Output()
	defer log.SetOutput(prevOutput)
	captureBuf := &bytes.Buffer{}
	log.SetOutput(captureBuf)

	// LoadTranscript should skip the malformed line and return only valid entries
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("LoadTranscript() returned %d entries, want 2 (malformed line skipped)", len(loaded))
	}

	// Verify log capture contains a Warn for the malformed line.
	captured := captureBuf.String()
	if !strings.Contains(captured, "WARN") {
		t.Errorf("expected log capture to contain WARN level, got: %q", captured)
	}
	if !strings.Contains(captured, "Malformed JSON line in transcript") {
		t.Errorf("expected log capture to contain 'Malformed JSON line in transcript', got: %q", captured)
	}
	if !strings.Contains(captured, sessionID) {
		t.Errorf("expected log capture to reference sessionID %q, got: %q", sessionID, captured)
	}
}

// TestConcurrency exercises concurrent AppendEntry and LoadTranscript on the
// same session ID. The locking implementation must (a) report zero data races
// under -race and (b) ensure that LoadTranscript never returns a truncated
// JSONL line (an entry that started but did not finish) from an in-progress
// AppendEntry (AC2).
func TestConcurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-concurrency-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_concurrency"
	const numWriters = 4
	const writesPerWriter = 25

	// Writers: each one appends N entries concurrently, then signals completion
	// via the WaitGroup.
	var writerWG sync.WaitGroup
	for w := 0; w < numWriters; w++ {
		writerWG.Add(1)
		go func(id int) {
			defer writerWG.Done()
			for i := 0; i < writesPerWriter; i++ {
				if err := m.AppendEntry(sessionID, TranscriptEntry{
					Type:    "user",
					Content: fmt.Sprintf("writer-%d-entry-%d", id, i),
				}); err != nil {
					t.Errorf("AppendEntry writer=%d i=%d error = %v", id, i, err)
					return
				}
			}
		}(w)
	}

	// Readers: continuously load the transcript while writers are active.
	// Each load must return a non-negative number of entries that is always
	// <= the total number of entries written so far.
	stopReaders := make(chan struct{})
	var readerWG sync.WaitGroup
	const numReaders = 2
	for r := 0; r < numReaders; r++ {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				loaded, err := m.LoadTranscript(sessionID)
				if err != nil {
					// We tolerate the race where the file does not
					// exist yet on the very first reader load.
					if !strings.Contains(err.Error(), "session not found") {
						t.Errorf("LoadTranscript error = %v", err)
					}
					continue
				}
				// Every loaded entry must have non-empty Content (i.e.
				// not be a truncated/partial JSONL line that survived
				// because of a missing newline).
				for _, e := range loaded {
					if e.Content == "" {
						t.Errorf("LoadTranscript returned entry with empty Content (truncated line?)")
					}
				}
			}
		}()
	}

	writerWG.Wait()
	close(stopReaders)
	readerWG.Wait()

	// Final load should see all writes from all writers.
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}
	expected := numWriters * writesPerWriter
	if len(loaded) != expected {
		t.Errorf("LoadTranscript() returned %d entries, want %d", len(loaded), expected)
	}

	// Verify all writes are accounted for by content prefix.
	seen := make(map[string]bool)
	for _, e := range loaded {
		seen[e.Content] = true
	}
	for w := 0; w < numWriters; w++ {
		for i := 0; i < writesPerWriter; i++ {
			key := fmt.Sprintf("writer-%d-entry-%d", w, i)
			if !seen[key] {
				t.Errorf("missing entry %q in final transcript", key)
			}
		}
	}
}

// TestAC2_LoadPostBoundaryMessages tests that LoadPostBoundaryMessages correctly
// filters entries after a compaction boundary.
func TestAC2_LoadPostBoundaryMessages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_boundary_test"

	// Append entries before boundary
	entries := []TranscriptEntry{
		{Type: "user", Content: "Hello"},
		{Type: "assistant", Content: "Hi there"},
		{Type: "user", Content: "How are you?"},
	}

	for _, e := range entries {
		if err := m.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Append compaction boundary
	boundaryEntry := TranscriptEntry{
		Type:    "system",
		Subtype: "compact_boundary",
		CompactMetadata: &CompactMetadata{
			Trigger:          "auto",
			PreTokens:        5000,
			PreservedSegment: 3,
		},
	}
	if err := m.AppendEntry(sessionID, boundaryEntry); err != nil {
		t.Fatalf("AppendEntry() for boundary error = %v", err)
	}

	// Append entries after boundary
	postBoundary := []TranscriptEntry{
		{Type: "user", Content: "Tell me about the project"},
		{Type: "assistant", Content: "The project is great"},
	}
	for _, e := range postBoundary {
		if err := m.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() for post-boundary error = %v", err)
		}
	}

	// Test LoadPostBoundaryMessages
	loaded, err := m.LoadPostBoundaryMessages(sessionID)
	if err != nil {
		t.Fatalf("LoadPostBoundaryMessages() error = %v", err)
	}

	// Should return only post-boundary entries (boundary + postBoundary entries = 3)
	if len(loaded) != 3 {
		t.Errorf("LoadPostBoundaryMessages() returned %d entries, want 3 (boundary + 2 post-boundary)", len(loaded))
	}

	// First entry should be the boundary
	if loaded[0].Type != "system" || loaded[0].Subtype != "compact_boundary" {
		t.Errorf("First entry is not boundary: type=%s, subtype=%s", loaded[0].Type, loaded[0].Subtype)
	}

	// Verify metadata
	if loaded[0].CompactMetadata == nil {
		t.Error("CompactMetadata is nil")
	} else {
		if loaded[0].CompactMetadata.Trigger != "auto" {
			t.Errorf("Trigger = %s, want auto", loaded[0].CompactMetadata.Trigger)
		}
		if loaded[0].CompactMetadata.PreTokens != 5000 {
			t.Errorf("PreTokens = %d, want 5000", loaded[0].CompactMetadata.PreTokens)
		}
		if loaded[0].CompactMetadata.PreservedSegment != 3 {
			t.Errorf("PreservedSegment = %d, want 3", loaded[0].CompactMetadata.PreservedSegment)
		}
	}

	// Verify post-boundary entries
	if len(loaded) > 1 {
		if loaded[1].Type != "user" || loaded[1].Content != "Tell me about the project" {
			t.Errorf("Second entry mismatch: got %+v", loaded[1])
		}
	}
}

// TestAC2_LoadPostBoundaryMessages_NoBoundary tests that when there's no boundary,
// LoadPostBoundaryMessages returns all entries (current behavior).
func TestAC2_LoadPostBoundaryMessages_NoBoundary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_no_boundary_test"

	// Append entries without any boundary
	entries := []TranscriptEntry{
		{Type: "user", Content: "Hello"},
		{Type: "assistant", Content: "Hi there"},
	}
	for _, e := range entries {
		if err := m.AppendEntry(sessionID, e); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// LoadPostBoundaryMessages should return all entries (no boundary to filter)
	loaded, err := m.LoadPostBoundaryMessages(sessionID)
	if err != nil {
		t.Fatalf("LoadPostBoundaryMessages() error = %v", err)
	}

	if len(loaded) != 2 {
		t.Errorf("LoadPostBoundaryMessages() returned %d entries, want 2", len(loaded))
	}
}

// TestAC5_MultipleCompactionBoundaries tests that only the most recent boundary applies.
func TestAC5_MultipleCompactionBoundaries(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_multi_boundary_test"

	// Append entries before first boundary
	for i := 0; i < 3; i++ {
		if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: fmt.Sprintf("msg-%d", i)}); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// First boundary
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "system", Subtype: "compact_boundary", CompactMetadata: &CompactMetadata{Trigger: "auto", PreTokens: 1000, PreservedSegment: 3}}); err != nil {
		t.Fatalf("AppendEntry() for boundary error = %v", err)
	}

	// Entries between boundaries
	for i := 0; i < 3; i++ {
		if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: fmt.Sprintf("msg-between-%d", i)}); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Second boundary (should be the one that matters)
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "system", Subtype: "compact_boundary", CompactMetadata: &CompactMetadata{Trigger: "auto", PreTokens: 2000, PreservedSegment: 3}}); err != nil {
		t.Fatalf("AppendEntry() for boundary error = %v", err)
	}

	// Entries after second boundary
	for i := 0; i < 2; i++ {
		if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: fmt.Sprintf("msg-after-%d", i)}); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// LoadPostBoundaryMessages should return only entries after the LAST boundary
	loaded, err := m.LoadPostBoundaryMessages(sessionID)
	if err != nil {
		t.Fatalf("LoadPostBoundaryMessages() error = %v", err)
	}

	// Should be: second boundary + 2 post-boundary = 3 entries
	if len(loaded) != 3 {
		t.Errorf("LoadPostBoundaryMessages() returned %d entries, want 3 (boundary + 2 post-boundary)", len(loaded))
	}

	// First entry should be the second boundary
	if loaded[0].Type != "system" || loaded[0].Subtype != "compact_boundary" {
		t.Errorf("First entry is not boundary: type=%s, subtype=%s", loaded[0].Type, loaded[0].Subtype)
	}

	// Verify it's the second boundary (PreTokens = 2000)
	if loaded[0].CompactMetadata.PreTokens != 2000 {
		t.Errorf("Got PreTokens = %d, want 2000 (second boundary)", loaded[0].CompactMetadata.PreTokens)
	}
}

// TestAC4_CompactionBoundaryMetadata tests that boundary metadata is persisted correctly.
func TestAC4_CompactionBoundaryMetadata(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_metadata_test"

	// Append boundary with specific metadata
	entry := TranscriptEntry{
		Type:    "system",
		Subtype: "compact_boundary",
		CompactMetadata: &CompactMetadata{
			Trigger:          "manual",
			PreTokens:        7500,
			PreservedSegment: 5,
		},
	}
	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Load and verify metadata
	loaded, err := m.LoadPostBoundaryMessages(sessionID)
	if err != nil {
		t.Fatalf("LoadPostBoundaryMessages() error = %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(loaded))
	}

	if loaded[0].CompactMetadata == nil {
		t.Fatal("CompactMetadata is nil")
	}

	// Verify all metadata fields are non-zero
	if loaded[0].CompactMetadata.Trigger == "" {
		t.Error("Trigger is empty")
	}
	if loaded[0].CompactMetadata.PreTokens == 0 {
		t.Error("PreTokens is 0")
	}
	if loaded[0].CompactMetadata.PreservedSegment == 0 {
		t.Error("PreservedSegment is 0")
	}
}

func TestTranscriptEntry_ThinkingPersistence(t *testing.T) {
	// Test that thinking and signature fields are correctly serialized/deserialized
	entry := TranscriptEntry{
		Type:      "assistant",
		Content:   "I need to think about this",
		Thinking:  "Let me analyze the problem step by step...",
		Signature: "sig_abc123",
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
	if parsed.Thinking != entry.Thinking {
		t.Errorf("parsed.Thinking = %v, want %v", parsed.Thinking, entry.Thinking)
	}
	if parsed.Signature != entry.Signature {
		t.Errorf("parsed.Signature = %v, want %v", parsed.Signature, entry.Signature)
	}
}

func TestTranscriptEntry_ThinkingOptional(t *testing.T) {
	// Test backward compatibility: entries without thinking/signature should work
	entry := TranscriptEntry{
		Type:    "user",
		Content: "Hello",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var parsed TranscriptEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Thinking and Signature should be empty strings (not panic or error)
	if parsed.Thinking != "" {
		t.Errorf("parsed.Thinking = %v, want empty", parsed.Thinking)
	}
	if parsed.Signature != "" {
		t.Errorf("parsed.Signature = %v, want empty", parsed.Signature)
	}
}

func TestManager_AppendAndLoadWithThinking(t *testing.T) {
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

	sessionID := "sess_thinking_test"

	// Append entry with thinking
	entry := TranscriptEntry{
		Type:      "assistant",
		Content:   "I solved the problem",
		Thinking:  "My reasoning process...",
		Signature: "sig_def456",
	}
	if err := m.AppendEntry(sessionID, entry); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Load transcript
	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("LoadTranscript() returned %d entries, want 1", len(entries))
	}

	if entries[0].Thinking != "My reasoning process..." {
		t.Errorf("entries[0].Thinking = %v, want %v", entries[0].Thinking, "My reasoning process...")
	}
	if entries[0].Signature != "sig_def456" {
		t.Errorf("entries[0].Signature = %v, want %v", entries[0].Signature, "sig_def456")
	}
}
