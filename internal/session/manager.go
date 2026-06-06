// Package session provides session persistence and resume functionality.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// TranscriptEntry represents a single turn in the conversation transcript.
type TranscriptEntry struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Content   string    `json:"content,omitempty"`
	ToolUse   []ToolUse `json:"tool_use,omitempty"`
	ToolID    string    `json:"tool_id,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`
}

// ToolUse represents a tool call in the transcript.
type ToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input,omitempty"`
}

// Manager handles session persistence.
type Manager struct {
	transcriptDir string
	Disabled      bool
}

// progressTypes are entry types that are not chain participants and should be
// filtered when loading transcripts for chain rebuild.
var progressTypes = map[string]bool{
	"progress":            true,
	"bash_progress":       true,
	"powershell_progress": true,
	"mcp_progress":        true,
}

// NewManager creates a new session manager with the given transcript directory.
// If disabled is true, no files are created, appended, or modified.
func NewManager(transcriptDir string, disabled bool) (*Manager, error) {
	if transcriptDir == "" {
		transcriptDir = ".jenny/transcripts"
	}
	if disabled {
		return &Manager{transcriptDir: transcriptDir, Disabled: true}, nil
	}
	// Ensure the directory exists
	if err := os.MkdirAll(transcriptDir, 0755); err != nil {
		return nil, fmt.Errorf("creating transcript directory: %w", err)
	}
	return &Manager{transcriptDir: transcriptDir}, nil
}

// NewSessionID generates a new session ID.
func NewSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session ID: %w", err)
	}
	return "sess_" + hex.EncodeToString(b), nil
}

// ParseSessionID validates a session ID format.
func ParseSessionID(id string) string {
	if id == "" {
		return ""
	}
	// Accept any non-empty string as session ID (for resume)
	return id
}

// ValidateSessionID checks if a session ID is valid (no path traversal).
func ValidateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("session ID is required")
	}
	if containsPathTraversal(id) {
		return fmt.Errorf("invalid session ID: path traversal not allowed")
	}
	return nil
}

// transcriptPath returns the path to the transcript file for a session.
func (m *Manager) transcriptPath(sessionID string) string {
	// Validate session ID doesn't contain path traversal attempts
	if sessionID == "" || containsPathTraversal(sessionID) {
		return ""
	}
	return filepath.Join(m.transcriptDir, sessionID+".jsonl")
}

// containsPathTraversal checks if a string contains path traversal components.
func containsPathTraversal(s string) bool {
	if s == "" {
		return false
	}
	// Check for absolute paths or parent directory references
	if s[0] == '/' || s[0] == '\\' {
		return true
	}
	if len(s) >= 2 && s[0] == '.' && (s[1] == '/' || s[1] == '\\' || (s[1] == '.' && (len(s) < 3 || s[2] == '/' || s[2] == '\\'))) {
		return true
	}
	// Also check for embedded ../
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && s[i+1] == '.' && (i+2 < len(s) && (s[i+2] == '/' || s[i+2] == '\\')) {
			return true
		}
	}
	return false
}

// AppendEntry appends a transcript entry to the session's transcript file.
func (m *Manager) AppendEntry(sessionID string, entry TranscriptEntry) error {
	if m.Disabled {
		return nil
	}
	if sessionID == "" {
		return fmt.Errorf("session ID is required")
	}

	// Validate session ID doesn't contain path traversal
	if containsPathTraversal(sessionID) {
		return fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	entry.Timestamp = time.Now().UTC()
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling transcript entry: %w", err)
	}

	path := m.transcriptPath(sessionID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening transcript file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, string(data)); err != nil {
		return fmt.Errorf("writing transcript entry: %w", err)
	}

	return nil
}

// UserMessageExists checks if a user message with the given content
// already exists in the transcript. This is used to avoid duplicate
// user message persistence when resuming a session.
// Returns false if the session does not exist.
func (m *Manager) UserMessageExists(sessionID string, content string) (bool, error) {
	entries, err := m.LoadTranscript(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "session not found") {
			return false, nil
		}
		return false, err
	}
	for _, entry := range entries {
		if entry.Type == "user" && entry.Content == content {
			return true, nil
		}
	}
	return false, nil
}

// LoadTranscript loads all transcript entries for a session.
// Progress/ephemeral entries (progress, bash_progress, powershell_progress, mcp_progress)
// are filtered out since they are not chain participants.
func (m *Manager) LoadTranscript(sessionID string) ([]TranscriptEntry, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}

	if containsPathTraversal(sessionID) {
		return nil, fmt.Errorf("invalid session ID: path traversal not allowed")
	}

	path := m.transcriptPath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}

	var entries []TranscriptEntry
	lines := splitLines(string(data))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var entry TranscriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines but continue
			continue
		}
		// Filter out progress/ephemeral entries
		if progressTypes[entry.Type] {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// splitLines splits a string into lines (without trailing newlines).
func splitLines(s string) []string {
	var lines []string
	for i := 0; i < len(s); {
		// Find next newline
		j := i
		for j < len(s) && s[j] != '\n' {
			j++
		}
		lines = append(lines, s[i:j])
		if j < len(s) {
			j++ // skip newline
		}
		i = j
	}
	return lines
}

// SessionExists checks if a session transcript exists.
func (m *Manager) SessionExists(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if containsPathTraversal(sessionID) {
		return false
	}
	path := m.transcriptPath(sessionID)
	_, err := os.Stat(path)
	return err == nil
}

// RegisterShutdownFlush registers a signal handler to flush pending writes
// before process exit. Since writes are synchronous, this is currently a NOP
// but provides a hook for future buffered write implementation.
func (m *Manager) RegisterShutdownFlush() {
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		// Synchronous writes are already flushed by the OS, so no action needed here.
		// This hook exists for future buffered write implementation.
		os.Exit(0)
	}()
}
