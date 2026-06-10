package e2e_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// runToolUse starts the mock server, registers a two-cassette
// "tool-use" sequence (turn 1 returns a Bash tool_use block;
// turn 2 returns the final end_turn text), runs jenny against it, and
// returns the parsed NDJSON lines. It fails the test if jenny exits
// non-zero or emits no parseable JSON.
//
// The cassettes are jenny_test/fixtures/cassettes/tool-use-turn{1,2}.sse;
// the path is given as a path-prefix on ANTHROPIC_BASE_URL and the
// cassette id "tool-use" is registered against the mock via
// SetSequence. The mock extracts the id from the URL and serves the
// next sequence entry on each request.
func runToolUse(t *testing.T) []map[string]any {
	t.Helper()
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	mock.SetSequence("tool-use", []string{"tool-use-turn1", "tool-use-turn2"})
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/tool-use",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		// Pin model to nothing so the binary's built-in default applies
		// (matches the convention used by the other stream-json tests).
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "run echo hello")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	if len(res.Parsed) == 0 {
		t.Fatal("no parseable JSON lines in jenny output")
	}
	return res.Parsed
}

// findEventsByTypeSubtype returns the subset of parsed NDJSON events
// whose "type" matches typ and "subtype" matches subtype. Order is
// preserved. Used by the tool-call conformance tests to slice the
// stream into the specific event categories asserted on below.
func findEventsByTypeSubtype(parsed []map[string]any, typ, subtype string) []map[string]any {
	out := make([]map[string]any, 0, 4)
	for _, ev := range parsed {
		if ev["type"] == typ && ev["subtype"] == subtype {
			out = append(out, ev)
		}
	}
	return out
}

// TestToolCallStartedEvent is AC3: a `tool_call/started` event must be
// emitted in the tool-use two-turn run, with a non-empty `tool_name`,
// a non-empty `tool_use_id`, and a v4 `uuid`.
func TestToolCallStartedEvent(t *testing.T) {
	parsed := runToolUse(t)

	started := findEventsByTypeSubtype(parsed, "tool_call", "started")
	if len(started) == 0 {
		t.Fatal("AC3: no tool_call/started event found in output")
	}
	// The single-tool-per-turn scenario must emit exactly one started
	// event (one tool_use block in the assistant turn).
	if len(started) != 1 {
		t.Errorf("AC3: expected exactly 1 tool_call/started event, got %d", len(started))
	}

	ev := started[0]
	if name, _ := ev["tool_name"].(string); name == "" {
		t.Errorf("AC3: tool_call/started tool_name is empty; got %T (%v)", ev["tool_name"], ev["tool_name"])
	}
	if id, _ := ev["tool_use_id"].(string); id == "" {
		t.Errorf("AC3: tool_call/started tool_use_id is empty; got %T (%v)", ev["tool_use_id"], ev["tool_use_id"])
	}
	if raw, ok := ev["uuid"].(string); !ok || !isValidUUID(raw) {
		t.Errorf("AC3: tool_call/started uuid is missing or not a valid UUID v4; got %T (%v)", ev["uuid"], ev["uuid"])
	}
}

// TestToolCallCompletedEvent is AC4: a `tool_call/completed` event
// must be emitted, with a `tool_use_id` field.
func TestToolCallCompletedEvent(t *testing.T) {
	parsed := runToolUse(t)

	completed := findEventsByTypeSubtype(parsed, "tool_call", "completed")
	if len(completed) == 0 {
		t.Fatal("AC4: no tool_call/completed event found in output")
	}
	if len(completed) != 1 {
		t.Errorf("AC4: expected exactly 1 tool_call/completed event, got %d", len(completed))
	}

	ev := completed[0]
	if id, _ := ev["tool_use_id"].(string); id == "" {
		t.Errorf("AC4: tool_call/completed tool_use_id is empty; got %T (%v)", ev["tool_use_id"], ev["tool_use_id"])
	}
}

// TestToolCallToolUseIdConsistency is AC5: the tool_use_id on the
// `tool_call/completed` event must equal the tool_use_id on the
// `tool_call/started` event. The single-tool scenario yields exactly
// one of each, so the IDs are compared directly.
func TestToolCallToolUseIdConsistency(t *testing.T) {
	parsed := runToolUse(t)

	started := findEventsByTypeSubtype(parsed, "tool_call", "started")
	completed := findEventsByTypeSubtype(parsed, "tool_call", "completed")
	if len(started) == 0 || len(completed) == 0 {
		t.Fatalf("AC5: prerequisite — need both started and completed events; got started=%d completed=%d", len(started), len(completed))
	}
	// Pair by index. The single-tool scenario has 1:1, but iterating
	// the min length is robust to additional completed events emitted
	// for tool_error paths etc. (out of scope here, but cheap to handle).
	n := min(len(completed), len(started))
	for i := range n {
		sID, _ := started[i]["tool_use_id"].(string)
		cID, _ := completed[i]["tool_use_id"].(string)
		if sID == "" || cID == "" {
			t.Errorf("AC5: pair %d has empty tool_use_id (started=%q completed=%q)", i, sID, cID)
			continue
		}
		if sID != cID {
			t.Errorf("AC5: pair %d tool_use_id mismatch: started=%q completed=%q", i, sID, cID)
		}
	}
}

