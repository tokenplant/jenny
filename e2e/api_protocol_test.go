package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestAPIMaxTokens verifies max_tokens in outbound API requests.
func TestAPIMaxTokens(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.max-tokens.default",
			Category:    "api-protocol",
			Description: "default max_tokens is 32000",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:     0,
						MaxTokens: 32000,
					},
				},
			},
		},
	})
}

// TestAPISystemPromptPlacement verifies system prompt is a top-level field.
func TestAPISystemPromptPlacement(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.system-prompt.top-level-field",
			Category:    "api-protocol",
			Description: "system prompt sent as top-level 'system' array, not as a message",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:           0,
						HasSystemPrompt: true,
						HasField:        []string{"system"},
					},
				},
			},
		},
	})
}

// TestAPIToolDefinitions verifies tool schemas in API requests.
func TestAPIToolDefinitions(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.tools.present-in-request",
			Category:    "api-protocol",
			Description: "API request includes tool definitions array",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							MinCount:      3,
							EachHasFields: []string{"name", "input_schema"},
						},
					},
				},
			},
		},
		{
			ID:          "api.tools.has-core-tools",
			Category:    "api-protocol",
			Description: "core tools (Read, Bash) present in both jenny and claude",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							HasTool: []string{"Read", "Bash"},
						},
					},
				},
			},
		},
		{
			ID:          "api.tools.each-has-description",
			Category:    "api-protocol",
			Description: "every tool has a description field",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index: 0,
						Tools: &harness.ToolsExpectation{
							EachHasFields: []string{"name", "description", "input_schema"},
						},
					},
				},
			},
		},
	})
}

// TestAPIModelInRequest verifies model field in API requests.
func TestAPIModelInRequest(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.model.present-in-request",
			Category:    "api-protocol",
			Description: "API request includes model field",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Index:    0,
						HasField: []string{"model"},
					},
				},
			},
		},
	})
}

// TestAPIMessagesFormat verifies message format in API requests.
func TestAPIMessagesFormat(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.messages.user-prompt-included",
			Category:    "api-protocol",
			Description: "user prompt appears in messages array",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
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

// TestAPIToolResultPairing verifies tool_use and tool_result pairing in multi-turn.
func TestAPIToolResultPairing(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.tool-pairing.second-request-has-tool-result",
			Category:    "api-protocol",
			Description: "second API request contains tool_result matching prior tool_use",
			Tags:        []string{"api"},
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
						HasField: []string{"messages", "tools", "model"},
					},
				},
			},
		},
	})
}

// TestAPISystemPromptContent verifies system prompt is present and non-empty.
// Note: system prompt content differs between jenny and claude, so we only
// verify structure, not content.
func TestAPISystemPromptContent(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "api.system-prompt.non-empty",
			Category:    "api-protocol",
			Description: "system prompt is present and non-empty",
			Tags:        []string{"api"},
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hello",
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
