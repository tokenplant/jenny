package e2e_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
)

// TestAPIProtocolMaxTokens is the AC9 conformance test: the outbound
// /v1/messages request must carry max_tokens == 64000. A lower default
// causes the agent to truncate long responses, which silently breaks
// real-world tasks. The reference implementation uses 64000; jenny must
// match.
func TestAPIProtocolMaxTokens(t *testing.T) {
	body := captureRequestBody(t)

	const want = 64000
	raw, present := body["max_tokens"]
	if !present {
		t.Fatalf("AC9: max_tokens missing from request body")
	}
	// JSON numbers decode as float64.
	num, ok := raw.(float64)
	if !ok {
		t.Fatalf("AC9: max_tokens is not a number; got %T (%v)", raw, raw)
	}
	if int(num) != want {
		t.Errorf("AC9: max_tokens = %d; want %d", int(num), want)
	}
}

// TestAPIProtocolSystemPrompt is the AC10 + AC11 conformance test: the
// outbound request must have a top-level "system" field whose text is
// at least 500 characters and contains the absolute path of the
// directory from which the jenny subprocess was spawned.
func TestAPIProtocolSystemPrompt(t *testing.T) {
	body := captureRequestBody(t)

	raw, present := body["system"]
	if !present {
		t.Fatalf("AC10: system field missing from request body")
	}

	text, err := extractSystemText(raw)
	if err != nil {
		t.Fatalf("AC10: %v", err)
	}

	// AC10: substantial system prompt.
	if len(text) < 500 {
		t.Errorf("AC10: system prompt length = %d; want >= 500", len(text))
	}

	// AC11: contains the working directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("AC11: getwd: %v", err)
	}
	if !strings.Contains(text, cwd) {
		t.Errorf("AC11: system prompt does not contain cwd %q (length=%d)", cwd, len(text))
	}
}

// TestAPIProtocolToolsArray is the AC12 + AC13 + AC14 conformance test:
// the outbound request must have a non-empty "tools" array; every
// element must be a JSON object with a non-empty name and, when
// description / input_schema fields are present, they must be
// well-formed; and the array must include tools named "Bash" and
// "Read" (matching the reference implementation capitalization).
func TestAPIProtocolToolsArray(t *testing.T) {
	body := captureRequestBody(t)

	raw, present := body["tools"]
	if !present {
		t.Fatalf("AC12: tools field missing from request body")
	}
	tools, ok := raw.([]any)
	if !ok {
		t.Fatalf("AC12: tools is not a JSON array; got %T", raw)
	}
	if len(tools) == 0 {
		t.Fatalf("AC12: tools array is empty")
	}

	names := make(map[string]struct{}, len(tools))
	for i, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("AC12/AC13: tools[%d] is not a JSON object; got %T", i, raw)
			continue
		}

		// AC13: name is always required and must be a non-empty string.
		name, _ := tool["name"].(string)
		if name == "" {
			t.Errorf("AC13: tools[%d].name is empty or missing", i)
		} else {
			names[name] = struct{}{}
		}

		// AC13: description. Anthropic's server-side tools (e.g. the
		// official web_search_20250305 tool) use a different shape that
		// does not carry a description field; tolerate its absence.
		if rawDesc, ok := tool["description"]; ok {
			desc, _ := rawDesc.(string)
			if desc == "" {
				t.Errorf("AC13: tools[%d] (name=%q).description is empty", i, name)
			}
		}

		// AC13: input_schema, if present, is a JSON object with
		// type "object". Same server-side-tool exception applies: the
		// web_search tool has no input_schema field at the top level.
		if rawSchema, ok := tool["input_schema"]; ok {
			schema, ok := rawSchema.(map[string]any)
			if !ok {
				t.Errorf("AC13: tools[%d] (name=%q).input_schema is not a JSON object; got %T", i, name, rawSchema)
			} else if schemaType, _ := schema["type"].(string); schemaType != "object" {
				t.Errorf("AC13: tools[%d] (name=%q).input_schema.type = %q; want \"object\"", i, name, schemaType)
			}
		}
	}

	// AC14: core tools must be present by name.
	for _, want := range []string{"Bash", "Read"} {
		if _, ok := names[want]; !ok {
			t.Errorf("AC14: tools array missing required tool %q (have: %v)", want, sortedKeys(names))
		}
	}
}

// captureRequestBody runs a single jenny invocation against the
// echo-hello cassette and returns the JSON-decoded body of the captured
// /v1/messages request. It fails the test if zero or more than one
// request was captured.
func captureRequestBody(t *testing.T) map[string]any {
	t.Helper()

	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		// Pin model to nothing so the binary's built-in default applies
		// (matches the existing stream_json_test.go convention).
		"ANTHROPIC_MODEL=",
	}

	_ = harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "echo hello")

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatalf("no /v1/messages request was captured by the mock server")
	}
	// Use the first request — jenny may issue follow-up requests in some
	// scenarios, but the conformance properties (max_tokens, system,
	// tools) are stable across the session.
	return reqs[0].Body
}

// extractSystemText returns the concatenated text of a "system" field.
// The field can be either a plain string or an array of content blocks
// (as used by structured / multi-part system prompts); in the array
// case, only blocks with type "text" contribute.
func extractSystemText(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case []any:
		var b strings.Builder
		for _, block := range v {
			m, ok := block.(map[string]any)
			if !ok {
				return "", fmt.Errorf("system block is not an object: %T", block)
			}
			// Skip non-text blocks (e.g. tool_result references) silently.
			if t, _ := m["type"].(string); t != "" && t != "text" {
				continue
			}
			text, _ := m["text"].(string)
			b.WriteString(text)
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("system field has unexpected type: %T", raw)
	}
}

// sortedKeys returns the keys of m in lexicographic order. Used only
// for error messages.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Inline bubble sort to keep the helper dependency-free.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