// TestToolCallUserMessageWrapper is AC6: at least one `user` event
// must wrap the tool result, with `message.role == "user"`, a
// non-empty `message.content` array, and a first block of
// `type=tool_result` whose `tool_use_id` matches the corresponding
// `tool_call/started` event's `tool_use_id`.
func TestToolCallUserMessageWrapper(t *testing.T) {
	parsed := runToolUse(t)

	// Collect the started event's tool_use_id to cross-check.
	started := findEventsByTypeSubtype(parsed, "tool_call", "started")
	if len(started) == 0 {
		t.Fatal("AC6: prerequisite — no tool_call/started event found")
	}
	startedID, _ := started[0]["tool_use_id"].(string)
	if startedID == "" {
		t.Fatal("AC6: prerequisite — tool_call/started tool_use_id is empty")
	}

	// Find the first `user` event.
	var user map[string]any
	for _, ev := range parsed {
		if ev["type"] == "user" {
			user = ev
			break
		}
	}
	if user == nil {
		t.Fatal("AC6: no event with type=user found in output")
	}

	rawMsg, ok := user["message"].(map[string]any)
	if !ok {
		t.Fatalf("AC6: user.message is not an object; got %T", user["message"])
	}
	if role, _ := rawMsg["role"].(string); role != "user" {
		t.Errorf("AC6: user.message.role = %q; want \"user\"", role)
	}

	rawContent, ok := rawMsg["content"].([]any)
	if !ok {
		t.Fatalf("AC6: user.message.content is not a JSON array; got %T", rawMsg["content"])
	}
	if len(rawContent) == 0 {
		t.Fatal("AC6: user.message.content is empty")
	}

	first, ok := rawContent[0].(map[string]any)
	if !ok {
		t.Fatalf("AC6: user.message.content[0] is not an object; got %T", rawContent[0])
	}
	if typ, _ := first["type"].(string); typ != "tool_result" {
		t.Errorf("AC6: user.message.content[0].type = %q; want \"tool_result\"", typ)
	}
	if id, _ := first["tool_use_id"].(string); id != startedID {
		t.Errorf("AC6: user.message.content[0].tool_use_id = %q; want %q (matches tool_call/started)", id, startedID)
	}
}

// TestToolCallTwoRequestStarts is AC7: exactly two
// `stream_request_start` events must be emitted in the two-turn run
// (one per /v1/messages API call).
func TestToolCallTwoRequestStarts(t *testing.T) {
	parsed := runToolUse(t)

	count := 0
	for _, ev := range parsed {
		if ev["type"] == "stream_request_start" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("AC7: expected exactly 2 stream_request_start events, got %d", count)
	}
}

// TestToolCallFinalResult is AC8: the last meaningful NDJSON event
// (the terminal one on a successful run) must have
// `type=result, subtype=success`.
func TestToolCallFinalResult(t *testing.T) {
	parsed := runToolUse(t)

	// Walk backward from the last line; NDJSON emission is line-
	// ordered and the terminal `result` is the last line on success,
	// but be defensive in case future changes interleave.
	var last map[string]any
	for i := len(parsed) - 1; i >= 0; i-- {
		if parsed[i] != nil {
			last = parsed[i]
			break
		}
	}
	if last == nil {
		t.Fatal("AC8: no NDJSON events emitted")
	}
	if last["type"] != "result" {
		t.Errorf("AC8: last event type = %q; want \"result\" (event: %v)", last["type"], last)
	}
	if last["subtype"] != "success" {
		t.Errorf("AC8: last event subtype = %q; want \"success\" (event: %v)", last["subtype"], last)
	}
}

// TestMockServerSequenceExhaustion is AC2: a third POST to the same
// cassette URL (after the two-cassette sequence is consumed) receives
// HTTP 400 with a JSON error body. The mock server does not panic or
// block. This is a unit test of the mock server itself, rather than an
// e2e test of jenny.
func TestMockServerSequenceExhaustion(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	mock.SetSequence("exhaust", []string{"echo-hello", "echo-hello"})

	url := mock.URL() + "/cassette/exhaust/v1/messages"
	body := strings.NewReader("{}")

	// Turn 1: OK
	resp1, err := http.DefaultClient.Post(url, "application/json", body)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	if resp1.StatusCode != 200 {
		t.Errorf("request 1: got status %d; want 200", resp1.StatusCode)
	}

	// Turn 2: OK
	body = strings.NewReader("{}")
	resp2, err := http.DefaultClient.Post(url, "application/json", body)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	if resp2.StatusCode != 200 {
		t.Errorf("request 2: got status %d; want 200", resp2.StatusCode)
	}

	// Turn 3: 400 Bad Request
	body = strings.NewReader("{}")
	resp3, err := http.DefaultClient.Post(url, "application/json", body)
	if err != nil {
		t.Fatalf("request 3 failed: %v", err)
	}
	if resp3.StatusCode != 400 {
		t.Errorf("request 3: got status %d; want 400", resp3.StatusCode)
	}

	// Verify error body.
	var errBody map[string]string
	if err := json.NewDecoder(resp3.Body).Decode(&errBody); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}
	if !strings.Contains(errBody["error"], "exhausted after 2 requests") {
		t.Errorf("error body %q does not mention exhaustion", errBody["error"])
	}
}
