package parity_test

import (
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// TestSkillActivation verifies the activate_skill tool behavior.
func TestSkillActivation(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "skill.activate.success",
			Category:    "skills",
			Description: "activate_skill returns skill content when skill exists",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "activate demo-skill",
				Format:           "stream-json",
				CassetteSequence: []string{"skill-activate", "tool-use-turn2"},
				Cassette:         "skill-activate",
			},
			WorkDirFiles: map[string]string{
				".jenny/skills/demo-skill/SKILL.md": "---\ndescription: A demo skill for testing\n---\n# Demo Skill\n\nThis is a demo skill.\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"activated_skill", "Demo Skill"},
				},
			},
		},
		{
			ID:          "skill.activate.tool-in-init-when-skills-exist",
			Category:    "skills",
			Description: "activate_skill tool appears in init when skills are discovered",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			WorkDirFiles: map[string]string{
				".jenny/skills/test-skill/SKILL.md": "---\ndescription: test\n---\ntest skill body\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"activate_skill"`},
				},
			},
		},
	})
}

// TestSkillDiscovery verifies skill discovery from project directory.
func TestSkillDiscovery(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "skill.discovery.activate-tool-registered",
			Category:    "skills",
			Description: "activate_skill tool is registered when skills are discovered",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			WorkDirFiles: map[string]string{
				".jenny/skills/my-test-skill/SKILL.md": "---\ndescription: test skill\n---\nContent\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"activate_skill"`},
				},
			},
		},
	})
}

// TestBareMode verifies --bare skips skill discovery.
func TestBareMode(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "skill.bare-mode.skips-skills",
			Category:    "skills",
			Description: "--bare flag skips skill discovery",
			Target: harness.TargetInvocation{
				Kind:     "cli",
				Args:     []string{"--bare", "--print-system-prompt"},
			},
			WorkDirFiles: map[string]string{
				".jenny/skills/should-not-load/SKILL.md": "---\ndescription: nope\n---\nskipped\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"should-not-load"},
				},
			},
		},
	})
}

// TestPluginDiscovery verifies plugin discovery and manifest loading.
func TestPluginDiscovery(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "plugin.discovery.loads-plugin-skills",
			Category:    "plugins",
			Description: "plugin skills register activate_skill tool",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			WorkDirFiles: map[string]string{
				".jenny-plugin/plugin.json": `{"name":"test-plugin","version":"0.1.0","skills":"./plugin-skills"}`,
				"plugin-skills/plugin-skill/SKILL.md": "---\ndescription: skill from plugin\n---\nPlugin skill content\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{`"activate_skill"`},
				},
			},
		},
		{
			ID:          "plugin.discovery.no-error-on-valid-manifest",
			Category:    "plugins",
			Description: "valid plugin.json loads without error",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "hi",
				Format:   "stream-json",
				Cassette: "echo-hello",
			},
			WorkDirFiles: map[string]string{
				".jenny-plugin/plugin.json": `{"name":"test-plugin","version":"0.1.0"}`,
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

// TestInstructionFiles verifies CLAUDE.md/AGENTS.md injection into system prompt.
func TestInstructionFiles(t *testing.T) {
	runParitySuite(t, []*harness.TestCase{
		{
			ID:          "instructions.claude-md.injected",
			Category:    "instructions",
			Description: "CLAUDE.md content appears in system prompt",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			WorkDirFiles: map[string]string{
				"CLAUDE.md": "# Rules\nINSTRUCTION_SENTINEL_ABC123\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"INSTRUCTION_SENTINEL_ABC123"},
				},
			},
		},
		{
			ID:          "instructions.agents-md.fallback",
			Category:    "instructions",
			Description: "AGENTS.md is used when CLAUDE.md is absent",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			WorkDirFiles: map[string]string{
				"AGENTS.md": "# Agents Rules\nAGENTS_SENTINEL_XYZ789\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains: []string{"AGENTS_SENTINEL_XYZ789"},
				},
			},
		},
		{
			ID:          "instructions.claude-md.precedence",
			Category:    "instructions",
			Description: "CLAUDE.md takes precedence over AGENTS.md",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			WorkDirFiles: map[string]string{
				"CLAUDE.md": "CLAUDE_WINS_SENTINEL\n",
				"AGENTS.md": "AGENTS_LOSES_SENTINEL\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					Contains:    []string{"CLAUDE_WINS_SENTINEL"},
					NotContains: []string{"AGENTS_LOSES_SENTINEL"},
				},
			},
		},
		{
			ID:          "instructions.subdir.not-loaded",
			Category:    "instructions",
			Description: "CLAUDE.md in subdirectory is NOT loaded",
			Target: harness.TargetInvocation{
				Kind: "cli",
				Args: []string{"--print-system-prompt"},
			},
			WorkDirFiles: map[string]string{
				"subdir/CLAUDE.md": "SUBDIR_SENTINEL_SHOULD_NOT_APPEAR\n",
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
				Stdout: &harness.StdoutExpectation{
					NotContains: []string{"SUBDIR_SENTINEL_SHOULD_NOT_APPEAR"},
				},
			},
		},
	})
}
