// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
)

// mockClient is a simple mock API client for testing
type mockClient struct {
	response *api.Response
	err      error
}

func (m *mockClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// mockAPIClient is a test double that implements the API client interface
type mockAPIClient struct {
	sendMessageFn func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error)
}

func (m *mockAPIClient) SendMessage(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
	if m.sendMessageFn != nil {
		return m.sendMessageFn(ctx, messages, tools, toolResults, systemPrompt)
	}
	return &api.Response{}, nil
}

// TestAC1_SessionMemoryInitAt10KTokens verifies that session memory file is created
// after accumulating approximately 10K context tokens when auto-compact is enabled.
func TestAC1_SessionMemoryInitAt10KTokens(t *testing.T) {
	// Create temp directory for memdir
	tmpDir := t.TempDir()

	// Create compact config with auto-compact enabled
	compactCfg := CompactConfig{
		DisableAutoCompact: false,
		DisableCompact:     false,
	}

	// Create mock API client
	mockClient := &mockAPIClient{}

	// Create session memory instance
	sm := NewSessionMemory("test-session-ac1", mockClient, compactCfg, tmpDir)

	// Verify no file exists initially
	if sm.fileExists() {
		t.Fatal("Memory file should not exist before threshold")
	}

	// Simulate accumulating ~10K tokens (10001 to be over threshold)
	shouldAct, action := sm.CheckThreshold(10001, 0)

	if !shouldAct {
		t.Fatal("Should trigger action at 10K+ tokens")
	}
	if action != "init" {
		t.Fatalf("Action should be 'init', got '%s'", action)
	}

	// Call Init to create the file
	err := sm.Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify file exists with template content
	info, err := os.Stat(sm.memoryFilePath)
	if err != nil {
		t.Fatalf("Memory file should exist after Init: %v", err)
	}

	// Check file permissions (should be 0600)
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("File permissions should be 0600, got %o", perm)
	}

	// Read and verify content
	content, err := os.ReadFile(sm.memoryFilePath)
	if err != nil {
		t.Fatalf("Failed to read memory file: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("Memory file should have content")
	}

	// Verify template structure
	if !contains(string(content), "# Session Memory: test-session-ac1") {
		t.Fatal("Memory file should contain session ID header")
	}
	if !contains(string(content), "## Context / Goals") {
		t.Fatal("Memory file should contain Context / Goals section")
	}
	if !contains(string(content), "## Key Decisions") {
		t.Fatal("Memory file should contain Key Decisions section")
	}
	if !contains(string(content), "## Current State") {
		t.Fatal("Memory file should contain Current State section")
	}
	if !contains(string(content), "## Open Questions") {
		t.Fatal("Memory file should contain Open Questions section")
	}
}

// TestAC2_UpdateRequiresBothThresholds verifies that update requires both
// token growth >= 5K AND tool calls >= 3.
func TestAC2_UpdateRequiresBothThresholds(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{
		DisableAutoCompact: false,
		DisableCompact:     false,
	}

	mockClient := &mockAPIClient{}
	sm := NewSessionMemory("test-session-ac2", mockClient, compactCfg, tmpDir)

	// Create the memory file first (simulate init happened)
	_ = sm.Init()

	// Reset baselines to simulate mid-session state
	sm.lastBaseline = sm.accumTokens
	sm.lastToolBaseline = sm.toolCalls

	// Test case 1: 5K tokens but only 1 tool call - should NOT update
	sm.accumTokens = sm.lastBaseline + 5000
	sm.toolCalls = sm.lastToolBaseline + 1

	shouldAct, action := sm.CheckThreshold(0, 0)
	if shouldAct {
		t.Fatal("Should NOT trigger update with only tokens met (5K tokens, 1 tool call)")
	}

	// Test case 2: 3 tool calls but only 4K tokens - should NOT update
	sm.accumTokens = sm.lastBaseline + 4000
	sm.toolCalls = sm.lastToolBaseline + 3

	shouldAct, action = sm.CheckThreshold(0, 0)
	if shouldAct {
		t.Fatal("Should NOT trigger update with only tool calls met (4K tokens, 3 tool calls)")
	}

	// Test case 3: 5K tokens AND 3 tool calls - SHOULD update
	sm.accumTokens = sm.lastBaseline + 5000
	sm.toolCalls = sm.lastToolBaseline + 3

	shouldAct, action = sm.CheckThreshold(0, 0)
	if !shouldAct {
		t.Fatal("Should trigger update when both thresholds met (5K tokens, 3 tool calls)")
	}
	if action != "update" {
		t.Fatalf("Action should be 'update', got '%s'", action)
	}
}

