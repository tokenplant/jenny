package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ipy/jenny/internal/session"
)

// TestResumeSessionIntegration tests the full session resume flow.
// This verifies that when a session is resumed:
// 1. The transcript is loaded correctly
// 2. Message history is rebuilt properly
// 3. Tool results are correctly paired with their tool_use entries
func TestResumeSessionIntegration(t *testing.T) {
	// Create a temporary transcript directory
	tmpDir := t.TempDir()

	// Create session manager
	mgr, err := session.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_integration_test"

	// Simulate a conversation with tool calls:
	// 1. User asks to list files
	// 2. Assistant responds with tool_use for bash
	// 3. Tool result comes back with file listing
	// 4. Assistant provides final response

	entries := []session.TranscriptEntry{
		{Type: "user", Content: "List files in /tmp"},
		{Type: "assistant", Content: "", ToolUse: []session.ToolUse{
			{ID: "tool_1", Name: "bash", Input: map[string]any{"command": "ls /tmp"}},
		}},
		{Type: "tool_result", ToolID: "tool_1", Content: "file1.txt\nfile2.txt"},
		{Type: "assistant", Content: "I found 2 files: file1.txt and file2.txt"},
	}

	// Append all entries to the transcript
	for _, entry := range entries {
		if err := mgr.AppendEntry(sessionID, entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Verify transcript was persisted
	if !mgr.SessionExists(sessionID) {
		t.Fatal("SessionExists() = false, want true after appending entries")
	}

	// Load transcript (simulating what happens on resume)
	loadedEntries, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loadedEntries) != len(entries) {
		t.Errorf("LoadTranscript() returned %d entries, want %d", len(loadedEntries), len(entries))
	}

	// Rebuild messages (simulating what happens on resume)
	msgs := RebuildMessages(loadedEntries)

	// Verify message structure matches API requirements:
	// - Tool results must be in user messages, not attached to assistant's tool_use
	// - Assistant messages with tool_use should have empty ToolResults

	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages after rebuild, got %d", len(msgs))
	}

	// Verify first user message
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0] role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[0].Content != "List files in /tmp" {
		t.Errorf("msgs[0] content = %q, want %q", msgs[0].Content, "List files in /tmp")
	}

	// Verify assistant message with tool_use
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1] role = %q, want %q", msgs[1].Role, "assistant")
	}
	if len(msgs[1].ToolUse) != 1 {
		t.Errorf("msgs[1] has %d tool_use blocks, want 1", len(msgs[1].ToolUse))
	}
	if msgs[1].ToolUse[0].Name != "bash" {
		t.Errorf("msgs[1] tool_use[0] name = %q, want %q", msgs[1].ToolUse[0].Name, "bash")
	}
	if len(msgs[1].ToolResults) != 0 {
		t.Errorf("msgs[1] has %d tool_results, want 0 (tool_results must be in separate user message)", len(msgs[1].ToolResults))
	}

	// Verify tool result is in a separate user message (not attached to assistant)
	if msgs[2].Role != "user" {
		t.Errorf("msgs[2] role = %q, want %q (tool_result must be in user message)", msgs[2].Role, "user")
	}
	if len(msgs[2].ToolResults) != 1 {
		t.Errorf("msgs[2] has %d tool_results, want 1", len(msgs[2].ToolResults))
	}
	if msgs[2].ToolResults[0].ToolUseID != "tool_1" {
		t.Errorf("msgs[2] tool_results[0] tool_use_id = %q, want %q", msgs[2].ToolResults[0].ToolUseID, "tool_1")
	}
	if msgs[2].ToolResults[0].Content != "file1.txt\nfile2.txt" {
		t.Errorf("msgs[2] tool_results[0] content = %q, want %q", msgs[2].ToolResults[0].Content, "file1.txt\nfile2.txt")
	}

	// Verify final assistant message
	if msgs[3].Role != "assistant" {
		t.Errorf("msgs[3] role = %q, want %q", msgs[3].Role, "assistant")
	}
	if msgs[3].Content != "I found 2 files: file1.txt and file2.txt" {
		t.Errorf("msgs[3] content = %q, want %q", msgs[3].Content, "I found 2 files: file1.txt and file2.txt")
	}
}

// TestResumeWithEmptyTranscript tests resuming a session that has only user messages (no tools).
func TestResumeWithEmptyTranscript(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := session.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_empty_test"

	// Simple conversation with no tool calls
	entries := []session.TranscriptEntry{
		{Type: "user", Content: "Hello"},
		{Type: "assistant", Content: "Hi there!"},
		{Type: "user", Content: "How are you?"},
		{Type: "assistant", Content: "I'm doing well, thank you!"},
	}

	for _, entry := range entries {
		if err := mgr.AppendEntry(sessionID, entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	loadedEntries, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	msgs := RebuildMessages(loadedEntries)

	if len(msgs) != 4 {
		t.Errorf("expected 4 messages, got %d", len(msgs))
	}

	// Verify alternating user/assistant pattern
	expectedRoles := []string{"user", "assistant", "user", "assistant"}
	for i, expected := range expectedRoles {
		if msgs[i].Role != expected {
			t.Errorf("msgs[%d] role = %q, want %q", i, msgs[i].Role, expected)
		}
	}
}

// TestPathTraversalPrevention tests that path traversal attempts are blocked.
func TestPathTraversalPrevention(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := session.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// These session IDs should be rejected
	badIDs := []string{
		"../etc/passwd",
		"/absolute/path",
		"foo/../../bar",
		"./foo",
		"..\\windows\\system32",
	}

	for _, id := range badIDs {
		// ValidateSessionID should reject these
		err := session.ValidateSessionID(id)
		if err == nil {
			t.Errorf("ValidateSessionID(%q) = nil, want error for path traversal", id)
		}

		// SessionExists should return false for these
		if mgr.SessionExists(id) {
			t.Errorf("SessionExists(%q) = true, want false", id)
		}
	}

	// These session IDs should be accepted
	goodIDs := []string{
		"sess_abc123def456",
		"sess_550e8400e29b41d4a716446655440000",
		"my-session-id",
	}

	for _, id := range goodIDs {
		err := session.ValidateSessionID(id)
		if err != nil {
			t.Errorf("ValidateSessionID(%q) = %v, want nil", id, err)
		}
	}
}

// TestTranscriptPersistenceOnDisk verifies the transcript file exists and is valid JSONL.
func TestTranscriptPersistenceOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := session.NewManager(tmpDir)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess_persistence_test"

	// Append some entries
	entries := []session.TranscriptEntry{
		{Type: "user", Content: "Test message"},
		{Type: "assistant", Content: "Test response"},
	}

	for _, entry := range entries {
		if err := mgr.AppendEntry(sessionID, entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Verify file exists
	path := filepath.Join(tmpDir, sessionID+".jsonl")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("transcript file does not exist")
	}

	// Verify it's valid JSONL by loading it back
	loadedEntries, err := mgr.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}

	if len(loadedEntries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(loadedEntries))
	}
}
