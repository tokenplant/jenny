package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/redact"
	"github.com/ipy/jenny/internal/tool"
)

func initTestGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Setup local git config instead of relying on env vars to avoid mutating test environment
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "checkout", "-b", "main")
	cmd.Dir = dir
	_ = cmd.Run()

	cmd = exec.Command("git", "commit", "--allow-empty", "-m", "initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}
}

// mockTool is a simple mock tool for testing.
type mockTool struct {
	name        string
	description string
	inputSchema map[string]any
}

func (t *mockTool) Name() string                { return t.name }
func (t *mockTool) Description() string         { return t.description }
func (t *mockTool) InputSchema() map[string]any { return t.inputSchema }
func (t *mockTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	return &tool.ToolResult{Content: "mock result"}, nil
}

func TestAssembleSystemPrompt_CustomReplacesDefaults(t *testing.T) {
	// AC1: Custom system prompt replaces all default sections
	cfg := StreamConfig{
		CustomSystemPrompt: "This is my custom system prompt that should replace everything.",
	}

	tools := []tool.Tool{
		&mockTool{name: "Read", description: "Read files"},
		&mockTool{name: "Bash", description: "Run bash commands"},
	}

	prompt := AssembleSystemPrompt(cfg, tools, "/some/path")

	// Custom prompt should be present
	if !strings.Contains(prompt, "This is my custom system prompt") {
		t.Error("custom prompt should be in output")
	}

	// Default intro should NOT be present
	if (strings.Contains(prompt, "autonomous") || strings.Contains(prompt, "non-interactive")) {
		t.Error("default intro should not be present when custom is set")
	}

	// Tool list should NOT be present (custom replaces defaults)
	if strings.Contains(prompt, "Available tools:") {
		t.Error("tool list should not be present when custom is set")
	}
}

func TestAssembleSystemPrompt_ToolListSync(t *testing.T) {
	// AC2: Tool list matches exactly the tools passed
	cfg := StreamConfig{}

	tools := []tool.Tool{
		&mockTool{name: "Read", description: "Read files"},
		&mockTool{name: "Bash", description: "Run bash commands"},
		&mockTool{name: "Glob", description: "Find files"},
	}

	prompt := AssembleSystemPrompt(cfg, tools, "/some/path")

	// Tool list should contain all tool names
	for _, tt := range tools {
		if !strings.Contains(prompt, tt.Name()) {
			t.Errorf("tool %s should be in prompt", tt.Name())
		}
	}

	// Should contain the exact format
	expectedTools := "Available tools: Read, Bash, Glob"
	if !strings.Contains(prompt, expectedTools) {
		t.Errorf("expected tool list %q in prompt", expectedTools)
	}
}

func TestAssembleSystemPrompt_ToolListEmpty(t *testing.T) {
	// When no tools, no tool list section
	cfg := StreamConfig{}
	tools := []tool.Tool{}

	prompt := AssembleSystemPrompt(cfg, tools, "/some/path")

	// Should not contain "Available tools" since no tools
	if strings.Contains(prompt, "Available tools:") {
		t.Error("should not have tool list when no tools")
	}
}

func TestAssembleSystemPrompt_GitStatusInsideRepo(t *testing.T) {
	// AC3: Git status injected when inside a git repo
	// Use a temporary git repo
	tmpDir := t.TempDir()
	initTestGitRepo(t, tmpDir)

	cfg := StreamConfig{}
	tools := []tool.Tool{}

	prompt := AssembleSystemPrompt(cfg, tools, tmpDir)

	// Should contain git context
	if !strings.Contains(prompt, "Git context:") {
		t.Error("git context should be present in git repo")
	}

	// Should contain branch info
	if !strings.Contains(prompt, "Branch:") {
		t.Error("branch info should be present")
	}
}

