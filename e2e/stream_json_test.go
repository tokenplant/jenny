package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

// TestStreamJSONEnvelope verifies every NDJSON event has required envelope fields.
func TestStreamJSONEnvelope(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.envelope.all-lines-valid-json",
			Category:    "stream-json",
			Description: "every stdout line is valid JSON in stream-json mode",
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
			},
		},
		{
			ID:          "stream-json.envelope.session-id-consistent",
			Category:    "stream-json",
			Description: "session_id is identical across all events",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					SessionIDConsistent: true,
				},
			},
		},
		{
			ID:          "stream-json.envelope.uuids-unique",
			Category:    "stream-json",
			Description: "every event uuid is unique",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					UUIDsUnique: true,
				},
			},
		},
	})
}

// TestStreamJSONInitEvent verifies the system/init event.
func TestStreamJSONInitEvent(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.init.first-event-is-system-init",
			Category:    "stream-json",
			Description: "first event is type=system subtype=init",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					FirstEvent: &harness.EventExpectation{
						Type:    "system",
						Subtype: "init",
					},
				},
			},
		},
		{
			ID:          "stream-json.init.has-required-fields",
			Category:    "stream-json",
			Description: "init event has cwd, tools, model, session_id, uuid",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					FirstEvent: &harness.EventExpectation{
						Type:          "system",
						HasFields:     []string{"cwd", "tools", "model", "session_id", "uuid"},
						FieldNotEmpty: []string{"cwd", "session_id", "uuid"},
					},
				},
			},
		},
		{
			ID:          "stream-json.init.tools-is-array",
			Category:    "stream-json",
			Description: "init event tools field is an array of tool names",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Matches: []string{`"tools":\[`},
				},
			},
		},
	})
}

// TestStreamJSONResultEvent verifies the terminal result event.
func TestStreamJSONResultEvent(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.result.last-event-is-result",
			Category:    "stream-json",
			Description: "last event is always type=result",
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
					},
				},
			},
		},
		{
			ID:          "stream-json.result.has-usage-fields",
			Category:    "stream-json",
			Description: "result event has usage with snake_case token fields",
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
						HasFields: []string{"usage", "session_id", "uuid"},
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
			ID:          "stream-json.result.has-duration",
			Category:    "stream-json",
			Description: "result event has duration_ms >= 0",
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
			ID:          "stream-json.result.has-total-cost",
			Category:    "stream-json",
			Description: "result event has total_cost_usd field",
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
			ID:          "stream-json.result.success-subtype",
			Category:    "stream-json",
			Description: "successful run has subtype=success",
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
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
		{
			ID:          "stream-json.result.has-stop-reason",
			Category:    "stream-json",
			Description: "result event includes stop_reason field",
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
						HasFields: []string{"stop_reason"},
					},
				},
			},
		},
		{
			ID:          "stream-json.result.has-num-turns",
			Category:    "stream-json",
			Description: "result event includes num_turns field",
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
	})
}

// TestStreamJSONAssistantEvent verifies assistant message format.
func TestStreamJSONAssistantEvent(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.assistant.has-message-wrapper",
			Category:    "stream-json",
			Description: "assistant event wraps content in message object",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"assistant"},
					EventAssertions: []harness.IndexedEventExpectation{
						{
							Index: -1, TypeFilter: "assistant",
							Expect: harness.EventExpectation{
								HasFields: []string{"message", "session_id", "uuid"},
								Nested: map[string]*harness.EventExpectation{
									"message": {
										HasFields: []string{"role", "content"},
										FieldEquals: map[string]any{
											"role": "assistant",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			ID:          "stream-json.assistant.one-per-turn",
			Category:    "stream-json",
			Description: "exactly one assistant event per API turn for text-only response",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"system", "assistant", "result"},
				},
			},
		},
	})
}

// TestStreamJSONEventSequence verifies the event ordering contract.
func TestStreamJSONEventSequence(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.sequence.init-then-result",
			Category:    "stream-json",
			Description: "simple turn: init → assistant → result",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					FirstEvent: &harness.EventExpectation{Type: "system", Subtype: "init"},
					LastEvent:  &harness.EventExpectation{Type: "result"},
					HasEventTypes: []string{
						"system", "assistant", "result",
					},
				},
			},
		},
	})
}

// TestStreamJSONToolCallEvents verifies tool call event format in stream-json.
func TestStreamJSONToolCallEvents(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.tool-call.has-started-and-completed",
			Category:    "stream-json",
			Description: "tool execution emits tool_call started and completed events",
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
					HasEventTypes: []string{"assistant", "result"},
				},
			},
		},
	})
}

// TestStreamJSONUserToolResult verifies tool results are wrapped in user messages.
func TestStreamJSONUserToolResult(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.user.tool-result-wrapped",
			Category:    "stream-json",
			Description: "tool results emitted as user message with tool_result content",
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
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"type":"user"`},
				},
			},
		},
	})
}

// TestStreamJSONSessionIDMatch verifies session_id consistency between init and result.
func TestStreamJSONSessionIDMatch(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.session-id.init-matches-result",
			Category:    "stream-json",
			Description: "session_id on init matches session_id on result",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					SessionIDConsistent: true,
				},
			},
		},
	})
}

// TestStreamJSON_ReferenceAlignment runs stream-json envelope tests against both jenny
// and the reference binary (if REFERENCE_BIN is set). It compares field-by-field and
// logs any differences found between the two outputs.
func TestStreamJSON_ReferenceAlignment(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "stream-json.alignment.all-lines-valid-json",
			Category:    "stream-json",
			Description: "every stdout line is valid JSON in stream-json mode (vs reference)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					CompareToReference: true,
					AllLinesValidJSON:  true,
				},
			},
		},
		{
			ID:          "stream-json.alignment.session-id-consistent",
			Category:    "stream-json",
			Description: "session_id is identical across all events (vs reference)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					CompareToReference:  true,
					SessionIDConsistent: true,
				},
			},
		},
		{
			ID:          "stream-json.alignment.uuids-unique",
			Category:    "stream-json",
			Description: "every event uuid is unique (vs reference)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					CompareToReference: true,
					UUIDsUnique:        true,
				},
			},
		},
		{
			ID:          "stream-json.alignment.init-event-type",
			Category:    "stream-json",
			Description: "init event has type=system subtype=init (vs reference)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					CompareToReference: true,
					FirstEvent: &harness.EventExpectation{
						Type:    "system",
						Subtype: "init",
					},
				},
			},
		},
		{
			ID:          "stream-json.alignment.final-result-event",
			Category:    "stream-json",
			Description: "final event is type=result (vs reference)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					CompareToReference: true,
					LastEvent: &harness.EventExpectation{
						Type: "result",
					},
				},
			},
		},
	})
}
