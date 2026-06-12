package tool_test

import (
	"context"
	"testing"

	"github.com/ipy/jenny/internal/agent"
	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/tool"
)

// fastClient returns an API client configured to fail fast for testing.
func fastClient() api.Requester {
	client, _ := api.NewClient()
	client.SetRetryConfig(api.RetryConfig{
		MaxRetries:    0,
		Max529Retries: 0,
	})
	return client
}

// ============================================================================
// AC1: No nested named teammates
// ============================================================================

func TestAC1_NamedAgent_CannotSpawnNamedAgent(t *testing.T) {
	// Create an AgentTool with swarms enabled
	agentTool := tool.NewAgentToolWithSwarms(nil, nil, true)

	// Simulate being in a named agent context by using tool.NamedAgentKey
	ctx := context.WithValue(context.Background(), tool.NamedAgentKey, true)

	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
		"name":          "worker1", // Named agent trying to spawn another named agent
	}

	result, err := agentTool.Execute(ctx, input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for nested named agents, got success")
	}
	if result.Content != "nested named agents not allowed" {
		t.Fatalf("expected 'nested named agents not allowed', got %q", result.Content)
	}
}

func TestAC1_NamedAgent_CanSpawnUnnamedAgent(t *testing.T) {
	// Create an AgentTool with swarms enabled
	// We need a mock runner since we're testing the context check, not actual execution
	mockRunner := &mockSubagentRunnerForAC1{}
	agentTool := tool.NewAgentToolWithSwarms(mockRunner, nil, true)

	// Simulate being in a named agent context
	ctx := context.WithValue(context.Background(), tool.NamedAgentKey, true)

	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
		// No "name" field - unnamed subagent from named agent should be allowed
	}

	result, err := agentTool.Execute(ctx, input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The mock runner will fail because it doesn't have actual tools, but that's okay
	// We're checking that the nested named agent check passes (no "nested named agents not allowed" error)
	// The error should be about the subagent execution, not the nested name check
	if result.IsError && result.Content == "nested named agents not allowed" {
		t.Fatalf("unnamed subagent should be allowed from named agent context")
	}
}

// mockSubagentRunnerForAC1 implements SubagentRunner for testing
type mockSubagentRunnerForAC1 struct{}

func (m *mockSubagentRunnerForAC1) RunSubagent(ctx context.Context, params tool.SubagentParams) (*tool.SubagentResult, error) {
	// Return a successful result - we only care that the nested name check passes
	return &tool.SubagentResult{Output: "success"}, nil
}

func (m *mockSubagentRunnerForAC1) GetCapturedStreamConfigInfo() map[string]any {
	return nil
}

// ============================================================================
// AC2: Swarm feature flag gates all team tools
// ============================================================================

func TestAC2_SwarmModeDisabled_NameParameterReturnsError(t *testing.T) {
	// Create an AgentTool with swarms DISABLED (default)
	agentTool := tool.NewAgentTool(nil, nil) // swarmsEnabled = false by default

	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
		"name":          "worker1", // Trying to use name without swarm mode enabled
	}

	result, err := agentTool.Execute(context.Background(), input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error when using name without swarm mode enabled")
	}
	if result.Content != "swarm mode not enabled" {
		t.Fatalf("expected 'swarm mode not enabled', got %q", result.Content)
	}
}

func TestAC2_SwarmModeEnabled_NameParameterAccepted(t *testing.T) {
	// Create an AgentTool with swarms ENABLED
	mockRunner := &mockSubagentRunnerForAC3{}
	agentTool := tool.NewAgentToolWithSwarms(mockRunner, nil, true)

	// Input with name should be accepted (will fail on execution since mock has no real tools)
	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
		"name":          "worker1",
	}

	result, err := agentTool.Execute(context.Background(), input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not be "swarm mode not enabled" error - that means name was accepted
	if result.Content == "swarm mode not enabled" {
		t.Fatalf("name parameter should be accepted when swarm mode is enabled")
	}
	// Verify Name was captured in mock runner params
	if mockRunner.capturedParams.Name != "worker1" {
		t.Errorf("AC2 FAIL: expected Name='worker1', got Name='%s'", mockRunner.capturedParams.Name)
	} else {
		t.Log("AC2 PASS: Name='worker1' was captured in mock runner params")
	}
}

func TestAC2_NameParameterNotInSchemaWhenDisabled(t *testing.T) {
	// Create an AgentTool with swarms DISABLED
	agentTool := tool.NewAgentTool(nil, nil)

	schema := agentTool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map in schema")
	}

	if _, exists := props["name"]; exists {
		t.Fatalf("name parameter should not be in schema when swarm mode is disabled")
	}
}

func TestAC2_NameParameterInSchemaWhenEnabled(t *testing.T) {
	// Create an AgentTool with swarms ENABLED
	agentTool := tool.NewAgentToolWithSwarms(nil, nil, true)

	schema := agentTool.InputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map in schema")
	}

	if _, exists := props["name"]; !exists {
		t.Fatalf("name parameter should be in schema when swarm mode is enabled")
	}
}

// ============================================================================
// AC3: Flat delegation only in headless mode
// ============================================================================

