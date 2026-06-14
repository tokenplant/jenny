package e2e_test

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/e2e/harness"
	"github.com/ipy/jenny/internal/testutil/mockapi"
)

// TestEnvModelOverride verifies ANTHROPIC_MODEL env var sets the model in API request.
func TestEnvModelOverride(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-env-sentinel-model",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}
	got, _ := reqs[0].Body["model"].(string)
	if got != "claude-env-sentinel-model" {
		t.Errorf("model = %q; want %q", got, "claude-env-sentinel-model")
	}
}

// TestModelFlagPrecedence verifies --model flag wins over ANTHROPIC_MODEL env.
func TestModelFlagPrecedence(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=claude-env-sentinel-model",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json",
		"--model", "claude-flag-sentinel", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}
	got, _ := reqs[0].Body["model"].(string)
	if got != "claude-flag-sentinel" {
		t.Errorf("model = %q; want %q (flag should win over env)", got, "claude-flag-sentinel")
	}
}

// TestEmptyModelUsesDefault verifies empty ANTHROPIC_MODEL uses built-in default.
func TestEmptyModelUsesDefault(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}
	got, _ := reqs[0].Body["model"].(string)
	if !strings.HasPrefix(got, "claude-") {
		t.Errorf("model = %q; want prefix %q (built-in default)", got, "claude-")
	}
}

// TestAuthTokenForwarded verifies auth token reaches the API header.
func TestAuthTokenForwarded(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=sentinel-auth-token-xyz42",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}
	authHeader := reqs[0].Header.Get("Authorization")
	if !strings.Contains(authHeader, "sentinel-auth-token-xyz42") {
		t.Errorf("Authorization header = %q; want it to contain auth token", authHeader)
	}
}

// TestSystemPromptOverride verifies --system-prompt replaces default in API request.
func TestSystemPromptOverride(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json",
		"--system-prompt", "CUSTOM_SYS_SENTINEL", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}

	sysText := extractSystemText(reqs[0].Body)
	if !strings.Contains(sysText, "CUSTOM_SYS_SENTINEL") {
		t.Errorf("system prompt does not contain CUSTOM_SYS_SENTINEL")
	}
	if (strings.Contains(sysText, "autonomous") || strings.Contains(sysText, "non-interactive")) {
		t.Error("system prompt still contains default text after --system-prompt override")
	}
}

// TestAppendSystemPrompt verifies --append-system-prompt appends to default.
func TestAppendSystemPrompt(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json",
		"--append-system-prompt", "APPEND_SENTINEL_XYZ", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}

	sysText := extractSystemText(reqs[0].Body)
	if !(strings.Contains(sysText, "autonomous") || strings.Contains(sysText, "non-interactive")) {
		t.Error("system prompt missing default text after --append-system-prompt")
	}
	if !strings.Contains(sysText, "APPEND_SENTINEL_XYZ") {
		t.Error("system prompt does not contain appended text")
	}
}

// TestStreamIsTrue verifies stream=true in API request.
func TestStreamIsTrue(t *testing.T) {
	mock := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock.Close()

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/echo-hello",
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "hi")
	if res.ExitCode != 0 {
		t.Fatalf("exit %d; stderr=%q", res.ExitCode, res.Stderr)
	}
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request captured")
	}
	stream, _ := reqs[0].Body["stream"].(bool)
	if !stream {
		t.Errorf("stream should be true, got %v", reqs[0].Body["stream"])
	}
}

// TestDeterministicReplay verifies two identical runs produce identical event sequences.
func TestDeterministicReplay(t *testing.T) {
	env := func(mockURL string) []string {
		return []string{
			"ANTHROPIC_BASE_URL=" + mockURL + "/cassette/echo-hello",
			"ANTHROPIC_AUTH_TOKEN=test-token",
			"ANTHROPIC_MODEL=",
		}
	}

	mock1 := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock1.Close()
	res1 := harness.RunJenny(t, env(mock1.URL()), "--output-format", "stream-json", "-p", "echo hello")
	if res1.ExitCode != 0 {
		t.Fatalf("run1: exit %d; stderr=%q", res1.ExitCode, res1.Stderr)
	}

	mock2 := mockapi.NewMockServer(mockapi.WithCassetteDir(cassetteDir))
	defer mock2.Close()
	res2 := harness.RunJenny(t, env(mock2.URL()), "--output-format", "stream-json", "-p", "echo hello")
	if res2.ExitCode != 0 {
		t.Fatalf("run2: exit %d; stderr=%q", res2.ExitCode, res2.Stderr)
	}

	if len(res1.Parsed) != len(res2.Parsed) {
		t.Errorf("event counts differ: run1=%d run2=%d", len(res1.Parsed), len(res2.Parsed))
	}

	for i := range min(len(res1.Parsed), len(res2.Parsed)) {
		t1, _ := res1.Parsed[i]["type"].(string)
		t2, _ := res2.Parsed[i]["type"].(string)
		if t1 != t2 {
			t.Errorf("event %d type mismatch: %q vs %q", i, t1, t2)
		}
	}
}

func extractSystemText(body map[string]any) string {
	raw, ok := body["system"]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, block := range v {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			text, _ := m["text"].(string)
			b.WriteString(text)
		}
		return b.String()
	default:
		return ""
	}
}
