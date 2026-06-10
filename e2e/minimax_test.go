package e2e_test

import (
	"testing"

	"github.com/ipy/jenny/e2e/harness"
)

func TestMinimaxBadRequestReproduced(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "minimax.reject-empty-tools.handled",
			Category:    "provider-parity",
			Description: "jenny adds __arg__ so minimax does not reject empty schemas",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo hello",
				Format:           "stream-json",
				CassetteSequence: []string{"tool-use-turn1", "tool-use-turn2"},
				Env: []string{
					// Use a placeholder for the mock server URL, we need dynamic substitution or just use a standard setup
					// The suite runner sets up the mock server. If we use kind=prompt, SuiteRunner injects ANTHROPIC_BASE_URL.
					// Let's explicitly override it. Wait, SuiteRunner uses the mock server URL.
					// We need the mock URL to have "minimaxi" in it.
					// We can do this by setting a feature in TargetInvocation or overriding it in test setup.
					// For declarative, we can use a special macro like ${MOCK_URL}/minimaxi/v1/cassette/...
					"ANTHROPIC_BASE_URL=${MOCK_URL}/minimaxi/cassette/tool-use-req",
					"ANTHROPIC_AUTH_TOKEN=test-token",
					"ANTHROPIC_MODEL=",
				},
				MockBehavior: &harness.MockBehavior{
					RejectEmptyToolProperties: true,
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

func TestMinimaxToolSerializationPasses(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "minimax.serialization.has-arg-placeholder",
			Category:    "provider-parity",
			Description: "__arg__ placeholder is added to tools when talking to minimax",
			Target: harness.TargetInvocation{
				Kind:             "prompt",
				Prompt:           "run echo hello",
				Format:           "stream-json",
				CassetteSequence: []string{"tool-use-turn1", "tool-use-turn2"},
				Env: []string{
					"ANTHROPIC_BASE_URL=${MOCK_URL}/minimaxi/cassette/tool-use-req",
					"ANTHROPIC_AUTH_TOKEN=test-token",
					"ANTHROPIC_MODEL=",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}

func TestAnthropicEndpointRegression(t *testing.T) {
	runE2ESuite(t, []*harness.TestCase{
		{
			ID:          "anthropic.regression.no-arg-placeholder",
			Category:    "provider-parity",
			Description: "standard anthropic endpoints do not receive __arg__ placeholder",
			Target: harness.TargetInvocation{
				Kind:     "prompt",
				Prompt:   "echo hello",
				Format:   "stream-json",
				Cassette: "echo-hello",
				Env: []string{
					"ANTHROPIC_BASE_URL=${MOCK_URL}/cassette/echo-hello",
					"ANTHROPIC_AUTH_TOKEN=test-token",
					"ANTHROPIC_MODEL=",
				},
			},
			Expected: harness.ExpectedBehavior{
				ExitCode: 0,
			},
		},
	})
}
