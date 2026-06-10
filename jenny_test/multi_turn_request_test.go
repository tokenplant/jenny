package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// runToolUseWithRequests runs the two-turn tool-use scenario and returns
// both the parsed NDJSON output and all HTTP requests captured by the mock.
func runToolUseWithRequests(t *testing.T) ([]map[string]any, []harness.APIRequest) {
	t.Helper()
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	mock.SetSequence("tool-use-req", []string{"tool-use-turn1", "tool-use-turn2"})
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/tool-use-req",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "run echo hello")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	return res.Parsed, mock.Requests()
}

// TestMultiTurnTwoRequestsCaptured verifies AC1.
func TestMultiTurnTwoRequestsCaptured(t *testing.T) {
	_, reqs := runToolUseWithRequests(t)
	if len(reqs) != 2 {
		t.Errorf("AC1: expected 2 captured requests, got %d", len(reqs))
	}
}

// TestMultiTurnSecondRequestHasHistory verifies AC2.
func TestMultiTurnSecondRequestHasHistory(t *testing.T) {
	_, reqs := runToolUseWithRequests(t)
	if len(reqs) < 2 {
		t.Fatal("AC2: prerequisite — need 2 captured requests")
	}
	msgs, ok := reqs[1].Body["messages"].([]any)
	if !ok {
		t.Fatalf("AC2: messages is not an array; got %T", reqs[1].Body["messages"])
	}
	if len(msgs) < 3 {
		t.Errorf("AC2: expected >= 3 messages in second request, got %d", len(msgs))
	}
}

// TestMultiTurnAssistantMessageShape verifies AC3.
func TestMultiTurnAssistantMessageShape(t *testing.T) {
	_, reqs := runToolUseWithRequests(t)
	if len(reqs) < 2 {
		t.Fatal("prerequisite — need 2 requests")
	}
	msgs, _ := reqs[1].Body["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatal("AC3: prerequisite — need at least 2 messages")
	}
	asst, ok := msgs[1].(map[string]any)
	if !ok {
		t.Fatalf("AC3: messages[1] is not an object; got %T", msgs[1])
	}
	if asst["role"] != "assistant" {
		t.Errorf("AC3: messages[1].role = %q; want \"assistant\"", asst["role"])
	}
	content, _ := asst["content"].([]any)
	if len(content) == 0 {
		t.Fatal("AC3: messages[1].content is empty")
	}
	found := false
	for _, block := range content {
		if b, ok := block.(map[string]any); ok && b["type"] == "tool_use" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AC3: no tool_use block in messages[1].content")
	}
}

// TestMultiTurnToolResultShape verifies AC4.
func TestMultiTurnToolResultShape(t *testing.T) {
	_, reqs := runToolUseWithRequests(t)
	if len(reqs) < 2 {
		t.Fatal("prerequisite — need 2 requests")
	}
	msgs, _ := reqs[1].Body["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatal("AC4: prerequisite — need at least 3 messages")
	}
	usr, ok := msgs[2].(map[string]any)
	if !ok {
		t.Fatalf("AC4: messages[2] is not an object; got %T", msgs[2])
	}
	if usr["role"] != "user" {
		t.Errorf("AC4: messages[2].role = %q; want \"user\"", usr["role"])
	}
	content, _ := usr["content"].([]any)
	if len(content) == 0 {
		t.Fatal("AC4: messages[2].content is empty")
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("AC4: messages[2].content[0] is not an object")
	}
	if first["type"] != "tool_result" {
		t.Errorf("AC4: messages[2].content[0].type = %q; want \"tool_result\"", first["type"])
	}
}

// TestMultiTurnToolUseIdRoundTrip verifies AC5.
func TestMultiTurnToolUseIdRoundTrip(t *testing.T) {
	_, reqs := runToolUseWithRequests(t)
	if len(reqs) < 2 {
		t.Fatal("prerequisite — need 2 requests")
	}
	msgs, _ := reqs[1].Body["messages"].([]any)
	if len(msgs) < 3 {
		t.Fatal("AC5: prerequisite — need at least 3 messages")
	}

	// Extract tool_use id from messages[1].content
	asstContent, _ := msgs[1].(map[string]any)["content"].([]any)
	var toolUseID string
	for _, block := range asstContent {
		if b, ok := block.(map[string]any); ok && b["type"] == "tool_use" {
			toolUseID, _ = b["id"].(string)
			break
		}
	}
	if toolUseID == "" {
		t.Fatal("AC5: could not find tool_use id in messages[1]")
	}

	// Extract tool_use_id from messages[2].content[0]
	usrContent, _ := msgs[2].(map[string]any)["content"].([]any)
	if len(usrContent) == 0 {
		t.Fatal("AC5: messages[2].content is empty")
	}
	resultID, _ := usrContent[0].(map[string]any)["tool_use_id"].(string)
	if resultID == "" {
		t.Fatal("AC5: tool_use_id not found in messages[2].content[0]")
	}
	if resultID != toolUseID {
		t.Errorf("AC5: tool_use_id mismatch: messages[1] id=%q, messages[2] tool_use_id=%q", toolUseID, resultID)
	}
}
