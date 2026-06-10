package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestSessionPersistence verifies transcript persistence behavior.
func TestSessionPersistence(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "session.persistence.session-id-in-output",
			Category:    "session",
			Description: "stream-json output includes stable session_id",
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
					FirstEvent: &harness.EventExpectation{
						FieldNotEmpty: []string{"session_id"},
					},
				},
			},
		},
		{
			ID:          "session.persistence.session-id-is-uuid",
			Category:    "session",
			Description: "session_id is a lowercase UUID v4",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Matches: []string{`"session_id":"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"`},
				},
			},
		},
	})
}

// TestSessionNoSessionPersistence verifies --no-session-persistence behavior.
func TestSessionNoSessionPersistence(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "session.no-persistence.still-emits-session-id",
			Category:    "session",
			Description: "--no-session-persistence still emits session_id in output",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--no-session-persistence"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					FirstEvent: &harness.EventExpectation{
						FieldNotEmpty: []string{"session_id"},
					},
				},
			},
		},
		{
			ID:          "session.no-persistence.runs-successfully",
			Category:    "session",
			Description: "--no-session-persistence completes without error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--no-session-persistence"},
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

// TestSessionResumeErrors verifies error handling for resume operations.
func TestSessionResumeErrors(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "session.resume.invalid-session-id",
			Category:    "session",
			Description: "resuming with invalid session ID exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"-r", "../../etc/passwd", "-p", "hello"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
			},
		},
		{
			ID:          "session.resume.nonexistent-id",
			Category:    "session",
			Description: "resuming nonexistent session exits nonzero",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"-r", "00000000-0000-0000-0000-000000000000", "-p", "hello"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"session", "not found"},
				},
			},
		},
	})
}

// TestSessionForkErrors verifies --fork-session error handling.
func TestSessionForkErrors(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "session.fork.requires-resume",
			Category:    "session",
			Description: "--fork-session without -r exits with error",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--fork-session", "-p", "hi"},
				Env:  []string{"ANTHROPIC_AUTH_TOKEN=dummy"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 1,
				Stderr: &harness.StderrExpectation{
					Contains: []string{"fork", "resume", "requires"},
				},
			},
		},
	})
}
