package e2e_test

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// envVarsBaseEnv returns the minimal env for env-var conformance tests.
// ANTHROPIC_MODEL is intentionally omitted so individual tests can set it.
func envVarsBaseEnv(mockURL string) []string {
	return []string{
		"ANTHROPIC_BASE_URL=" + mockURL + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
	}
}

// TestAnthropicModelEnvVar verifies AC1: ANTHROPIC_MODEL env var sets the model.
func TestAnthropicModelEnvVar(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	env := append(envVarsBaseEnv(mock.URL()), "ANTHROPIC_MODEL=claude-env-sentinel-model")
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC1: exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC1: no request captured")
	}
	got, _ := reqs[0].Body["model"].(string)
	if got != "claude-env-sentinel-model" {
		t.Errorf("AC1: model = %q; want %q", got, "claude-env-sentinel-model")
	}
}

// TestModelFlagPrecedenceOverEnvVar verifies AC2: --model flag wins over ANTHROPIC_MODEL.
func TestModelFlagPrecedenceOverEnvVar(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	env := append(envVarsBaseEnv(mock.URL()), "ANTHROPIC_MODEL=claude-env-sentinel-model")
	res := harness.RunJenny(t, env, "--output-format", "stream-json",
		"--model", "claude-flag-sentinel", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC2: exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC2: no request captured")
	}
	got, _ := reqs[0].Body["model"].(string)
	if got != "claude-flag-sentinel" {
		t.Errorf("AC2: model = %q; want %q (flag should win over env)", got, "claude-flag-sentinel")
	}
}

// TestEmptyAnthropicModelUsesDefault verifies AC3: empty ANTHROPIC_MODEL uses built-in default.
func TestEmptyAnthropicModelUsesDefault(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	env := append(envVarsBaseEnv(mock.URL()), "ANTHROPIC_MODEL=")
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC3: exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC3: no request captured")
	}
	got, _ := reqs[0].Body["model"].(string)
	if !strings.HasPrefix(got, "claude-") {
		t.Errorf("AC3: model = %q; want prefix %q (built-in default)", got, "claude-")
	}
}

// TestAuthTokenForwardedAsXApiKey verifies AC4: auth token reaches x-api-key header.
func TestAuthTokenForwardedAsXApiKey(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=sentinel-auth-token-xyz42",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("AC4: exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC4: no request captured")
	}
	// ANTHROPIC_AUTH_TOKEN is forwarded as Authorization: Bearer <token>.
	authHeader := reqs[0].Header.Get("Authorization")
	if !strings.Contains(authHeader, "sentinel-auth-token-xyz42") {
		t.Errorf("AC4: Authorization header = %q; want it to contain %q", authHeader, "sentinel-auth-token-xyz42")
	}
}
