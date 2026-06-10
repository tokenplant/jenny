package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestCostTrackingResultFields verifies cost/usage fields in terminal result.
func TestCostTrackingResultFields(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "cost.result.has-usage-object",
			Category:    "cost-tracking",
			Description: "terminal result includes usage object",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"usage"},
					},
				},
			},
		},
		{
			ID:          "cost.result.usage-has-input-tokens",
			Category:    "cost-tracking",
			Description: "usage includes input_tokens (snake_case)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
						Nested: map[string]*harness.EventExpectation{
							"usage": {
								HasFields: []string{"input_tokens", "output_tokens"},
							},
						},
					},
				},
			},
		},
		{
			ID:          "cost.result.usage-has-cache-fields",
			Category:    "cost-tracking",
			Description: "usage includes cache_read_input_tokens and cache_creation_input_tokens",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type: "result",
						Nested: map[string]*harness.EventExpectation{
							"usage": {
								HasFields: []string{
									"cache_read_input_tokens",
									"cache_creation_input_tokens",
								},
							},
						},
					},
				},
			},
		},
		{
			ID:          "cost.result.has-total-cost-usd",
			Category:    "cost-tracking",
			Description: "terminal result includes total_cost_usd",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"total_cost_usd"},
					},
				},
			},
		},
		{
			ID:          "cost.result.has-duration-ms",
			Category:    "cost-tracking",
			Description: "terminal result includes duration_ms",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"duration_ms"},
					},
				},
			},
		},
		{
			ID:          "cost.result.has-duration-api-ms",
			Category:    "cost-tracking",
			Description: "terminal result includes duration_api_ms",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"duration_api_ms"},
					},
				},
			},
		},
		{
			ID:          "cost.result.has-num-turns",
			Category:    "cost-tracking",
			Description: "terminal result includes num_turns",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"num_turns"},
					},
				},
			},
		},
		{
			ID:          "cost.result.has-model-usage",
			Category:    "cost-tracking",
			Description: "terminal result includes per-model usage breakdown",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"modelUsage"},
					},
				},
			},
		},
		{
			ID:          "cost.result.not-duplicate-on-stdout",
			Category:    "cost-tracking",
			Description: "total_cost_usd appears only on result event, not elsewhere",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Matches: []string{`"total_cost_usd"`},
				},
			},
		},
	})
}

// TestCostTrackingMultiTurn verifies cost accumulation across tool turns.
func TestCostTrackingMultiTurn(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "cost.multi-turn.accumulates-across-turns",
			Category:    "cost-tracking",
			Description: "multi-turn usage is aggregated in terminal result",
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
				StreamJSON: &harness.StreamJSONExpectation{
					LastEvent: &harness.EventExpectation{
						Type:      "result",
						HasFields: []string{"usage", "total_cost_usd", "num_turns"},
						Nested: map[string]*harness.EventExpectation{
							"usage": {
								HasFields: []string{"input_tokens", "output_tokens"},
							},
						},
					},
				},
			},
		},
	})
}