func TestAssembleSystemPrompt_GitStatusOutsideRepo(t *testing.T) {
	// AC3: Git section not added outside a git repo
	cfg := StreamConfig{}
	tools := []tool.Tool{}

	// Use a path that is definitely NOT in a git repo
	nonGitDir := t.TempDir()

	prompt := AssembleSystemPrompt(cfg, tools, nonGitDir)

	// Should not contain git context
	if strings.Contains(prompt, "Git context:") {
		t.Error("git context should NOT be present outside git repo")
	}
}

func TestAssembleSystemPrompt_PlatformContext(t *testing.T) {
	// AC4: Platform and cwd context included
	cfg := StreamConfig{}
	tools := []tool.Tool{}

	cwd, _ := os.Getwd()

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	// Should contain platform info
	if !strings.Contains(prompt, "Platform:") {
		t.Error("platform info should be present")
	}

	// Should contain cwd
	if !strings.Contains(prompt, "Cwd:") {
		t.Error("cwd info should be present")
	}

	if !strings.Contains(prompt, cwd) {
		t.Errorf("prompt should contain cwd %s", cwd)
	}
}

func TestAssembleSystemPrompt_AppendSupport(t *testing.T) {
	// AC5: AppendSystemPrompt is appended after assembled sections
	cfg := StreamConfig{
		AppendSystemPrompt: "This is appended content.",
	}
	tools := []tool.Tool{}
	cwd := "/tmp" // Outside git repo to keep it simple

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	if !strings.Contains(prompt, "This is appended content.") {
		t.Error("append content should be present")
	}

	// Should be at the end (followed by a single trailing newline per AC1)
	if !strings.HasSuffix(prompt, "This is appended content.\n") {
		t.Error("append content should be at the end (with trailing newline per AC1)")
	}
}

func TestAssembleSystemPrompt_OverrideSuppressesAppend(t *testing.T) {
	// AC5: OverrideSystemPrompt suppresses append
	cfg := StreamConfig{
		AppendSystemPrompt:   "This should not appear.",
		OverrideSystemPrompt: true,
	}
	tools := []tool.Tool{}
	cwd := "/tmp"

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	if strings.Contains(prompt, "This should not appear.") {
		t.Error("append content should NOT be present when override is true")
	}
}

func TestAssembleSystemPrompt_EmptyAppendIsNoOp(t *testing.T) {
	// Empty append is no-op
	cfg := StreamConfig{
		AppendSystemPrompt: "",
	}
	tools := []tool.Tool{}
	cwd := "/tmp"

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	// Should not have trailing newlines or weird formatting
	// The intro should be the last thing if no append
	if !(strings.Contains(prompt, "autonomous") || strings.Contains(prompt, "non-interactive")) {
		t.Error("should have default intro")
	}
}

func TestAssembleSystemPrompt_CustomWithAppend(t *testing.T) {
	// When custom is set, append is still added (unless override)
	cfg := StreamConfig{
		CustomSystemPrompt: "Custom only.",
		AppendSystemPrompt: "Appended.",
	}
	tools := []tool.Tool{}
	cwd := "/tmp"

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	if !strings.Contains(prompt, "Custom only.") {
		t.Error("custom should be present")
	}
	if !strings.Contains(prompt, "Appended.") {
		t.Error("append should be present with custom")
	}
}

func TestAssembleSystemPrompt_CustomWithOverride(t *testing.T) {
	// Custom + override = only custom, no append
	cfg := StreamConfig{
		CustomSystemPrompt:   "Custom only.",
		AppendSystemPrompt:   "Should not appear.",
		OverrideSystemPrompt: true,
	}
	tools := []tool.Tool{}
	cwd := "/tmp"

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	if !strings.Contains(prompt, "Custom only.") {
		t.Error("custom should be present")
	}
	if strings.Contains(prompt, "Should not appear.") {
		t.Error("append should NOT be present when override is true")
	}
}

