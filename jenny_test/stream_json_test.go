package e2e_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// cassettesDir is the path (relative to the test process's working
// directory) that holds the SSE cassette fixtures. `go test` runs each
// package's tests with the package directory as cwd, so this resolves to
// jenny_test/fixtures/cassettes when the tests are invoked as
// `go test ./jenny_test/...` from the repo root.
const cassettesDir = "fixtures/cassettes"

// echoHelloCassette is the cassette id used by the basic stream-json smoke
// test; the file is jenny_test/fixtures/cassettes/<echoHelloCassette>.sse.
const echoHelloCassette = "echo-hello"

// TestBasicStreamJsonSmoke is the AC4/AC5/AC6 end-to-end smoke test:
//
//   - AC4: jenny emits at least one "system" event and at least one
//     "result" event with subtype "success" on stdout, and exits 0.
//   - AC5: the mock server captures exactly one POST to /v1/messages,
//     with model starting with "claude-", stream == true, and
//     messages[0].role == "user" whose content contains the prompt.
//   - AC6: running the same scenario twice yields identical NDJSON
//     line counts and identical "type" sequences.
func TestBasicStreamJsonSmoke(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		// Explicitly clear any ANTHROPIC_MODEL inherited from the parent
		// environment so the binary's built-in default model is what gets
		// sent on the wire. The earlier pin
		// (ANTHROPIC_MODEL=claude-haiku-4-5-20251001) was a workaround
		// for the non-claude default; now that the default starts with
		// "claude-", the empty value lets NewClientWithModel fall
		// through to the const.
		"ANTHROPIC_MODEL=",
	}

	// --- First run --------------------------------------------------------
	res1 := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "echo hello")
	assertBasicSmokeOK(t, "first run", res1)
	assertRequestShape(t, mock.Requests())

	// --- Second run (AC6: deterministic replay) ---------------------------
	res2 := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "echo hello")
	assertBasicSmokeOK(t, "second run", res2)

	// AC6: identical line counts and identical "type" sequences.
	if len(res1.Lines) != len(res2.Lines) {
		t.Errorf(
			"AC6: line counts differ between runs: first=%d second=%d",
			len(res1.Lines), len(res2.Lines),
		)
	}
	types1 := typeSequence(res1.Parsed)
	types2 := typeSequence(res2.Parsed)
	if !reflect.DeepEqual(types1, types2) {
		t.Errorf(
			"AC6: type sequences differ between runs:\n  first:  %v\n  second: %v",
			types1, types2,
		)
	}
}

// assertBasicSmokeOK enforces AC4: exit 0, ≥1 "system" event, ≥1
// "result" event with subtype "success".
func assertBasicSmokeOK(t *testing.T, label string, res harness.RunResult) {
	t.Helper()

	if res.ExitCode != 0 {
		t.Fatalf("%s: expected exit 0, got %d; stderr=%q", label, res.ExitCode, res.Stderr)
	}

	var systemCount, successCount int
	for _, ev := range res.Parsed {
		switch ev["type"] {
		case "system":
			systemCount++
		case "result":
			if ev["subtype"] == "success" {
				successCount++
			}
		}
	}
	if systemCount < 1 {
		t.Errorf("%s: expected ≥1 'system' event, got %d", label, systemCount)
	}
	if successCount < 1 {
		t.Errorf("%s: expected ≥1 'result' event with subtype 'success', got %d", label, successCount)
	}
}

// assertRequestShape enforces AC5: exactly one captured POST, model
// starts with "claude-", stream == true, messages[0].role == "user",
// messages[0].content contains the prompt.
func assertRequestShape(t *testing.T, reqs []harness.APIRequest) {
	t.Helper()

	if len(reqs) != 1 {
		t.Fatalf("AC5: expected exactly 1 captured request, got %d", len(reqs))
	}
	body := reqs[0].Body

	model, _ := body["model"].(string)
	if !strings.HasPrefix(model, "claude-") {
		t.Errorf("AC5: model %q does not start with 'claude-'", model)
	}

	stream, _ := body["stream"].(bool)
	if !stream {
		t.Errorf("AC5: stream should be true, got %v", body["stream"])
	}

	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		t.Fatalf("AC5: expected at least one message; got 0")
	}
	firstMsg, _ := messages[0].(map[string]any)
	if firstMsg["role"] != "user" {
		t.Errorf("AC5: messages[0].role should be 'user', got %v", firstMsg["role"])
	}

	// Content is a list of content blocks. Each block is a map; the
	// "text" key (when present) carries the prompt. Search all blocks.
	content, _ := firstMsg["content"].([]any)
	const prompt = "echo hello"
	found := false
	for _, raw := range content {
		block, _ := raw.(map[string]any)
		if text, ok := block["text"].(string); ok && strings.Contains(text, prompt) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AC5: messages[0].content did not contain %q; got: %v", prompt, content)
	}
}

// typeSequence returns the ordered list of "type" values from a parsed
// NDJSON sequence, used by the AC6 determinism check.
func typeSequence(parsed []map[string]any) []string {
	out := make([]string, len(parsed))
	for i, ev := range parsed {
		if t, ok := ev["type"].(string); ok {
			out[i] = t
		} else {
			out[i] = "<no-type>"
		}
	}
	return out
}
