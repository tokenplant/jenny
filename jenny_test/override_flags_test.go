package e2e_test

import (
	"strings"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// overrideFlagsEnv returns the env slice needed to talk to the mock server.
func overrideFlagsEnv(mockURL string) []string {
	return []string{
		"ANTHROPIC_BASE_URL=" + mockURL + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}
}

// TestCLIModelFlag verifies AC1: --model value reaches the outbound API request.
func TestCLIModelFlag(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	res := harness.RunJenny(t, overrideFlagsEnv(mock.URL()),
		"--output-format", "stream-json",
		"--model", "claude-sentinel-model",
		"-p", "hi",
	)

	if res.ExitCode != 0 {
		t.Fatalf("AC1: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC1: no request captured by mock server")
	}

	got, _ := reqs[0].Body["model"].(string)
	if got != "claude-sentinel-model" {
		t.Errorf("AC1: model = %q; want %q", got, "claude-sentinel-model")
	}
}

// TestCLISystemPromptFlag verifies AC2: --system-prompt replaces the default system prompt.
func TestCLISystemPromptFlag(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	res := harness.RunJenny(t, overrideFlagsEnv(mock.URL()),
		"--output-format", "stream-json",
		"--system-prompt", "CUSTOM_SYS_SENTINEL",
		"-p", "hi",
	)

	if res.ExitCode != 0 {
		t.Fatalf("AC2: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC2: no request captured by mock server")
	}

	text, err := extractSystemText(reqs[0].Body["system"])
	if err != nil {
		t.Fatalf("AC2: %v", err)
	}

	if !strings.Contains(text, "CUSTOM_SYS_SENTINEL") {
		t.Errorf("AC2: system prompt does not contain CUSTOM_SYS_SENTINEL; got %q", text[:min(len(text), 200)])
	}
	if strings.Contains(text, "You are an AI assistant") {
		t.Errorf("AC2: system prompt still contains default text 'You are an AI assistant'")
	}
}

// TestCLIAppendSystemPromptFlag verifies AC3: --append-system-prompt appends to the default.
func TestCLIAppendSystemPromptFlag(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	res := harness.RunJenny(t, overrideFlagsEnv(mock.URL()),
		"--output-format", "stream-json",
		"--append-system-prompt", "APPEND_SENTINEL",
		"-p", "hi",
	)

	if res.ExitCode != 0 {
		t.Fatalf("AC3: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC3: no request captured by mock server")
	}

	text, err := extractSystemText(reqs[0].Body["system"])
	if err != nil {
		t.Fatalf("AC3: %v", err)
	}

	if !strings.Contains(text, "You are an AI assistant") {
		t.Errorf("AC3: system prompt missing default text 'You are an AI assistant'")
	}
	if !strings.Contains(text, "APPEND_SENTINEL") {
		t.Errorf("AC3: system prompt does not contain APPEND_SENTINEL; got length=%d", len(text))
	}
}

// TestCLICombinedSystemPromptFlags verifies AC4: combined --system-prompt + --append-system-prompt.
func TestCLICombinedSystemPromptFlags(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	defer mock.Close()

	res := harness.RunJenny(t, overrideFlagsEnv(mock.URL()),
		"--output-format", "stream-json",
		"--system-prompt", "CUSTOM_SYS_SENTINEL",
		"--append-system-prompt", "APPEND_SENTINEL",
		"-p", "hi",
	)

	if res.ExitCode != 0 {
		t.Fatalf("AC4: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("AC4: no request captured by mock server")
	}

	text, err := extractSystemText(reqs[0].Body["system"])
	if err != nil {
		t.Fatalf("AC4: %v", err)
	}

	if !strings.Contains(text, "CUSTOM_SYS_SENTINEL") {
		t.Errorf("AC4: system prompt does not contain CUSTOM_SYS_SENTINEL")
	}
	if !strings.Contains(text, "APPEND_SENTINEL") {
		t.Errorf("AC4: system prompt does not contain APPEND_SENTINEL")
	}
	if strings.Contains(text, "You are an AI assistant") {
		t.Errorf("AC4: system prompt still contains default text 'You are an AI assistant'")
	}
}

// TestCLIPrintSystemPromptWithOverride verifies AC5: --print-system-prompt respects --system-prompt offline.
func TestCLIPrintSystemPromptWithOverride(t *testing.T) {
	res := harness.RunJenny(t, nil,
		"--print-system-prompt",
		"--system-prompt", "OFFLINE_SENTINEL",
	)

	if res.ExitCode != 0 {
		t.Fatalf("AC5: jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	text := strings.Join(res.Lines, "\n")

	if !strings.Contains(text, "OFFLINE_SENTINEL") {
		t.Errorf("AC5: output does not contain OFFLINE_SENTINEL; got %q", text[:min(len(text), 300)])
	}
	if strings.Contains(text, "You are an AI assistant") {
		t.Errorf("AC5: output still contains default text 'You are an AI assistant'")
	}
}