func TestAssembleSystemPrompt_DefaultSections(t *testing.T) {
	// Without custom, all default sections should be present
	cfg := StreamConfig{}
	tools := []tool.Tool{
		&mockTool{name: "Read", description: "Read files"},
	}
	cwd := "/tmp" // Outside git repo

	prompt := AssembleSystemPrompt(cfg, tools, cwd)

	// Default intro
	if !(strings.Contains(prompt, "autonomous") || strings.Contains(prompt, "non-interactive")) {
		t.Error("default intro should be present")
	}

	// Tool list
	if !strings.Contains(prompt, "Available tools:") {
		t.Error("tool list should be present")
	}

	// Platform
	if !strings.Contains(prompt, "Platform:") {
		t.Error("platform should be present")
	}
}

func TestToolListSection_FormatsCorrectly(t *testing.T) {
	tools := []tool.Tool{
		&mockTool{name: "Alpha"},
		&mockTool{name: "Beta"},
		&mockTool{name: "Gamma"},
	}

	section, ok := toolListSection(tools)
	if !ok {
		t.Fatal("expected tool list section to be included")
	}

	// Should be comma-separated
	if !strings.Contains(section, "Alpha, Beta, Gamma") && !strings.Contains(section, "Alpha,Beta,Gamma") {
		t.Error("tools should be comma-separated")
	}

	// Should start with "Available tools:"
	if !strings.HasPrefix(section, "Available tools:") {
		t.Error("should start with Available tools:")
	}
}

func TestPlatformSection_ContainsCorrectInfo(t *testing.T) {
	section, ok := platformSection("/test/path")
	if !ok {
		t.Fatal("expected platform section to be included")
	}

	if !strings.Contains(section, "Platform:") {
		t.Error("should contain Platform:")
	}
	if !strings.Contains(section, "Cwd:") {
		t.Error("should contain Cwd:")
	}
	if !strings.Contains(section, "/test/path") {
		t.Error("should contain the cwd path")
	}
}

func TestDefaultIntroSection(t *testing.T) {
	section, ok := defaultIntroSection()
	if !ok {
		t.Fatal("expected default intro section to be included")
	}

	if !(strings.Contains(section, "autonomous") || strings.Contains(section, "non-interactive")) {
		t.Error("should contain intro text")
	}
}

func TestAppendSection_Override(t *testing.T) {
	// When override is true, should not return content
	section, ok := appendSection("some content", true)
	if ok || section != "" {
		t.Error("should return false when override is true")
	}

	// When override is false and content exists, should return content
	section, ok = appendSection("some content", false)
	if !ok || section != "some content" {
		t.Error("should return content when override is false")
	}

	// When content is empty, should not return
	section, ok = appendSection("", false)
	if ok || section != "" {
		t.Error("should return false when content is empty")
	}
}

func TestAssembleSystemPrompt_FreezeOnCachedSystemPrompt(t *testing.T) {
	// When CachedSystemPrompt is set, AssembleSystemPrompt returns it verbatim
	// even if other cfg fields differ (simulating git status change between turns).
	frozen := "This is the frozen prompt from turn 1."
	cfg := StreamConfig{
		CachedSystemPrompt: frozen,
		MemoryContent:      "new memory that changed", // Would bust cache if used
	}
	tools := []tool.Tool{}

	// First call with CachedSystemPrompt set
	result := AssembleSystemPrompt(cfg, tools, "/tmp")
	if result != frozen {
		t.Errorf("expected frozen prompt, got different result:\n%s", result)
	}

	// Simulate second turn: cfg with different dynamic fields but same CachedSystemPrompt
	cfg2 := StreamConfig{
		CachedSystemPrompt: frozen,
		MemoryContent:      "completely different memory",
	}
	result2 := AssembleSystemPrompt(cfg2, tools, "/tmp")
	if result2 != frozen {
		t.Errorf("expected frozen prompt on second call, got:\n%s", result2)
	}

	// Verify the frozen value does NOT contain the new memory content
	if strings.Contains(result, "new memory that changed") {
		t.Error("frozen prompt should not contain dynamic memory content")
	}
}

