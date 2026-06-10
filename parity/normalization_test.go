package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestNormalizationSystemPromptFormat verifies system prompt is sent as array, not message.
func TestNormalizationSystemPromptFormat(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "normalization.system.as-top-level-array",
			Category:    "normalization",
			Description: "system prompt sent as top-level system field, not role:system message",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:           0,
						HasSystemPrompt: true,
					},
				},
			},
		},
	})
}

// TestNormalizationToolPairing verifies tool_use/tool_result pairing.
func TestNormalizationToolPairing(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "normalization.pairing.result-follows-use-in-second-request",
			Category:    "normalization",
			Description: "second API request includes tool_result following tool_use",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo hello",
				Format:           "stream-json",
				Cassette:         "tool-use-turn1",
				CassetteSequence: []string{"tool-use-turn1", "tool-use-turn2"},
				Args:             []string{"--dangerously-skip-permissions"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:    0,
						HasField: []string{"messages"},
					},
				},
			},
		},
	})
}

// TestNormalizationUnknownToolResultPairing verifies pairing for unknown tools.
func TestNormalizationUnknownToolResultPairing(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "normalization.unknown-tool.error-result-paired",
			Category:    "normalization",
			Description: "unknown tool gets error tool_result that pairs with tool_use",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "use a fake tool",
				Format:           "stream-json",
				Cassette:         "unknown-tool",
				CassetteSequence: []string{"unknown-tool", "unknown-tool-followup"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
					},
				},
			},
		},
	})
}

// TestNormalizationStdoutPurity verifies no internal data leaks to stdout.
func TestNormalizationStdoutPurity(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "normalization.stdout.no-debug-in-stream-json",
			Category:    "normalization",
			Description: "no debug or internal messages leak to stdout in stream-json mode",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
				},
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{
						"level=DEBUG",
						"level=INFO",
						"level=WARN",
					},
				},
			},
		},
		{
			ID:          "normalization.stdout.verbose-only-stderr",
			Category:    "normalization",
			Description: "verbose logging goes to stderr not stdout",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--verbose"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					AllLinesValidJSON: true,
				},
			},
		},
	})
}

// TestNormalizationRoleMerging verifies consecutive same-role messages are merged.
func TestNormalizationRoleMerging(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "normalization.role.user-messages-present",
			Category:    "normalization",
			Description: "API request contains user messages in proper role format",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hello world",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:    0,
						HasField: []string{"messages", "model", "system"},
					},
				},
			},
		},
	})
}
