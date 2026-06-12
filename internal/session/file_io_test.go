package session

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ipy/jenny/internal/log"
)

// TestMalformedJSONLogging_AC6 is the AC6 regression test.
// It verifies that LoadTranscript logs a warning for malformed JSON lines
// and continues parsing remaining lines.
func TestMalformedJSONLogging_AC6(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess-malformed-logging"

	// Append a valid entry first.
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "user", Content: "valid 1"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Manually append a malformed JSON line.
	path := m.transcriptPath(sessionID)
	malformedLine := `{"type":"user","content":"missing brace"`
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if _, err := fmt.Fprintln(f, malformedLine); err != nil {
		f.Close()
		t.Fatalf("Write error = %v", err)
	}
	f.Close()

	// Append another valid entry after the malformed one.
	if err := m.AppendEntry(sessionID, TranscriptEntry{Type: "assistant", Content: "valid 2"}); err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Capture log output.
	prevOutput := log.Output()
	defer log.SetOutput(prevOutput)
	captureBuf := &bytes.Buffer{}
	log.SetOutput(captureBuf)

	// AC6: LoadTranscript should skip the malformed line and return only valid entries.
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("AC6 FAIL: LoadTranscript() returned %d entries, want 2 (malformed line skipped)", len(loaded))
	}

	// AC6: Verify log capture contains a Warn for the malformed line.
	captured := captureBuf.String()
	if !strings.Contains(captured, "WARN") {
		t.Errorf("AC6 FAIL: expected log capture to contain WARN level, got: %q", captured)
	}
	if !strings.Contains(captured, "Malformed JSON line in transcript") {
		t.Errorf("AC6 FAIL: expected 'Malformed JSON line in transcript' in log, got: %q", captured)
	}
}

// TestConcurrency_AC2 is the AC2 regression test.
// It exercises concurrent AppendEntry and LoadTranscript on the same session ID.
// The RWMutex implementation must report zero data races under -race and
// ensure LoadTranscript never returns a truncated JSONL line.
func TestConcurrency_AC2(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}
	t.Parallel()

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess-concurrency"
	const numWriters = 4
	const writesPerWriter = 25

	// Writers: each appends N entries concurrently.
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

	// Readers: continuously load while writers are active.
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
					if !strings.Contains(err.Error(), "session not found") {
						t.Errorf("LoadTranscript error = %v", err)
					}
					continue
				}
				// AC2: Every entry must have non-empty Content (not a truncated line).
				for _, e := range loaded {
					if e.Content == "" {
						t.Errorf("AC2 FAIL: LoadTranscript returned entry with empty Content (truncated line?)")
					}
				}
			}
		}()
	}

	writerWG.Wait()
	close(stopReaders)
	readerWG.Wait()

	// Final load must have all entries from all writers.
	loaded, err := m.LoadTranscript(sessionID)
	if err != nil {
		t.Fatalf("LoadTranscript() error = %v", err)
	}
	expected := numWriters * writesPerWriter
	if len(loaded) != expected {
		t.Errorf("AC2 FAIL: LoadTranscript() returned %d entries, want %d", len(loaded), expected)
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
				t.Errorf("AC2 FAIL: missing entry %q in final transcript", key)
			}
		}
	}
}

// TestUTF8SafeTruncate_TruncateForLog verifies AC7: truncateForLog uses rune-aware
// slicing so multi-byte code points are never split mid-character.
func TestUTF8SafeTruncate_TruncateForLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		max    int
		wantOK bool
	}{
		{
			name:   "emoji at boundary",
			input:  "Hello 🔥",
			max:    8,
			wantOK: true,
		},
		{
			name:   "ASCII truncation",
			input:  "hello world",
			max:    5,
			wantOK: true,
		},
		{
			name:   "ASCII fits",
			input:  "hi",
			max:    10,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForLog(tt.input, tt.max)
			// AC7: result must be valid UTF-8.
			if !utf8Valid(got) {
				t.Errorf("AC7 FAIL: truncateForLog(%q, %d) = %q is not valid UTF-8", tt.input, tt.max, got)
			}
		})
	}
}

// utf8Valid returns true if s is a valid UTF-8 string.
func utf8Valid(s string) bool {
	for _, c := range s {
		if c == 0xFFFD {
			return false
		}
	}
	return true
}

// TestAtomicWrite_NoPartialFiles verifies AC4: after AppendEntry calls,
// the transcript file ends with a newline (no partial JSONL line).
func TestAtomicWrite_NoPartialFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	m, err := NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	sessionID := "sess-atomic"
	path := m.transcriptPath(sessionID)

	// Append entries.
	for i := 0; i < 10; i++ {
		if err := m.AppendEntry(sessionID, TranscriptEntry{
			Type:    "user",
			Content: fmt.Sprintf("entry-%d", i),
		}); err != nil {
			t.Fatalf("AppendEntry error = %v", err)
		}
	}

	// Verify the file ends with a newline (no partial line).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	content := string(data)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != 10 {
		t.Errorf("AC4: got %d lines, want 10", len(lines))
	}
	for _, line := range lines {
		if line == "" {
			t.Errorf("AC4: found empty line (partial/corrupt JSONL)")
		}
	}
}
