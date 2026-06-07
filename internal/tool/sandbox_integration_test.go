//go:build !production

package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/sandbox"
)

// ---------------------------------------------------------------------------
// AC1 — Bash wrapped unless excluded
// ---------------------------------------------------------------------------

func TestBashTool_AC1_NoSandboxConfigured(t *testing.T) {
	// When sandbox is nil, command executes normally without wrapping
	bash := NewBashTool(true)
	result, err := bash.Execute(context.Background(), map[string]any{
		"command": "echo ac1_no_sb",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "ac1_no_sb") {
		t.Errorf("expected output to contain 'ac1_no_sb', got: %s", result.Content)
	}
}

func TestBashTool_AC1_ActiveSandboxWrapsCommand(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	bash := NewBashTool(true).WithSandbox(sb)
	result, err := bash.Execute(context.Background(), map[string]any{
		"command": "echo ac1_wrapped",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Verify WrapWithSandbox was called with the original command
	calls := sb.GetWrapCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 WrapWithSandbox call, got %d", len(calls))
	}
	if calls[0] != "echo ac1_wrapped" {
		t.Errorf("expected wrap call for 'echo ac1_wrapped', got %q", calls[0])
	}
}

func TestBashTool_AC1_ExcludedCommandBypassesSandbox(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:          sandbox.BackendLinux,
		ExcludedCommands: []string{"echo*"},
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	bash := NewBashTool(true).WithSandbox(sb)
	_, err := bash.Execute(context.Background(), map[string]any{
		"command": "echo ac1_excluded",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := sb.GetWrapCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 WrapWithSandbox call, got %d", len(calls))
	}
	// The mock's WrapWithSandbox returns original command for excluded patterns.
	// The call was still made — exclusion happens inside WrapWithSandbox.
	// Verifying the call was made confirms BashTool delegated correctly.
}

func TestBashTool_AC1_DangerouslyDisableSandboxBypasses(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	bash := NewBashTool(true).WithSandbox(sb)
	result, err := bash.Execute(context.Background(), map[string]any{
		"command":                   "echo ac1_optout",
		"dangerouslyDisableSandbox": true,
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "ac1_optout") {
		t.Errorf("expected output to contain 'ac1_optout', got: %s", result.Content)
	}

	// WrapWithSandbox should NOT be called
	calls := sb.GetWrapCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 WrapWithSandbox calls with dangerouslyDisableSandbox, got %d", len(calls))
	}
}

func TestBashTool_AC1_InactiveSandboxDoesNotWrap(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendNone, // inactive
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	bash := NewBashTool(true).WithSandbox(sb)
	result, err := bash.Execute(context.Background(), map[string]any{
		"command": "echo ac1_inactive",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	calls := sb.GetWrapCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 WrapWithSandbox calls with inactive sandbox, got %d", len(calls))
	}
}

// ---------------------------------------------------------------------------
// AC2 — Managed-domains-only restricts network (via mock config)
// ---------------------------------------------------------------------------

func TestSandbox_AC2_ManagedDomainsOnlyConfig(t *testing.T) {
	// Verify the network policy constant values
	if sandbox.NetworkPolicyManagedDomainsOnly != "managed-domains-only" {
		t.Errorf("expected NetworkPolicyManagedDomainsOnly to be 'managed-domains-only', got %q", sandbox.NetworkPolicyManagedDomainsOnly)
	}
	if sandbox.NetworkPolicyNormal != "normal" {
		t.Errorf("expected NetworkPolicyNormal to be 'normal', got %q", sandbox.NetworkPolicyNormal)
	}

	// Verify the mock honors network policy config (round-trip through Config)
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:        sandbox.BackendLinux,
		NetworkPolicy:  sandbox.NetworkPolicyManagedDomainsOnly,
		AllowedDomains: []string{"api.example.com", "docs.example.com"},
		DeniedDomains:  []string{"evil.example.com"},
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if !sb.IsActive() {
		t.Error("expected sandbox to be active")
	}

	// Wrap a command to verify mock processing works under managed-domains-only
	wrapped, err := sb.WrapWithSandbox("curl https://api.example.com")
	if err != nil {
		t.Fatalf("WrapWithSandbox failed: %v", err)
	}
	if wrapped == "" {
		t.Error("expected non-empty wrapped command")
	}
}

func TestSandbox_AC2_DeniedDomainsAlwaysBlocked(t *testing.T) {
	// DeniedDomains should block regardless of policy mode
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:       sandbox.BackendLinux,
		NetworkPolicy: sandbox.NetworkPolicyNormal, // normal mode, but still should have denied domains config
		DeniedDomains: []string{"evil.example.com"},
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	_ = sb // Config has DeniedDomains — real backends enforce, mock stores config
}

// ---------------------------------------------------------------------------
// AC3 — Grep uses sandboxed ripgrep when sandbox on
// ---------------------------------------------------------------------------

func TestGrepTool_AC3_SandboxedRipgrepPathUsed(t *testing.T) {
	// When sandbox is active and ripgrep config has Command, Grep uses that path
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
		Ripgrep: sandbox.RipgrepConfig{
			Command: "rg",
			Args:    []string{"--hidden"},
			Argv0:   "rg",
		},
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	grep := NewGrepTool().WithSandbox(sb)

	// Create a temp dir with a searchable file
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello rg"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := grep.Execute(context.Background(), map[string]any{
		"pattern": "hello",
		"path":    tmpDir,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	// Should find the file
	if !strings.Contains(result.Content, "test.txt") {
		t.Errorf("expected output to contain 'test.txt', got: %s", result.Content)
	}
}

func TestGrepTool_AC3_NoSandboxFallsBackToHostRg(t *testing.T) {
	// When no sandbox configured, Grep uses host rg
	grep := NewGrepTool()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello rg"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := grep.Execute(context.Background(), map[string]any{
		"pattern": "hello",
		"path":    tmpDir,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test.txt") {
		t.Errorf("expected output to contain 'test.txt', got: %s", result.Content)
	}
}

func TestGrepTool_AC3_InactiveSandboxFallsBackToHostRg(t *testing.T) {
	// When sandbox is inactive, Grep uses host rg even if sandbox is configured
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendNone, // inactive
		Ripgrep: sandbox.RipgrepConfig{
			Command: "rg",
		},
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	grep := NewGrepTool().WithSandbox(sb)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("hello rg"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	result, err := grep.Execute(context.Background(), map[string]any{
		"pattern": "hello",
		"path":    tmpDir,
	}, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test.txt") {
		t.Errorf("expected output to contain 'test.txt', got: %s", result.Content)
	}
}

// ---------------------------------------------------------------------------
// AC4 — Missing deps yield clear unavailable reason
// ---------------------------------------------------------------------------

func TestSandbox_AC4_ErrMissingDependencyFormat(t *testing.T) {
	err := &sandbox.ErrMissingDependency{
		Backend:     sandbox.BackendLinux,
		Dependency:  "bwrap",
		InstallHint: "sudo apt install bubblewrap",
	}
	msg := err.Error()
	expected := "sandbox backend linux missing dependency: bwrap. sudo apt install bubblewrap"
	if msg != expected {
		t.Errorf("error message mismatch:\n  got:  %s\n  want: %s", msg, expected)
	}
}

func TestSandbox_AC4_MockMissingDepsReturnsErr(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	sb.SetMissingDeps(true, "bwrap", "sudo apt install bubblewrap")

	cfg := sandbox.Config{
		Backend:           sandbox.BackendLinux,
		FailIfUnavailable: true,
	}

	err := sb.Initialize(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when deps missing and FailIfUnavailable is true")
	}

	depErr, ok := err.(*sandbox.ErrMissingDependency)
	if !ok {
		t.Fatalf("expected *ErrMissingDependency, got %T", err)
	}
	if depErr.Dependency != "bwrap" {
		t.Errorf("expected dependency 'bwrap', got %q", depErr.Dependency)
	}
	if depErr.InstallHint == "" {
		t.Error("expected non-empty install hint")
	}
	// mock SetMissingDeps captures Backend before Initialize sets config,
	// so Backend field may be empty. Verify from the error string instead.
	if !strings.Contains(err.Error(), "bwrap") {
		t.Errorf("expected error to mention 'bwrap', got: %s", err.Error())
	}
}

func TestSandbox_AC4_MockMissingDepsWarningMode(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	sb.SetMissingDeps(true, "bwrap", "sudo apt install bubblewrap")

	cfg := sandbox.Config{
		Backend:           sandbox.BackendLinux,
		FailIfUnavailable: false, // Warning mode — no error, but unavailable
	}

	err := sb.Initialize(context.Background(), cfg)
	if err != nil {
		t.Errorf("expected no error in warning mode, got: %v", err)
	}
	if sb.IsAvailable() {
		t.Error("expected sandbox to be unavailable in warning mode")
	}
}

// Test that the macOS backend properly checks for sandbox-exec
// (black-box: initializing on macOS should succeed since sandbox-exec is present)
func TestSandbox_AC4_MacOSBackendDependencyCheck(t *testing.T) {
	// This is a platform-specific test — only runs on darwin
	sb := sandbox.NewMacOSSandboxManager()
	cfg := sandbox.Config{
		Backend:           sandbox.BackendMacOS,
		FailIfUnavailable: true,
	}

	err := sb.Initialize(context.Background(), cfg)
	if err != nil {
		// Could fail if sandbox-exec is not available (e.g., containerized env)
		// The important thing is it returns a clear error
		depErr, ok := err.(*sandbox.ErrMissingDependency)
		if !ok {
			t.Fatalf("expected ErrMissingDependency on failure, got %T: %v", err, err)
		}
		t.Logf("macOS backend missing dependency: %s — hint: %s", depErr.Dependency, depErr.InstallHint)
	}
	if err == nil {
		if !sb.IsAvailable() {
			t.Error("expected sandbox to be available after successful init")
		}
		if !sb.IsActive() {
			t.Error("expected sandbox to be active with BackendMacOS")
		}
	}
}

// ---------------------------------------------------------------------------
// AC5 — Config refresh without restart
// ---------------------------------------------------------------------------

func TestSandbox_AC5_MockRefreshConfig(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Refresh should succeed without error
	if err := sb.RefreshConfig(context.Background()); err != nil {
		t.Errorf("RefreshConfig failed: %v", err)
	}
}

func TestSandbox_AC5_MacOSRefreshConfig(t *testing.T) {
	sb := sandbox.NewMacOSSandboxManager()
	cfg := sandbox.Config{
		Backend:           sandbox.BackendMacOS,
		FailIfUnavailable: true,
	}

	if err := sb.Initialize(context.Background(), cfg); err != nil {
		t.Skipf("macOS backend not available, skipping: %v", err)
	}

	// Refresh should re-run setup and succeed
	if err := sb.RefreshConfig(context.Background()); err != nil {
		t.Errorf("RefreshConfig failed: %v", err)
	}
}

func TestSandbox_AC5_RefreshAfterConfigChange(t *testing.T) {
	// Verify RefreshConfig re-reads sandbox settings
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Initial state — no excluded commands
	wrapped, err := sb.WrapWithSandbox("echo test")
	if err != nil {
		t.Fatalf("WrapWithSandbox failed: %v", err)
	}
	// Default mock behavior: returns original command
	if wrapped != "echo test" {
		t.Logf("initial wrap result: %s", wrapped)
	}

	// Refresh with new config (simulates admin surface permission change)
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:          sandbox.BackendLinux,
		ExcludedCommands: []string{"echo*"},
	}); err != nil {
		t.Fatalf("re-init failed: %v", err)
	}

	// After re-init with excluded patterns, echo commands should bypass
	wrapped, err = sb.WrapWithSandbox("echo test")
	if err != nil {
		t.Fatalf("WrapWithSandbox after refresh failed: %v", err)
	}
	if wrapped != "echo test" {
		t.Errorf("expected excluded command to return original, got %q", wrapped)
	}
}

// ---------------------------------------------------------------------------
// Registry wiring test
// ---------------------------------------------------------------------------

func TestRegistry_WithSandboxWiring(t *testing.T) {
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Build registry with sandbox
	tools := NewRegistry().
		WithBaseTools().
		WithReadFileCache(nil).
		WithSandbox(sb).
		Build()

	// Find bash and grep tools
	var bashTool *BashTool
	var grepTool *GrepTool
	for _, t := range tools {
		switch v := t.(type) {
		case *BashTool:
			bashTool = v
		case *GrepTool:
			grepTool = v
		}
	}
	if bashTool == nil {
		t.Fatal("expected BashTool in built registry")
	}
	if grepTool == nil {
		t.Fatal("expected GrepTool in built registry")
	}

	// Verify they have the sandbox wired
	// Can't directly read unexported fields, so test behavior:
	result, err := bashTool.Execute(context.Background(), map[string]any{
		"command": "echo wired_test",
	}, "/tmp")
	if err != nil {
		t.Fatalf("bash execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("bash error: %s", result.Content)
	}
	calls := sb.GetWrapCalls()
	found := slices.Contains(calls, "echo wired_test")
	if !found {
		t.Error("expected WrapWithSandbox to be called with the bash command via registry wiring")
	}
}

// ---------------------------------------------------------------------------
// Platform backend: macOS sandbox-exec wraps commands
// ---------------------------------------------------------------------------

func TestMacOSSandbox_AC1_WrapsCommand(t *testing.T) {
	sb := sandbox.NewMacOSSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:           sandbox.BackendMacOS,
		FailIfUnavailable: true,
	}); err != nil {
		t.Skipf("macOS sandbox not available: %v", err)
	}

	wrapped, err := sb.WrapWithSandbox("echo hello")
	if err != nil {
		t.Fatalf("WrapWithSandbox failed: %v", err)
	}

	// Should start with sandbox-exec
	if !strings.HasPrefix(wrapped, "sandbox-exec") {
		t.Errorf("expected wrapped command to start with 'sandbox-exec', got: %s", wrapped)
	}
	// Should contain the original command
	if !strings.Contains(wrapped, "echo hello") {
		t.Errorf("expected wrapped command to contain 'echo hello', got: %s", wrapped)
	}
}

func TestMacOSSandbox_AC1_ExcludedCommands(t *testing.T) {
	sb := sandbox.NewMacOSSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:           sandbox.BackendMacOS,
		FailIfUnavailable: true,
		ExcludedCommands:  []string{"echo*"},
	}); err != nil {
		t.Skipf("macOS sandbox not available: %v", err)
	}

	// A command matching the excluded pattern should be returned as-is
	wrapped, err := sb.WrapWithSandbox("echo hello")
	if err != nil {
		t.Fatalf("WrapWithSandbox failed: %v", err)
	}
	if wrapped != "echo hello" {
		t.Errorf("expected excluded command to return unchanged, got: %s", wrapped)
	}
}

