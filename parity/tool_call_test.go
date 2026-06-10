package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestToolCallBasicFlow verifies the basic tool_use → tool_result → end_turn flow.
func TestToolCallBasicFlow(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.basic.bash-echo",
			Category:    "tool-call",
			Description: "bash echo command executes and returns result",
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
						Type:    "result",
						Subtype: "success",
					},
					HasEventTypes: []string{"system", "assistant", "result"},
				},
			},
		},
		{
			ID:          "tool-call.basic.exits-zero-on-success",
			Category:    "tool-call",
			Description: "successful tool call exits 0",
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
			},
		},
	})
}

// TestToolCallUnknownTool verifies unknown tool_use produces error result.
func TestToolCallUnknownTool(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.unknown.immediate-error",
			Category:    "tool-call",
			Description: "unknown tool returns synthetic error without hanging",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "use a nonexistent tool",
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

// TestToolCallTextWithTool verifies assistant messages containing both text and tool_use.
func TestToolCallTextWithTool(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.mixed.text-and-tool-in-one-assistant",
			Category:    "tool-call",
			Description: "assistant turn with text + tool_use emits single assistant event",
			WorkDirFiles: map[string]string{
				"test.txt": "file content for testing\n",
			},
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "check the test file",
				Format:           "stream-json",
				Cassette:         "text-with-tool",
				CassetteSequence: []string{"text-with-tool", "text-with-tool-followup"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"system", "assistant", "result"},
					LastEvent: &harness.EventExpectation{
						Type:    "result",
						Subtype: "success",
					},
				},
			},
		},
	})
}

// TestToolCallMultipleToolsOneTurn verifies multiple tool_use blocks in one turn.
func TestToolCallMultipleToolsOneTurn(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.multi.two-bash-in-one-turn",
			Category:    "tool-call",
			Description: "two tool_use blocks in one turn both execute",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run two commands",
				Format:           "stream-json",
				Cassette:         "multi-tool",
				CassetteSequence: []string{"multi-tool", "multi-tool-followup"},
				Args:             []string{"--dangerously-skip-permissions"},
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
	})
}

// TestToolCallParallelReads verifies read-only tools can run concurrently.
func TestToolCallParallelReads(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.parallel.concurrent-reads",
			Category:    "tool-call",
			Description: "multiple Read tools execute concurrently",
			WorkDirFiles: map[string]string{
				"a.txt": "content of file a\n",
				"b.txt": "content of file b\n",
			},
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "read both files",
				Format:           "stream-json",
				Cassette:         "parallel-reads",
				CassetteSequence: []string{"parallel-reads", "parallel-reads-followup"},
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
	})
}

// TestToolCallMaxIterations verifies --max-iterations caps the loop.
func TestToolCallMaxIterations(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.max-iterations.caps-loop",
			Category:    "tool-call",
			Description: "--max-iterations 1 stops after one iteration with error exit",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo hello",
				Format:           "stream-json",
				Cassette:         "tool-use-turn1",
				CassetteSequence: []string{"tool-use-turn1", "tool-use-turn2"},
				Args:             []string{"--dangerously-skip-permissions", "--max-iterations", "1"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"max iterations"},
				},
			},
		},
	})
}

// TestToolCallEmptyStopReason verifies empty stop_reason treated as end_turn.
func TestToolCallEmptyStopReason(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool-call.stop-reason.empty-treated-as-end-turn",
			Category:    "tool-call",
			Description: "null/empty stop_reason treated as end_turn (no infinite loop)",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say something",
				Format:   "stream-json",
				Cassette: "empty-stop-reason",
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