func TestBuildSystemPrompt_ContainsAllSections(t *testing.T) {
	// buildSystemPrompt assembles all sections; used when CachedSystemPrompt is empty.
	tmpDir := t.TempDir()
	initTestGitRepo(t, tmpDir)

	cfg := StreamConfig{
		MemoryContent:      "session memory content",
		AppendSystemPrompt: "extra appended section",
	}
	tools := []tool.Tool{
		&mockTool{name: "Read", description: "Read files"},
	}

	result := buildSystemPrompt(cfg, tools, tmpDir)

	// Stable sections (these go into cached block)
	if !(strings.Contains(result, "autonomous") || strings.Contains(result, "non-interactive")) {
		t.Error("should contain default intro")
	}
	if !strings.Contains(result, "session memory content") {
		t.Error("should contain memory content")
	}
	if !strings.Contains(result, "Available tools: Read") {
		t.Error("should contain tool list")
	}
	if !strings.Contains(result, "extra appended section") {
		t.Error("should contain append section")
	}

	// Dynamic sections (these go into uncached suffix)
	if !strings.Contains(result, "Git context:") {
		t.Error("should contain git context")
	}
	if !strings.Contains(result, "Platform:") {
		t.Error("should contain platform")
	}
}

func TestDynamicSystemSuffix_AlwaysEmpty_InGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	initTestGitRepo(t, tmpDir)

	for _, cfg := range []StreamConfig{
		{},
		{CustomSystemPrompt: "custom"},
		{MemoryContent: "memory"},
	} {
		result := DynamicSystemSuffix(cfg, tmpDir)
		if result != "" {
			t.Errorf("DynamicSystemSuffix should always return empty, got: %q", result)
		}
	}
}

// AC1: --print-system-prompt must end with a newline so the shell prompt does
// not run onto the last line. Verified by ensuring the last byte is '\n'.
func TestAssembleSystemPrompt_TrailingNewline(t *testing.T) {
	cfg := StreamConfig{}
	tools := []tool.Tool{
		&mockTool{name: "Read", description: "Read files"},
	}

	prompt := AssembleSystemPrompt(cfg, tools, "/some/path")

	if len(prompt) == 0 {
		t.Fatal("prompt is empty")
	}
	if prompt[len(prompt)-1] != '\n' {
		t.Errorf("assembled system prompt must end with a newline; last byte = 0x%02x", prompt[len(prompt)-1])
	}
}

func TestAssembleSystemPrompt_RedactionInstruction(t *testing.T) {
	tests := []struct {
		name           string
		mode           redact.RedactMode
		wantRecoverMsg bool
		wantRedactMsg  bool
	}{
		{
			name:           "disabled",
			mode:           redact.ModeDisabled,
			wantRedactMsg:  false,
			wantRecoverMsg: false,
		},
		{
			name:           "redact mode",
			mode:           redact.ModeRedact,
			wantRedactMsg:  true,
			wantRecoverMsg: false,
		},
		{
			name:           "recover mode",
			mode:           redact.ModeRecover,
			wantRedactMsg:  true,
			wantRecoverMsg: true,
		},
		{
			name:           "empty mode (defaults to recover)",
			mode:           "",
			wantRedactMsg:  true,
			wantRecoverMsg: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := StreamConfig{
				RedactMode: tt.mode,
			}
			prompt := buildSystemPrompt(cfg, nil, "/tmp")

			redactMsg := "This session has secret redaction enabled."
			recoverMsg := "They will be automatically recovered when you use them in tool calls"

			if tt.wantRedactMsg {
				if !strings.Contains(prompt, redactMsg) {
					t.Errorf("expected redaction message in prompt for mode %s", tt.mode)
				}
			} else {
				if strings.Contains(prompt, redactMsg) {
					t.Errorf("did not expect redaction message in prompt for mode %s", tt.mode)
				}
			}

			if tt.wantRecoverMsg {
				if !strings.Contains(prompt, recoverMsg) {
					t.Errorf("expected recovery message in prompt for mode %s", tt.mode)
				}
			} else {
				if strings.Contains(prompt, recoverMsg) {
					t.Errorf("did not expect recovery message in prompt for mode %s", tt.mode)
				}
			}
		})
	}
}
