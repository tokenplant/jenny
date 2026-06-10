package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

func isValidUUID(s string) bool { return harness.IsValidUUID(s) }

// runEchoHello starts the mock server, runs jenny with the echo-hello
// cassette, and returns the parsed NDJSON lines. It is the shared
// setup for all stream_json_format tests. The function fails the test
// if jenny exits non-zero or emits no parseable JSON.
func runEchoHello(t *testing.T) []map[string]any {
	t.Helper()
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		// Pin model to nothing so the binary's built-in default applies
		// (matches the existing stream_json_test.go and api_protocol_test.go
		// convention).
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "echo hello")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	if len(res.Parsed) == 0 {
		t.Fatal("no parseable JSON lines in jenny output")
	}
	return res.Parsed
}

// TestStreamJsonAllEventsHaveType is AC1: every JSON object emitted on
// stdout in stream-json mode must have a non-empty "type" field.
func TestStreamJsonAllEventsHaveType(t *testing.T) {
	parsed := runEchoHello(t)
	for i, ev := range parsed {
		raw, present := ev["type"]
		if !present {
			t.Errorf("AC1: line %d missing \"type\" field: %v", i, ev)
			continue
		}
		s, ok := raw.(string)
		if !ok {
			t.Errorf("AC1: line %d \"type\" is not a string; got %T", i, raw)
			continue
		}
		if s == "" {
			t.Errorf("AC1: line %d has empty \"type\"", i)
		}
	}
}

// TestStreamJsonUUIDFormat is AC2: every event that carries a top-level
// "uuid" must use a valid UUID v4 string.
func TestStreamJsonUUIDFormat(t *testing.T) {
	parsed := runEchoHello(t)
	for i, ev := range parsed {
		raw, present := ev["uuid"]
		if !present {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			t.Errorf("AC2: line %d \"uuid\" is not a string; got %T (%v)", i, raw, raw)
			continue
		}
		if !isValidUUID(s) {
			t.Errorf("AC2: line %d \"uuid\" = %q is not a valid UUID v4", i, s)
		}
	}
}

// TestStreamJsonSystemInitFields covers AC3 and AC4: there is at least
// one `type=system, subtype=init` event, its envelope fields
// (session_id, uuid, tools) are present and well-formed, and the
// `tools` field is a non-empty string array containing "Bash" and
// "Read".
func TestStreamJsonSystemInitFields(t *testing.T) {
	parsed := runEchoHello(t)

	// Find the first system/init event.
	var init map[string]any
	for _, ev := range parsed {
		if ev["type"] == "system" && ev["subtype"] == "init" {
			init = ev
			break
		}
	}
	if init == nil {
		t.Fatal("AC3: no event with type=system, subtype=init found")
	}

	// AC3: session_id is a non-empty string.
	sid, ok := init["session_id"].(string)
	if !ok || sid == "" {
		t.Errorf("AC3: system/init session_id is missing or empty; got %T (%v)", init["session_id"], init["session_id"])
	}

	// AC3: uuid is a non-empty v4 string.
	rawUUID, ok := init["uuid"]
	if !ok {
		t.Errorf("AC3: system/init uuid is missing")
	} else if uuid, ok := rawUUID.(string); !ok || uuid == "" {
		t.Errorf("AC3: system/init uuid is not a non-empty string; got %T (%v)", rawUUID, rawUUID)
	} else if !isValidUUID(uuid) {
		t.Errorf("AC3: system/init uuid %q is not a valid UUID v4", uuid)
	}

	// AC3: tools is present.
	if _, ok := init["tools"]; !ok {
		t.Errorf("AC3: system/init tools is missing")
	}

	// AC4: tools is a non-empty []any of non-empty strings, including "Bash" and "Read".
	tools, ok := init["tools"].([]any)
	if !ok {
		t.Fatalf("AC4: system/init tools is not a JSON array; got %T", init["tools"])
	}
	if len(tools) == 0 {
		t.Fatal("AC4: system/init tools array is empty")
	}
	names := make(map[string]struct{}, len(tools))
	for i, raw := range tools {
		name, ok := raw.(string)
		if !ok {
			t.Errorf("AC4: system/init tools[%d] is not a string; got %T", i, raw)
			continue
		}
		if name == "" {
			t.Errorf("AC4: system/init tools[%d] is an empty string", i)
			continue
		}
		names[name] = struct{}{}
	}
	for _, want := range []string{"Bash", "Read"} {
		if _, ok := names[want]; !ok {
			t.Errorf("AC4: system/init tools missing required entry %q (have: %v)", want, names)
		}
	}
}

