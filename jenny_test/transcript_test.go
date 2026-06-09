package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

// transcriptEnv returns env for transcript tests: mock server + controlled transcript dir.
func transcriptEnv(mockURL, transcriptDir string) []string {
	return []string{
		"ANTHROPIC_BASE_URL=" + mockURL + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
}

// findJSONLFiles returns all .jsonl files in dir (non-recursive).
func findJSONLFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %q: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// TestTranscriptFileCreated verifies AC1.
func TestTranscriptFileCreated(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC1: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Errorf("AC1: expected 1 .jsonl file, got %d", len(files))
	}
}

// TestTranscriptLinesAreValidJSON verifies AC2.
func TestTranscriptLinesAreValidJSON(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC2: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("AC2: no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("AC2: reading transcript: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("AC2: line %d is not valid JSON: %v; content: %q", i, err, line)
			continue
		}
		if typ, _ := obj["type"].(string); typ == "" {
			t.Errorf("AC2: line %d missing non-empty 'type' field; got %v", i, obj)
		}
	}
}

// TestNoSessionPersistenceSuppressesFile verifies AC3.
func TestNoSessionPersistenceSuppressesFile(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := append(transcriptEnv(mock.URL(), dir), "JENNY_TRANSCRIPT_DIR="+dir)
	res := harness.RunJenny(t, env,
		"--output-format", "stream-json", "--no-session-persistence", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC3: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 0 {
		t.Errorf("AC3: expected 0 .jsonl files with --no-session-persistence, got %d", len(files))
	}
}

// TestTranscriptEntriesHaveSessionID verifies that every non-empty line in
// the transcript carries a non-empty session_id field (AC1).
func TestTranscriptEntriesHaveSessionID(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC1: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("AC1: no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("AC1: reading transcript: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("AC1: line %d not valid JSON: %v", i, err)
			continue
		}
		sid, _ := obj["session_id"].(string)
		if sid == "" {
			t.Errorf("AC1: line %d missing non-empty session_id; got %v", i, obj)
		}
	}
}

// TestTranscriptEntriesHaveUUIDv4 verifies that every non-empty line carries
// a uuid field that is a valid lowercase UUID v4 (AC2).
func TestTranscriptEntriesHaveUUIDv4(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC2: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("AC2: no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("AC2: reading transcript: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("AC2: line %d not valid JSON: %v", i, err)
			continue
		}
		uuid, _ := obj["uuid"].(string)
		if !isValidUUID(uuid) {
			t.Errorf("AC2: line %d uuid %q is not a valid UUID v4", i, uuid)
		}
	}
}

// TestTranscriptSessionIDIsConsistent verifies that all session_id values
// within one run are the same string (AC3).
func TestTranscriptSessionIDIsConsistent(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC3: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("AC3: no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("AC3: reading transcript: %v", err)
	}

	seen := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("AC3: line %d not valid JSON: %v", i, err)
			continue
		}
		if sid, _ := obj["session_id"].(string); sid != "" {
			seen[sid] = true
		}
	}
	if len(seen) != 1 {
		t.Errorf("AC3: expected exactly 1 distinct session_id, got %d: %v", len(seen), seen)
	}
}

// TestTranscriptSessionIDMatchesFilename verifies that the session_id in
// entries equals the stem of the .jsonl file (AC4).
func TestTranscriptSessionIDMatchesFilename(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC4: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("AC4: no transcript file found")
	}
	for _, path := range files {
		stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("AC4: reading %q: %v", path, err)
		}
		for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				t.Errorf("AC4: line %d not valid JSON: %v", i, err)
				continue
			}
			sid, _ := obj["session_id"].(string)
			if sid != stem {
				t.Errorf("AC4: line %d session_id=%q, want filename stem %q", i, sid, stem)
			}
		}
	}
}

// TestStreamJsonSessionIdMatchesTranscriptStem verifies AC6: the session_id in
// stdout stream-json events matches the stem of the .jsonl transcript file.
func TestStreamJsonSessionIdMatchesTranscriptStem(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	var systemSID, resultSID string
	for _, ev := range res.Parsed {
		sid, _ := ev["session_id"].(string)
		switch ev["type"] {
		case "system":
			systemSID = sid
		case "result":
			resultSID = sid
		}
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 .jsonl, got %d", len(files))
	}
	stem := strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")

	if systemSID == "" {
		t.Error("AC1: no session_id in system event")
	}
	if systemSID != stem {
		t.Errorf("AC1: stdout system session_id=%q != transcript stem=%q", systemSID, stem)
	}

	if resultSID == "" {
		t.Error("AC2: no session_id in result event")
	}
	if resultSID != stem {
		t.Errorf("AC2: stdout result session_id=%q != transcript stem=%q", resultSID, stem)
	}

	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("AC3: reading transcript: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if sid, _ := entry["session_id"].(string); sid != stem {
			t.Errorf("AC3: transcript line %d session_id=%q != stem=%q", i, sid, stem)
		}
	}
}

// TestTranscriptEntriesHaveCWD verifies AC7: every transcript line carries a
// non-empty cwd field equal to the absolute path from which jenny was invoked.
func TestTranscriptEntriesHaveCWD(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	transcriptDir := t.TempDir()
	runDir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res := harness.RunJennyInDir(t, runDir, env,
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, transcriptDir)
	if len(files) == 0 {
		t.Fatal("no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading transcript: %v", err)
	}

	// Resolve symlinks once for macOS /var -> /private/var aliasing.
	resolvedRunDir, _ := filepath.EvalSymlinks(runDir)

	seen := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		cwd, _ := entry["cwd"].(string)
		// AC1: non-empty
		if cwd == "" {
			t.Errorf("AC1: line %d missing cwd", i)
			continue
		}
		// AC2: absolute path
		if !filepath.IsAbs(cwd) {
			t.Errorf("AC2: line %d cwd %q is not absolute", i, cwd)
		}
		// AC4: matches runDir (account for symlink aliasing)
		if cwd != runDir && cwd != resolvedRunDir {
			t.Errorf("AC4: line %d cwd=%q, want %q", i, cwd, runDir)
		}
		seen[cwd] = true
	}
	// AC3: exactly one distinct cwd
	if len(seen) > 1 {
		t.Errorf("AC3: expected 1 distinct cwd, got %d: %v", len(seen), seen)
	}
}

// TestTranscriptHasUserAndAssistantEntries verifies AC4 and AC5.
func TestTranscriptHasUserAndAssistantEntries(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	res := harness.RunJenny(t, transcriptEnv(mock.URL(), dir),
		"--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC4/AC5: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("AC4/AC5: no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("AC4/AC5: reading transcript: %v", err)
	}

	types := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(line), &obj) == nil {
			if typ, _ := obj["type"].(string); typ != "" {
				types[typ] = true
			}
		}
	}
	if !types["user"] {
		t.Error("AC4: no transcript entry with type='user' found")
	}
	if !types["assistant"] {
		t.Error("AC5: no transcript entry with type='assistant' found")
	}
}
