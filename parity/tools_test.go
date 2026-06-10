package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestToolRead verifies the Read tool behavior end-to-end.
func TestToolRead(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.read.existing-file",
			Category:    "tools",
			Description: "Read tool returns line-numbered content for an existing file",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "read test.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"read-file", "tool-use-turn2"},
				Cassette:         "read-file",
			},
			WorkDirFiles: map[string]string{
				"test.txt": "line one\nline two\nline three\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"tool_call"},
				},
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"line one", "line two"},
				},
			},
		},
		{
			ID:          "tool.read.missing-file",
			Category:    "tools",
			Description: "Read tool returns warning (not error) for missing file",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "read nonexistent.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"read-missing", "tool-use-turn2"},
				Cassette:         "read-missing",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"does not exist"},
				},
			},
		},
		{
			ID:          "tool.read.tool-name-in-init",
			Category:    "tools",
			Description: "Read tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
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
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"Read"`},
				},
			},
		},
	})
}

// TestToolWrite verifies the write tool behavior.
func TestToolWrite(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.write.no-prior-read",
			Category:    "tools",
			Description: "write tool rejects when no prior Read was performed",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "write to out.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"write-no-read", "tool-use-turn2"},
				Cassette:         "write-no-read",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"read", "Read", "reading first"},
				},
			},
		},
		{
			ID:          "tool.write.after-read-success",
			Category:    "tools",
			Description: "write tool succeeds after Read and produces diff",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "update target.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"read-then-write", "write-after-read", "tool-use-turn2"},
				Cassette:         "read-then-write",
			},
			WorkDirFiles: map[string]string{
				"target.txt": "original content\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"---", "+++", "diff"},
				},
			},
		},
		{
			ID:          "tool.write.tool-in-init",
			Category:    "tools",
			Description: "write tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"write"`},
				},
			},
		},
	})
}

// TestToolEdit verifies the edit tool behavior.
func TestToolEdit(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.edit.no-prior-read",
			Category:    "tools",
			Description: "edit tool rejects when no prior Read was performed",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "edit target.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"edit-no-read", "tool-use-turn2"},
				Cassette:         "edit-no-read",
			},
			WorkDirFiles: map[string]string{
				"target.txt": "foo bar baz\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"read", "Read", "reading first"},
				},
			},
		},
		{
			ID:          "tool.edit.after-read-success",
			Category:    "tools",
			Description: "edit tool succeeds after Read and produces diff",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "edit target.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"read-then-write", "edit-after-read", "tool-use-turn2"},
				Cassette:         "read-then-write",
			},
			WorkDirFiles: map[string]string{
				"target.txt": "original content\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"---", "+++", "modified"},
				},
			},
		},
		{
			ID:          "tool.edit.tool-in-init",
			Category:    "tools",
			Description: "edit tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"edit"`},
				},
			},
		},
	})
}

// TestToolBash verifies the Bash tool behavior and security.
func TestToolBash(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.bash.echo-success",
			Category:    "tools",
			Description: "Bash tool executes a simple echo command",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo hello",
				Format:           "stream-json",
				CassetteSequence: []string{"tool-use-turn1", "tool-use-turn2"},
				Cassette:         "tool-use-turn1",
				Args:             []string{"--dangerously-skip-permissions"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"hello"},
				},
				StreamJSON: &harness.StreamJSONExpectation{
					HasEventTypes: []string{"tool_call"},
				},
			},
		},
		{
			ID:          "tool.bash.dangerous-command-substitution",
			Category:    "tools",
			Description: "Bash tool blocks command substitution $(...)",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo $(whoami)",
				Format:           "stream-json",
				CassetteSequence: []string{"bash-dangerous-subst", "tool-use-turn2"},
				Cassette:         "bash-dangerous-subst",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Security error", "not allowed", "command substitution"},
				},
			},
		},
		{
			ID:          "tool.bash.sleep-blocked-foreground",
			Category:    "tools",
			Description: "Bash tool blocks sleep >= 2 in foreground",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "sleep 5",
				Format:           "stream-json",
				CassetteSequence: []string{"bash-sleep-blocked", "tool-use-turn2"},
				Cassette:         "bash-sleep-blocked",
				Args:             []string{"--dangerously-skip-permissions"},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"sleep", "not allowed", "background", "run_in_background"},
				},
			},
		},
		{
			ID:          "tool.bash.tool-name-capitalized",
			Category:    "tools",
			Description: "Bash tool name is PascalCase 'Bash' in API tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"Bash"`},
				},
			},
		},
	})
}

