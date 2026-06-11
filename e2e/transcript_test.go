package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

func TestTranscriptCreation(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "transcript.creation.file-created",
			Category:    "transcript",
			Description: "transcript file is created with one JSONL file in the directory",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_TRANSCRIPT_DIR=${WORK_DIR}",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				FileSystem: []harness.FileSystemExpectation{
					{
						Pattern:       "*.jsonl",
						ExpectedCount: 1,
					},
				},
			},
		},
	})
}

func TestTranscriptEntriesValid(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "transcript.entries.valid-json",
			Category:    "transcript",
			Description: "all transcript lines are valid JSON and have a type",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_TRANSCRIPT_DIR=${WORK_DIR}",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				FileSystem: []harness.FileSystemExpectation{
					{
						Pattern: "*.jsonl",
						JSONL: &harness.JSONLExpectation{
							AllLinesValidJSON: true,
							RequiredFields:    []string{"type"},
						},
					},
				},
			},
		},
	})
}

func TestTranscriptSessionIDIsUUID(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "transcript.session-id.filename-is-uuid",
			Category:    "transcript",
			Description: "transcript filename is a valid UUID",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_TRANSCRIPT_DIR=${WORK_DIR}",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				// A bit hard to check filename is UUID purely declaratively without custom code,
				// but we added SessionIDMatchesStdout which implicitly checks the stem against the UUID session_id from stdout.
				FileSystem: []harness.FileSystemExpectation{
					{
						Pattern: "*.jsonl",
						JSONL: &harness.JSONLExpectation{
							SessionIDMatchesStdout: true,
						},
					},
				},
			},
		},
	})
}

func TestTranscriptNoSessionPersistence(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "transcript.persistence.none-created",
			Category:    "transcript",
			Description: "--no-session-persistence suppresses transcript creation",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Args:     []string{"--no-session-persistence"},
				Env: []string{
					"JENNY_TRANSCRIPT_DIR=${WORK_DIR}",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				FileSystem: []harness.FileSystemExpectation{
					{
						Pattern:       "*.jsonl",
						ExpectedCount: 0,
						MustNotExist:  true,
					},
				},
			},
		},
	})
}

func TestTranscriptEntriesHaveUUID(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "transcript.entries.have-uuid",
			Category:    "transcript",
			Description: "every transcript entry has a valid uuid v4",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "say hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"JENNY_TRANSCRIPT_DIR=${WORK_DIR}",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				FileSystem: []harness.FileSystemExpectation{
					{
						Pattern: "*.jsonl",
						JSONL: &harness.JSONLExpectation{
							AllLinesHaveUUID: true,
						},
					},
				},
			},
		},
	})
}

// TestTranscriptResume verifies that resuming a session logs to the same file
func TestTranscriptResume(t *testing.T) {
	// For resume and continue, we need sequential execution of the CLI.
	// Currently harness TargetInvocation runs exactly one command.
	// Multi-invocation tests (like resume) might still need to be partly imperative or
	// we would need a Sequence of Invocations in the declarative harness.
	// Since we are migrating them to declarative, we should consider if we can chain them.
	// We can leave this imperative, OR we can just write an imperative wrapper that calls RunJenny twice.

	// Let's implement an imperative wrapper since extending harness for multi-CLI execution
	// is beyond simple modifications.
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}

	workDir := t.TempDir()
	envWithDir := append(env, "JENNY_TRANSCRIPT_DIR="+workDir)

	// Run 1
	res1 := harness.RunJennyInDir(t, workDir, envWithDir, "--output-format", "stream-json", "-p", "hi")
	if res1.ExitCode != 0 {
		t.Fatalf("first run exited %d", res1.ExitCode)
	}

	// Extract session ID to resume
	var sid string
	for _, m := range res1.Parsed {
		if id, ok := m["session_id"].(string); ok && id != "" {
			sid = id
			break
		}
	}
	if sid == "" {
		t.Fatal("no session_id found in first run")
	}

	// Run 2: Resume
	mock.ClearRequests()
	res2 := harness.RunJennyInDir(t, workDir, envWithDir, "--output-format", "stream-json", "-r", sid, "-p", "hi again")
	if res2.ExitCode != 0 {
		t.Fatalf("second run exited %d", res2.ExitCode)
	}

	// Assert using the new declarative filesystem check
	cmp := harness.Compare(&harness.TestCase{
		Expected: harness.ExpectedBehavior{
			FileSystem: []harness.FileSystemExpectation{
				{
					Pattern:       "*.jsonl",
					ExpectedCount: 1, // still exactly 1 file
					JSONL: &harness.JSONLExpectation{
						MinCount: 3, // Initial (system, result) + Resume (system, result) - should be more than a single run
					},
				},
			},
		},
	}, &harness.CapturedOutput{Stdout: res2.Stdout}, workDir)

	if !cmp.Pass {
		t.Fatalf("transcript resume checks failed: %+v", cmp.Diff)
	}
}

// TestTranscriptContinue verifies that the --continue flag resumes the latest session.
func TestTranscriptContinue(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}

	workDir := t.TempDir()
	envWithDir := append(env, "JENNY_TRANSCRIPT_DIR="+workDir)

	res1 := harness.RunJennyInDir(t, workDir, envWithDir, "--output-format", "stream-json", "-p", "hi")
	if res1.ExitCode != 0 {
		t.Fatalf("first run exited %d", res1.ExitCode)
	}

	mock.ClearRequests()
	res2 := harness.RunJennyInDir(t, workDir, envWithDir, "--output-format", "stream-json", "--continue", "-p", "continue prompt")
	if res2.ExitCode != 0 {
		t.Fatalf("second run exited %d", res2.ExitCode)
	}

	// Assert
	cmp := harness.Compare(&harness.TestCase{
		Expected: harness.ExpectedBehavior{
			FileSystem: []harness.FileSystemExpectation{
				{
					Pattern:       "*.jsonl",
					ExpectedCount: 1, // still exactly 1 file
					JSONL: &harness.JSONLExpectation{
						MinCount: 3,
					},
				},
			},
		},
	}, &harness.CapturedOutput{Stdout: res2.Stdout}, workDir)

	if !cmp.Pass {
		t.Fatalf("transcript continue checks failed: %+v", cmp.Diff)
	}
}
