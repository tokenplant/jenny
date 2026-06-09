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