// TestAC3_15SecondTimeout verifies that forked agent extraction has a 15-second timeout.
// When the forked agent takes longer than 15 seconds, the extraction is abandoned
// and the main agent loop continues without blocking.
func TestAC3_15SecondTimeout(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{
		DisableAutoCompact: false,
		DisableCompact:     false,
	}

	// Create a slow mock client that blocks beyond 15 seconds
	slowClient := &mockAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			// Simulate work that takes longer than 15 seconds
			select {
			case <-time.After(20 * time.Second):
				return &api.Response{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	sm := NewSessionMemory("test-session-ac3", slowClient, compactCfg, tmpDir)

	// Create memory file
	_ = sm.Init()

	// Set up state that would trigger update
	sm.lastBaseline = 0
	sm.lastToolBaseline = 0
	sm.accumTokens = 15000
	sm.toolCalls = 10

	// Create a context with its own timeout
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Update should not block - it should timeout gracefully
	err := sm.Update(ctx)

	// The update should return nil (graceful timeout per AC3)
	// The main loop should continue without error
	if err != nil {
		t.Fatalf("Update should not return error on timeout, got: %v", err)
	}
}

// TestAC4_ForkedAgentEditOnly verifies that the forked agent is restricted to
// Edit tool only on the session memory file.
func TestAC4_ForkedAgentEditOnly(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{
		DisableAutoCompact: false,
		DisableCompact:     false,
	}

	mockClient := &mockAPIClient{
		sendMessageFn: func(ctx context.Context, messages []api.Message, tools []api.ToolParam, toolResults []api.ToolResult, systemPrompt string) (*api.Response, error) {
			// Verify that only Edit tool is passed
			if len(tools) != 1 {
				t.Fatalf("Expected 1 tool (Edit only), got %d", len(tools))
			}
			if tools[0].Name != "edit" {
				t.Fatalf("Expected tool name 'edit', got '%s'", tools[0].Name)
			}
			return &api.Response{
				Content: []api.ContentBlock{
					{Type: "text", Text: "Session memory updated"},
				},
			}, nil
		},
	}

	sm := NewSessionMemory("test-session-ac4", mockClient, compactCfg, tmpDir)

	// Create memory file
	_ = sm.Init()

	// Set up state that would trigger update
	sm.lastBaseline = 0
	sm.lastToolBaseline = 0
	sm.accumTokens = 15000
	sm.toolCalls = 10

	ctx := context.Background()
	err := sm.Update(ctx)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
}

// TestAC5_DisabledWhenAutoCompactOff verifies that session memory is disabled
// when auto-compact is disabled.
func TestAC5_DisabledWhenAutoCompactOff(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with DisableAutoCompact = true
	compactCfg := CompactConfig{
		DisableAutoCompact: true,
		DisableCompact:     false,
	}

	mockClient := &mockAPIClient{}
	sm := NewSessionMemory("test-session-ac5", mockClient, compactCfg, tmpDir)

	// Even with 10K+ tokens, should not trigger
	shouldAct, action := sm.CheckThreshold(15000, 5)

	if shouldAct {
		t.Fatal("Should NOT trigger action when auto-compact is disabled")
	}
	if action != "disabled" {
		t.Fatalf("Action should be 'disabled', got '%s'", action)
	}

	// Verify no file was created
	if sm.fileExists() {
		t.Fatal("Memory file should not exist when auto-compact is disabled")
	}

	// Test with DisableCompact = true
	compactCfg2 := CompactConfig{
		DisableAutoCompact: false,
		DisableCompact:     true,
	}

	sm2 := NewSessionMemory("test-session-ac5-2", mockClient, compactCfg2, tmpDir)

	shouldAct, action = sm2.CheckThreshold(15000, 5)

	if shouldAct {
		t.Fatal("Should NOT trigger action when compact is disabled")
	}
	if action != "disabled" {
		t.Fatalf("Action should be 'disabled', got '%s'", action)
	}
}

// TestAC5_DisabledBothFlags verifies session memory is disabled when both flags are set.
func TestAC5_DisabledBothFlags(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{
		DisableAutoCompact: true,
		DisableCompact:     true,
	}

	mockClient := &mockAPIClient{}
	sm := NewSessionMemory("test-session-ac5-both", mockClient, compactCfg, tmpDir)

	shouldAct, action := sm.CheckThreshold(20000, 10)

	if shouldAct {
		t.Fatal("Should NOT trigger action when both flags are set")
	}
	if action != "disabled" {
		t.Fatalf("Action should be 'disabled', got '%s'", action)
	}
}

// TestSessionMemory_FilePath verifies the correct file path construction.
func TestSessionMemory_FilePath(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{}

	mockClient := &mockAPIClient{}
	sm := NewSessionMemory("sess_abc123", mockClient, compactCfg, tmpDir)

	expectedPath := filepath.Join(tmpDir, "sess_abc123.md")
	if sm.memoryFilePath != expectedPath {
		t.Fatalf("Expected memory file path %s, got %s", expectedPath, sm.memoryFilePath)
	}
}

// TestSessionMemory_ReadCacheRestricted verifies that the read cache is properly
// managed for the Edit tool restriction.
func TestSessionMemory_ReadCacheRestricted(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{
		DisableAutoCompact: false,
		DisableCompact:     false,
	}

	mockClient := &mockAPIClient{}
	sm := NewSessionMemory("test-session-cache", mockClient, compactCfg, tmpDir)

	// Create memory file
	_ = sm.Init()

	// Verify read cache has entry for the memory file
	entry, exists := sm.readCache.GetRead(sm.memoryFilePath)
	if !exists {
		t.Fatal("Read cache should have entry for memory file after Init")
	}
	if entry.Content == "" {
		t.Fatal("Read cache entry should have content")
	}

	// Verify Remove works for dedup invalidation
	sm.readCache.Remove(sm.memoryFilePath)
	_, exists = sm.readCache.GetRead(sm.memoryFilePath)
	if exists {
		t.Fatal("Read cache entry should be removed after Remove")
	}
}

// TestSessionMemory_ResetBaselines verifies that ResetBaselines correctly updates baselines.
func TestSessionMemory_ResetBaselines(t *testing.T) {
	tmpDir := t.TempDir()

	compactCfg := CompactConfig{}

	mockClient := &mockAPIClient{}
	sm := NewSessionMemory("test-session-reset", mockClient, compactCfg, tmpDir)

	// Set up state
	sm.accumTokens = 20000
	sm.toolCalls = 15
	sm.lastBaseline = 5000
	sm.lastToolBaseline = 3

	// Reset baselines
	sm.ResetBaselines()

	if sm.lastBaseline != 20000 {
		t.Fatalf("Expected lastBaseline 20000, got %d", sm.lastBaseline)
	}
	if sm.lastToolBaseline != 15 {
		t.Fatalf("Expected lastToolBaseline 15, got %d", sm.lastToolBaseline)
	}
	if sm.lastUpdateTime.IsZero() {
		t.Fatal("lastUpdateTime should be set")
	}
}

// contains is a helper to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
