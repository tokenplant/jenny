package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

func TestContinueFlagResumesSession(t *testing.T) {
	transcriptDir := t.TempDir()

	// --- Run 1: create a session ---
	mock1 := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock1.Close)
	env1 := []string{
		"ANTHROPIC_BASE_URL=" + mock1.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res1 := harness.RunJenny(t, env1, "--output-format", "stream-json", "-p", "first message")
	if res1.ExitCode != 0 {
		t.Fatalf("run1: jenny exited %d; stderr=%q", res1.ExitCode, res1.Stderr)
	}

	// --- Run 2: --continue ---
	mock2 := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock2.Close)
	env2 := []string{
		"ANTHROPIC_BASE_URL=" + mock2.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + transcriptDir,
	}
	res2 := harness.RunJenny(t, env2,
		"--output-format", "stream-json", "--continue", "-p", "continue prompt")
	// AC1
	if res2.ExitCode != 0 {
		t.Fatalf("AC1: run2 jenny exited %d; stderr=%q", res2.ExitCode, res2.Stderr)
	}

	// AC2: assistant event present
	hasAssistant := false
	for line := range strings.SplitSeq(strings.TrimSpace(res2.Stdout), "\n") {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil && ev["type"] == "assistant" {
			hasAssistant = true
		}
	}
	if !hasAssistant {
		t.Error("AC2: no assistant event in run2 stdout")
	}

	// AC3–AC5: inspect messages in run2's API request
	reqs := mock2.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC3: no API requests captured for run2")
	}
	msgs, _ := reqs[0].Body["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatalf("AC3: want ≥3 messages in run2 request, got %d", len(msgs))
	}
	asMsg := func(v any) map[string]any { m, _ := v.(map[string]any); return m }
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
	m0 := asMsg(msgs[0])
	if m0["role"] != "user" {
		t.Errorf("AC4: messages[0].role=%v, want 'user'", m0["role"])
	}
	if !strings.Contains(contentStr(m0["content"]), "first message") {
		t.Errorf("AC4: messages[0] content missing 'first message'; got %v", m0["content"])
	}
	m1 := asMsg(msgs[1])
	if m1["role"] != "assistant" {
		t.Errorf("AC5: messages[1].role=%v, want 'assistant'", m1["role"])
	}
}

func TestContinueFlagNoSessionsExitsNonZero(t *testing.T) {
	emptyDir := t.TempDir()
	env := []string{
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
		"JENNY_TRANSCRIPT_DIR=" + emptyDir,
	}
	// AC6: no mock needed — jenny should fail before making any API call
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "--continue", "-p", "any")
	if res.ExitCode == 0 {
		t.Error("AC6: expected non-zero exit when no sessions exist, got 0")
	}
}
