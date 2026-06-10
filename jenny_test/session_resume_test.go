package e2e_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

func TestSessionResumeInjectsHistoryMessages(t *testing.T) {
	transcriptDir := t.TempDir()

	// --- Run 1: create a session transcript ---
	mock1 := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock1.Close)
	env1 := []string{
		"ANTHROPIC_BASE_URL=" + mock1.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res1 := harness.RunJenny(t, env1, "--output-format", "stream-json", "-p", "hello resume")
	if res1.ExitCode != 0 {
		t.Fatalf("run1: jenny exited %d; stderr=%q", res1.ExitCode, res1.Stderr)
	}

	// Extract session ID from JSONL filename.
	files := findJSONLFiles(t, transcriptDir)
	if len(files) != 1 {
		t.Fatalf("run1: expected 1 .jsonl, got %d", len(files))
	}
	sid := strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")

	// --- Run 2: resume session ---
	mock2 := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock2.Close)
	env2 := []string{
		"ANTHROPIC_BASE_URL=" + mock2.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res2 := harness.RunJenny(t, env2,
		"--output-format", "stream-json", "-r", sid, "-p", "second turn")
	// AC1
	if res2.ExitCode != 0 {
		t.Fatalf("run2: jenny exited %d; stderr=%q", res2.ExitCode, res2.Stderr)
	}

	// AC2: assistant event on stdout
	hasAssistant := false
	for line := range strings.SplitSeq(strings.TrimSpace(res2.Stdout), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil {
			if ev["type"] == "assistant" {
				hasAssistant = true
			}
		}
	}
	if !hasAssistant {
		t.Error("AC2: no assistant event in run2 stdout")
	}

	// AC3–AC6: inspect run2's API request messages.
	reqs := mock2.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC3: no API requests captured for run2")
	}
	msgs, _ := reqs[0].Body["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatalf("AC3: want ≥3 messages, got %d: %v", len(msgs), msgs)
	}

	asMsg := func(v any) map[string]any {
		m, _ := v.(map[string]any)
		return m
	}
	contentStr := func(v any) string {
		switch c := v.(type) {
		case string:
			return c
		case []any:
			if len(c) > 0 {
				if blk, ok := c[0].(map[string]any); ok {
					s, _ := blk["text"].(string)
					return s
				}
			}
		}
		return ""
	}

	// AC4: messages[0] is run1's user turn
	m0 := asMsg(msgs[0])
	if m0["role"] != "user" {
		t.Errorf("AC4: messages[0].role=%v, want 'user'", m0["role"])
	}
	if !strings.Contains(contentStr(m0["content"]), "hello resume") {
		t.Errorf("AC4: messages[0].content missing 'hello resume'; got %v", m0["content"])
	}

	// AC5: messages[1] is the assistant turn
	m1 := asMsg(msgs[1])
	if m1["role"] != "assistant" {
		t.Errorf("AC5: messages[1].role=%v, want 'assistant'", m1["role"])
	}

	// AC6: last message is run2's new user turn
	last := asMsg(msgs[len(msgs)-1])
	if last["role"] != "user" {
		t.Errorf("AC6: last message role=%v, want 'user'", last["role"])
	}
	if !strings.Contains(contentStr(last["content"]), "second turn") {
		t.Errorf("AC6: last message content missing 'second turn'; got %v", last["content"])
	}
}