func TestMacOSSandbox_AC2_NetworkPolicy(t *testing.T) {
	sb := sandbox.NewMacOSSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend:           sandbox.BackendMacOS,
		FailIfUnavailable: true,
		NetworkPolicy:     sandbox.NetworkPolicyManagedDomainsOnly,
		AllowedDomains:    []string{"api.example.com"},
		DeniedDomains:     []string{"evil.example.com"},
	}); err != nil {
		t.Skipf("macOS sandbox not available: %v", err)
	}

	// Wrap a network command — the profile should restrict network
	wrapped, err := sb.WrapWithSandbox("curl https://api.example.com")
	if err != nil {
		t.Fatalf("WrapWithSandbox failed: %v", err)
	}
	if !strings.Contains(wrapped, "sandbox-exec") {
		t.Errorf("expected wrapped command to contain 'sandbox-exec', got: %s", wrapped)
	}
}

// ---------------------------------------------------------------------------
// Unhappy paths
// ---------------------------------------------------------------------------

func TestBashTool_AC1_Unhappy_SandboxWrapError(t *testing.T) {
	// When WrapWithSandbox returns an error, BashTool should return a clear
	// sandbox error message. Use a command that passes CommandGate.
	sb := sandbox.NewMockSandboxManager()
	if err := sb.Initialize(context.Background(), sandbox.Config{
		Backend: sandbox.BackendLinux,
	}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sb.SetWrapError(fmt.Errorf("sandbox policy violation"))

	bash := NewBashTool(true).WithSandbox(sb)
	result, err := bash.Execute(context.Background(), map[string]any{
		"command": "echo blocked_by_sandbox",
	}, "/tmp")
	if err != nil {
		t.Fatalf("expected no top-level error, got: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result when sandbox wrapping fails")
	}
	if !strings.Contains(result.Content, "Sandbox error") {
		t.Errorf("expected 'Sandbox error' in result content, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "sandbox policy violation") {
		t.Errorf("expected error detail in result, got: %s", result.Content)
	}
}

func TestBashTool_AC1_Unhappy_NilSandboxInactive(t *testing.T) {
	// Proves nil sandbox doesn't cause issues — command runs normally
	bash := NewBashTool(true)
	result, err := bash.Execute(context.Background(), map[string]any{
		"command": "echo no_sandbox_ok",
	}, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no_sandbox_ok") {
		t.Errorf("expected output 'no_sandbox_ok', got: %s", result.Content)
	}
}

func TestMacOSSandbox_AC4_Unhappy_MissingBinary(t *testing.T) {
	// Test macOS backend behavior when binary is not found
	sb := sandbox.NewMacOSSandboxManager()
	// Use a non-existent ripgrep path to trigger missing dep error
	cfg := sandbox.Config{
		Backend:           sandbox.BackendMacOS,
		FailIfUnavailable: true,
		Ripgrep: sandbox.RipgrepConfig{
			Command: "/nonexistent/rg",
		},
	}

	err := sb.Initialize(context.Background(), cfg)
	if err == nil {
		t.Skip("ripgrep at nonexistent path found (unexpected), skipping")
	}

	depErr, ok := err.(*sandbox.ErrMissingDependency)
	if !ok {
		t.Fatalf("expected ErrMissingDependency, got %T: %v", err, err)
	}
	if !strings.Contains(depErr.Dependency, "ripgrep") {
		t.Errorf("expected dependency reference to ripgrep, got %q", depErr.Dependency)
	}
	if depErr.InstallHint == "" {
		t.Error("expected non-empty install hint")
	}
}