// TestStreamJsonResultSuccessFields is AC5: there is at least one
// `type=result, subtype=success` event, and it has all required
// content fields with the right types.
func TestStreamJsonResultSuccessFields(t *testing.T) {
	parsed := runEchoHello(t)

	var result map[string]any
	for _, ev := range parsed {
		if ev["type"] == "result" && ev["subtype"] == "success" {
			result = ev
			break
		}
	}
	if result == nil {
		t.Fatal("AC5: no event with type=result, subtype=success found")
	}

	// result: string (empty allowed).
	if _, ok := result["result"].(string); !ok {
		t.Errorf("AC5: result/success \"result\" is not a string; got %T (%v)", result["result"], result["result"])
	}

	// stop_reason: non-empty string.
	sr, ok := result["stop_reason"].(string)
	if !ok || sr == "" {
		t.Errorf("AC5: result/success stop_reason is missing or empty; got %T (%v)", result["stop_reason"], result["stop_reason"])
	}

	// duration_ms: JSON number >= 0.
	rawDur, present := result["duration_ms"]
	if !present {
		t.Errorf("AC5: result/success duration_ms is missing")
	} else if d, ok := rawDur.(float64); !ok {
		t.Errorf("AC5: result/success duration_ms is not a number; got %T (%v)", rawDur, rawDur)
	} else if d < 0 {
		t.Errorf("AC5: result/success duration_ms = %v; want >= 0", d)
	}

	// session_id: non-empty string.
	sid, ok := result["session_id"].(string)
	if !ok || sid == "" {
		t.Errorf("AC5: result/success session_id is missing or empty; got %T (%v)", result["session_id"], result["session_id"])
	}

	// uuid: valid UUID v4.
	rawUUID, ok := result["uuid"]
	if !ok {
		t.Errorf("AC5: result/success uuid is missing")
	} else if uuid, ok := rawUUID.(string); !ok || uuid == "" {
		t.Errorf("AC5: result/success uuid is not a non-empty string; got %T (%v)", rawUUID, rawUUID)
	} else if !isValidUUID(uuid) {
		t.Errorf("AC5: result/success uuid %q is not a valid UUID v4", uuid)
	}
}

// TestStreamJsonSessionIdConsistency is AC6: all events in a single
// run that carry a "session_id" must share the same value. Exactly one
// distinct session_id is allowed per run.
func TestStreamJsonSessionIdConsistency(t *testing.T) {
	parsed := runEchoHello(t)

	seen := make(map[string]struct{})
	for i, ev := range parsed {
		raw, present := ev["session_id"]
		if !present {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			t.Errorf("AC6: line %d session_id is not a string; got %T", i, raw)
			continue
		}
		seen[s] = struct{}{}
	}
	if len(seen) == 0 {
		t.Error("AC6: no event carried a session_id")
	}
	if len(seen) > 1 {
		t.Errorf("AC6: expected exactly 1 distinct session_id, got %d: %v", len(seen), seen)
	}
}

// TestStreamJsonSystemIsFirst is AC7: the first JSON-parseable line
// emitted on stdout must be a system event.
func TestStreamJsonSystemIsFirst(t *testing.T) {
	parsed := runEchoHello(t)
	first := parsed[0]
	typ, _ := first["type"].(string)
	if typ != "system" {
		t.Errorf("AC7: first event type = %q; want \"system\" (event: %v)", typ, first)
	}
}

func TestStreamJsonResultUsageAndCost(t *testing.T) {
	parsed := runEchoHello(t)

	var result map[string]any
	for _, ev := range parsed {
		if ev["type"] == "result" && ev["subtype"] == "success" {
			result = ev
			break
		}
	}
	if result == nil {
		t.Fatal("no result/success event found")
	}

	// AC1: usage is a JSON object.
	rawUsage, ok := result["usage"]
	if !ok {
		t.Fatal("AC1: result event missing 'usage' field")
	}
	usage, ok := rawUsage.(map[string]any)
	if !ok {
		t.Fatalf("AC1: result.usage is %T, want object", rawUsage)
	}

	// AC2: input_tokens is a non-negative integer.
	rawIn, ok := usage["input_tokens"]
	if !ok {
		t.Error("AC2: usage missing 'input_tokens'")
	} else if in, ok := rawIn.(float64); !ok || in < 0 {
		t.Errorf("AC2: usage.input_tokens=%v, want non-negative number", rawIn)
	} else if in != 100 {
		t.Errorf("AC2: usage.input_tokens=%v, want 100 (echo-hello cassette)", in)
	}

	// AC3: output_tokens is a non-negative integer.
	rawOut, ok := usage["output_tokens"]
	if !ok {
		t.Error("AC3: usage missing 'output_tokens'")
	} else if out, ok := rawOut.(float64); !ok || out < 0 {
		t.Errorf("AC3: usage.output_tokens=%v, want non-negative number", rawOut)
	} else if out != 5 {
		t.Errorf("AC3: usage.output_tokens=%v, want 5 (echo-hello cassette)", out)
	}

	// AC4: total_cost_usd is present and non-negative.
	rawCost, ok := result["total_cost_usd"]
	if !ok {
		t.Error("AC4: result event missing 'total_cost_usd' field")
	} else if cost, ok := rawCost.(float64); !ok {
		t.Errorf("AC4: total_cost_usd is %T, want float", rawCost)
	} else if cost < 0 {
		t.Errorf("AC4: total_cost_usd=%v, want >= 0", cost)
	}
}