// TestToolGlob verifies the Glob tool behavior.
func TestToolGlob(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.glob.matches-files",
			Category:    "tools",
			Description: "Glob tool finds matching files in workdir",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "glob *.txt",
				Format:           "stream-json",
				CassetteSequence: []string{"glob-pattern", "tool-use-turn2"},
				Cassette:         "glob-pattern",
			},
			WorkDirFiles: map[string]string{
				"a.txt":   "file a",
				"b.txt":   "file b",
				"c.go":    "not a txt",
				"d.txt":   "file d",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"a.txt", "b.txt", "d.txt"},
				},
			},
		},
		{
			ID:          "tool.glob.tool-in-init",
			Category:    "tools",
			Description: "Glob tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"Glob"`},
				},
			},
		},
	})
}

// TestToolGrep verifies the Grep tool behavior.
func TestToolGrep(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.grep.finds-pattern",
			Category:    "tools",
			Description: "Grep tool finds matching pattern in files",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "grep for sentinel_marker",
				Format:           "stream-json",
				CassetteSequence: []string{"grep-pattern", "tool-use-turn2"},
				Cassette:         "grep-pattern",
			},
			WorkDirFiles: map[string]string{
				"haystack.txt": "no match here\nsentinel_marker found\nmore text\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"sentinel_marker"},
				},
			},
		},
		{
			ID:          "tool.grep.tool-in-init",
			Category:    "tools",
			Description: "Grep tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"Grep"`},
				},
			},
		},
	})
}

// TestToolWebFetch verifies the web_fetch tool validation behavior.
func TestToolWebFetch(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.webfetch.invalid-scheme",
			Category:    "tools",
			Description: "web_fetch rejects non-HTTP(S) URLs",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "fetch ftp://example.com/file",
				Format:           "stream-json",
				CassetteSequence: []string{"webfetch-invalid", "tool-use-turn2"},
				Cassette:         "webfetch-invalid",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"Unsupported", "scheme", "ftp"},
				},
			},
		},
		{
			ID:          "tool.webfetch.localhost-blocked",
			Category:    "tools",
			Description: "web_fetch blocks access to localhost",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "fetch http://localhost:8080/secret",
				Format:           "stream-json",
				CassetteSequence: []string{"webfetch-localhost", "tool-use-turn2"},
				Cassette:         "webfetch-localhost",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"blocked", "loopback", "localhost", "not allowed"},
				},
			},
		},
		{
			ID:          "tool.webfetch.embedded-credentials",
			Category:    "tools",
			Description: "web_fetch rejects URLs with embedded credentials",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "fetch http://user:pass@example.com",
				Format:           "stream-json",
				CassetteSequence: []string{"webfetch-credentials", "tool-use-turn2"},
				Cassette:         "webfetch-credentials",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"credentials", "not contain"},
				},
			},
		},
		{
			ID:          "tool.webfetch.tool-in-init",
			Category:    "tools",
			Description: "web_fetch tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"web_fetch"`},
				},
			},
		},
	})
}

// TestToolWebSearch verifies the web_search tool validation behavior.
func TestToolWebSearch(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.websearch.tool-in-init",
			Category:    "tools",
			Description: "web_search tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"web_search"`, `web_search`},
				},
			},
		},
	})
}

// TestToolNotebookEdit verifies notebook_edit tool registration.
func TestToolNotebookEdit(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.notebook-edit.tool-in-init",
			Category:    "tools",
			Description: "notebook_edit tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"notebook_edit"`},
				},
			},
		},
	})
}

// TestToolAgent verifies the agent tool registration.
func TestToolAgent(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.agent.tool-in-init",
			Category:    "tools",
			Description: "agent tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"agent"`},
				},
			},
		},
	})
}

// TestToolListMCPResources verifies list_mcp_resources tool registration.
func TestToolListMCPResources(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.list-mcp-resources.tool-in-init",
			Category:    "tools",
			Description: "list_mcp_resources tool appears in system/init tools array",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"list_mcp_resources"`},
				},
			},
		},
	})
}

// TestToolAPIDefinitions verifies all tools have proper API schema.
func TestToolAPIDefinitions(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "tool.api.all-tools-have-schema",
			Category:    "tools",
			Description: "Every tool in the API request has name, description, input_schema",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				APIRequests: []harness.APIRequestExpectation{
					{
						Tools: &harness.ToolsExpectation{
							MinCount: 8,
							HasTool: []string{
								"Bash", "Read", "write", "edit",
								"Glob", "Grep", "web_fetch",
								"notebook_edit",
							},
							EachHasFields: []string{"name", "description", "input_schema"},
						},
					},
				},
			},
		},
	})
}