func TestAC3_NamedAgentHasAccessToParentTools(t *testing.T) {
	// This test verifies the structural constraint:
	// When name is set, IsNamedAgent is marked in the child's StreamConfig
	// The tool.NamedAgentKey context value should propagate

	// Use LocalSubagentRunner to verify StreamConfig capture
	readTool := tool.NewReadTool(false, nil)
	runner := agent.NewLocalSubagentRunner([]tool.Tool{readTool}, nil, fastClient())

	// Set parent config with non-zero values before Execute
	parentCfg := agent.StreamConfig{
		MaxBudgetUSD: 1.5,
		MaxBudgetCNY: 10.0,
		MaxTurns:     50,
	}
	runner.SetParentConfig(parentCfg)

	agentTool := tool.NewAgentToolWithSwarms(runner, nil, true)

	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
		"name":          "worker1",
	}

	result, err := agentTool.Execute(context.Background(), input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The runner will fail on execution (no API client), but the structural
	// constraint is that IsNamedAgent was set to true in the child's StreamConfig
	// We verify this by checking that the result is not the nested agent error
	if result.Content == "nested named agents not allowed" {
		t.Fatalf("named agent should be able to execute (flat delegation)")
	}

	// AC2-(5): Verify captured StreamConfig shows IsNamedAgent=true and inherited fields
	cfgInfo := runner.GetCapturedStreamConfigInfo()
	if cfgInfo == nil {
		t.Fatal("AC2-(5) FAIL: expected captured StreamConfig, got nil")
	}

	if !cfgInfo["IsNamedAgent"].(bool) {
		t.Error("AC2-(5) FAIL: IsNamedAgent should be true")
	} else {
		t.Log("AC2-(5) PASS: IsNamedAgent is true")
	}

	// Verify inherited fields match parent config values
	if cfgInfo["MaxBudgetUSD"] != parentCfg.MaxBudgetUSD {
		t.Errorf("AC2-(5) FAIL: MaxBudgetUSD should be inherited, got %v", cfgInfo["MaxBudgetUSD"])
	} else {
		t.Logf("AC2-(5) PASS: MaxBudgetUSD is inherited: %v", cfgInfo["MaxBudgetUSD"])
	}

	if cfgInfo["MaxBudgetCNY"] != parentCfg.MaxBudgetCNY {
		t.Errorf("AC2-(5) FAIL: MaxBudgetCNY should be inherited, got %v", cfgInfo["MaxBudgetCNY"])
	} else {
		t.Logf("AC2-(5) PASS: MaxBudgetCNY is inherited: %v", cfgInfo["MaxBudgetCNY"])
	}

	if cfgInfo["MaxTurns"] != parentCfg.MaxTurns {
		t.Errorf("AC2-(5) FAIL: MaxTurns should be inherited, got %v", cfgInfo["MaxTurns"])
	} else {
		t.Logf("AC2-(5) PASS: MaxTurns is inherited: %v", cfgInfo["MaxTurns"])
	}
}

// mockSubagentRunnerForAC3 captures the params to verify IsNamedAgent was set
type mockSubagentRunnerForAC3 struct {
	capturedParams tool.SubagentParams
}

func (m *mockSubagentRunnerForAC3) RunSubagent(ctx context.Context, params tool.SubagentParams) (*tool.SubagentResult, error) {
	m.capturedParams = params
	// Verify that Name was propagated
	if params.Name != "worker1" {
		return &tool.SubagentResult{Output: "name not propagated"}, nil
	}
	return &tool.SubagentResult{Output: "success"}, nil
}

func (m *mockSubagentRunnerForAC3) GetCapturedStreamConfigInfo() map[string]any {
	return nil
}

// ============================================================================
// Edge cases
// ============================================================================

func TestEdgeCase_EmptyNameParameterIsIgnored(t *testing.T) {
	// Create an AgentTool with swarms DISABLED and a mock runner
	mockRunner := &mockSubagentRunnerForAC1{}
	agentTool := tool.NewAgentToolWithSwarms(mockRunner, nil, false) // swarmsEnabled = false

	// Empty string for name should be treated as no name (unnamed subagent)
	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
		"name":          "", // Empty string - should be ignored
	}

	result, err := agentTool.Execute(context.Background(), input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not be "swarm mode not enabled" because empty name is treated as no name
	if result.Content == "swarm mode not enabled" {
		t.Fatalf("empty name should be treated as unnamed subagent")
	}
}

func TestEdgeCase_ForkChildStillBlocked(t *testing.T) {
	// Even with swarms enabled, recursive fork should still be blocked
	agentTool := tool.NewAgentToolWithSwarms(nil, nil, true)

	// Simulate being in a fork child context
	ctx := context.WithValue(context.Background(), tool.ForkChildKey, true)

	input := map[string]any{
		"prompt":        "test prompt",
		"subagent_type": "explore",
	}

	result, err := agentTool.Execute(ctx, input, "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("fork child should still be blocked even with swarm mode enabled")
	}
	if result.Content != "recursive fork not allowed" {
		t.Fatalf("expected 'recursive fork not allowed', got %q", result.Content)
	}
}
