package parity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestTranscriptCreation verifies that jenny creates transcript files.
func TestTranscriptCreation(t *testing.T) {
	mock := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + dir,
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Errorf("expected 1 .jsonl transcript, got %d", len(files))
	}
}

// TestTranscriptEntriesValid verifies all transcript lines are valid JSON.
func TestTranscriptEntriesValid(t *testing.T) {
	mock := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + dir,
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading transcript: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
			continue
		}
		if typ, _ := obj["type"].(string); typ == "" {
			t.Errorf("line %d missing non-empty 'type' field", i)
		}
	}
}

// TestTranscriptSessionIDIsUUID verifies transcript filename is a UUID.
func TestTranscriptSessionIDIsUUID(t *testing.T) {
	mock := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + dir,
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 .jsonl transcript, got %d", len(files))
	}

	stem := strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")
	if !harness.IsValidUUID(stem) {
		t.Errorf("transcript filename stem %q is not a valid UUID v4", stem)
	}
}

// TestTranscriptSessionIDMatchesStdout verifies session_id in stream-json matches transcript.
func TestTranscriptSessionIDMatchesStdout(t *testing.T) {
	mock := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + dir,
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("expected 1 .jsonl, got %d", len(files))
	}
	stem := strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")

	var stdoutSID string
	for _, ev := range res.Parsed {
		if ev["type"] == "system" {
			sid, _ := ev["session_id"].(string)
			stdoutSID = sid
			break
		}
	}
	if stdoutSID == "" {
		t.Error("no session_id in system event")
	}
	if stdoutSID != stem {
		t.Errorf("stdout session_id=%q != transcript stem=%q", stdoutSID, stem)
	}
}

// TestTranscriptNoSessionPersistence verifies no transcript with --no-session-persistence.
func TestTranscriptNoSessionPersistence(t *testing.T) {
	mock := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + dir,
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--no-session-persistence", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) != 0 {
		t.Errorf("expected 0 .jsonl files with --no-session-persistence, got %d", len(files))
	}
}

// TestTranscriptEntriesHaveUUID verifies every transcript line has a valid uuid.
func TestTranscriptEntriesHaveUUID(t *testing.T) {
	mock := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock.Close)
	dir := t.TempDir()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + dir,
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	files := findJSONLFiles(t, dir)
	if len(files) == 0 {
		t.Fatal("no transcript file found")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("reading transcript: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		uuid, _ := obj["uuid"].(string)
		if !harness.IsValidUUID(uuid) {
			t.Errorf("line %d uuid %q is not a valid UUID v4", i, uuid)
		}
	}
}

// TestTranscriptResume verifies session resume injects history.
func TestTranscriptResume(t *testing.T) {
	transcriptDir := t.TempDir()

	mock1 := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock1.Close)
	env1 := []string{
		"ANTHROPIC_BASE_URL=" + mock1.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res1 := harness.RunJenny(t, env1, "--output-format", "stream-json", "-p", "hello resume")
	if res1.ExitCode != 0 {
		t.Fatalf("run1: jenny exited %d; stderr=%q", res1.ExitCode, res1.Stderr)
	}

	files := findJSONLFiles(t, transcriptDir)
	if len(files) != 1 {
		t.Fatalf("run1: expected 1 .jsonl, got %d", len(files))
	}
	sid := strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")

	mock2 := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock2.Close)
	env2 := []string{
		"ANTHROPIC_BASE_URL=" + mock2.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res2 := harness.RunJenny(t, env2,
		"--output-format", "stream-json", "-r", sid, "-p", "second turn")
	if res2.ExitCode != 0 {
		t.Fatalf("run2: jenny exited %d; stderr=%q", res2.ExitCode, res2.Stderr)
	}

	reqs := mock2.Requests()
	if len(reqs) == 0 {
		t.Fatal("no API requests captured for run2")
	}
	msgs, _ := reqs[0].Body["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatalf("want >= 3 messages in resumed run, got %d", len(msgs))
	}
}

// TestTranscriptContinue verifies the --continue flag resumes the latest session.
func TestTranscriptContinue(t *testing.T) {
	transcriptDir := t.TempDir()

	mock1 := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock1.Close)
	env1 := []string{
		"ANTHROPIC_BASE_URL=" + mock1.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res1 := harness.RunJenny(t, env1, "--output-format", "stream-json", "-p", "first msg")
	if res1.ExitCode != 0 {
		t.Fatalf("run1: jenny exited %d; stderr=%q", res1.ExitCode, res1.Stderr)
	}

	mock2 := harness.NewMockServer(cassetteDir)
	t.Cleanup(mock2.Close)
	env2 := []string{
		"ANTHROPIC_BASE_URL=" + mock2.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res2 := harness.RunJenny(t, env2,
		"--output-format", "stream-json", "--continue", "-p", "continue prompt")
	if res2.ExitCode != 0 {
		t.Fatalf("run2: jenny exited %d; stderr=%q", res2.ExitCode, res2.Stderr)
	}

	reqs := mock2.Requests()
	if len(reqs) == 0 {
		t.Fatal("no API requests captured for run2")
	}
	msgs, _ := reqs[0].Body["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatalf("want >= 3 messages with --continue, got %d", len(msgs))
	}
}

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
