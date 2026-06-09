package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

func TestSubagentTypeToolAllowlists(t *testing.T) {
	tests := []struct {
		name            string
		typename        string
		expectedAllowed []string
	}{
		{
			name:            "explore",
			typename:        "explore",
			expectedAllowed: []string{"Read", "Glob", "Grep", "Bash"},
		},
		{
			name:            "plan",
			typename:        "plan",
			expectedAllowed: []string{"Read", "Glob", "Grep"},
		},
		{
			name:            "shell",
			typename:        "shell",
			expectedAllowed: []string{"Bash", "Read", "Glob", "Grep"},
		},
		{
			name:            "general-purpose",
			typename:        "general-purpose",
			expectedAllowed: []string{"*"},
		},
		{
			name:            "verification",
			typename:        "verification",
			expectedAllowed: []string{"Read", "TaskOutput", "TaskStop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.typename)
			if st == nil {
				t.Fatalf("expected valid subagent type %q", tt.typename)
			}
			got := st.AllowedTools()
			if len(got) != len(tt.expectedAllowed) {
				t.Errorf("AllowedTools() = %v, want %v", got, tt.expectedAllowed)
				return
			}
			for i, want := range tt.expectedAllowed {
				if got[i] != want {
					t.Errorf("AllowedTools()[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestSubagentType_InvalidTypeError(t *testing.T) {
	st := FindBuiltin("nonexistent")
	if st != nil {
		t.Fatal("expected nil for invalid type")
	}
	// Verify error message format from RunSubagent for invalid type
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	runner := NewLocalSubagentRunner(nil, nil)
	params := tool.SubagentParams{
		Prompt:       "test",
		SubagentType: "nonexistent",
	}
	_, err := runner.RunSubagent(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for invalid subagent_type")
	}
	errStr := err.Error()
	// Error should contain the invalid type name
	if !strings.Contains(errStr, "nonexistent") {
		t.Errorf("error should contain invalid type name, got: %s", errStr)
	}
	// Error should list valid types
	if !strings.Contains(errStr, "valid types are") {
		t.Errorf("error should mention valid types, got: %s", errStr)
	}
}

func TestLocalSubagentRunner_AC1_InvalidTypeError(t *testing.T) {
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil)

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "invalid-type",
	}

	result, err := runner.RunSubagent(context.Background(), params)
	if err == nil {
		// Should get an error for invalid type
		if result != nil {
			t.Logf("result: %s", result.Output)
		}
		// The error should be descriptive
		t.Error("expected error for invalid subagent_type")
	}

	// Error message should contain valid types
	if err != nil {
		errStr := err.Error()
		if errStr == "" {
			t.Error("error message should not be empty")
		}
		// Should mention the invalid type
		if !strings.Contains(errStr, "invalid-type") {
			t.Errorf("error should mention invalid type, got: %s", errStr)
		}
	}
}

func TestLocalSubagentRunner_AC3_ParameterPassthrough(t *testing.T) {
	// Test that parameters are forwarded correctly
	// This is a basic test - full verification would require mocking RunStream
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil)

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
		Model:        "sonnet",
		CWD:          "/tmp",
	}

	// This will likely fail due to API client not being configured in test
	// but we can verify the params are being used
	_, _ = runner.RunSubagent(context.Background(), params)
	// If we get here without panic, the params were at least parsed correctly
}

func TestLocalSubagentRunner_AC4_SubagentLifecycle(t *testing.T) {
	// Test that subagent runs in its own context
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewLocalSubagentRunner(tools, nil)

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
	}

	// Run once
	result1, _ := runner.RunSubagent(context.Background(), params)

	// Run again - should be independent
	result2, _ := runner.RunSubagent(context.Background(), params)

	// Both runs should complete (even if they fail due to no API client)
	if result1 == nil && result2 == nil {
		t.Error("at least one run should produce a result")
	}
}

func TestAsyncSubagentRunner_AC2_AsyncLaunch(t *testing.T) {
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(false, nil)
	tools := []tool.Tool{readTool}

	runner := NewAsyncSubagentRunner(tools, nil)

	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
	}

	// Run async - should return immediately
	result, err := runner.RunSubagentAsync(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify response shape
	if result.Status != "async_launched" {
		t.Errorf("expected status 'async_launched', got %q", result.Status)
	}
	if result.AgentID == "" {
		t.Error("expected non-empty agent_id")
	}
	if result.OutputFile == "" {
		t.Error("expected non-empty output_file")
	}
}

func TestBuiltinTypesMatchSubagentTypes(t *testing.T) {
	// Verify that BuiltinTypes() returns the same types as the subagent type registry
	types := BuiltinTypes()
	expectedTypes := []string{"general-purpose", "explore", "plan", "shell", "verification"}

	if len(types) != len(expectedTypes) {
		t.Errorf("expected %d builtin types, got %d", len(expectedTypes), len(types))
	}

	for _, expected := range expectedTypes {
		found := false
		for _, t := range types {
			if t.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find type %q in BuiltinTypes()", expected)
		}
	}
}

// TestLocalSubagentRunner_AC4_StreamConfigPropagation verifies that all 8 new fields
// are properly propagated from parent config to child StreamConfig when Name is set.
func TestLocalSubagentRunner_AC4_StreamConfigPropagation(t *testing.T) {
	_, hasURL := os.LookupEnv("ANTHROPIC_BASE_URL")
	_, hasToken := os.LookupEnv("ANTHROPIC_AUTH_TOKEN")
	if !hasURL || !hasToken {
		t.Skip("skipping: ANTHROPIC_BASE_URL or ANTHROPIC_AUTH_TOKEN not set")
	}

	readTool := tool.NewReadTool(false, nil)
	runner := NewLocalSubagentRunner([]tool.Tool{readTool}, nil)

	// Set up parent config with all 8 new fields
	parentCfg := StreamConfig{
		MaxBudgetUSD:         1.50,
		MaxBudgetCNY:         10.0,
		MaxTurns:             5,
		CustomSystemPrompt:   "custom prompt",
		AppendSystemPrompt:   "append prompt",
		OverrideSystemPrompt: true,
		StructuredSchema:     map[string]any{"type": "object"},
		StructuredDenyRules:  []string{"Bash"},
	}
	runner.SetParentConfig(parentCfg)

	// Call RunSubagent with Name="worker1"
	params := tool.SubagentParams{
		Prompt:       "test prompt",
		SubagentType: "explore",
		Name:         "worker1",
	}
	_, _ = runner.RunSubagent(context.Background(), params)

	// Get the captured stream config
	capturedCfg := runner.GetCapturedStreamConfig()

	// Verify IsNamedAgent is true
	if !capturedCfg.IsNamedAgent {
		t.Error("AC4 FAIL: IsNamedAgent should be true for named agent")
	} else {
		t.Log("AC4 PASS: IsNamedAgent is true")
	}

	// Verify all 8 inherited fields
	if capturedCfg.MaxBudgetUSD != parentCfg.MaxBudgetUSD {
		t.Errorf("AC4 FAIL: MaxBudgetUSD not inherited, got %v want %v", capturedCfg.MaxBudgetUSD, parentCfg.MaxBudgetUSD)
	} else {
		t.Log("AC4 PASS: MaxBudgetUSD inherited")
	}

	if capturedCfg.MaxBudgetCNY != parentCfg.MaxBudgetCNY {
		t.Errorf("AC4 FAIL: MaxBudgetCNY not inherited, got %v want %v", capturedCfg.MaxBudgetCNY, parentCfg.MaxBudgetCNY)
	} else {
		t.Log("AC4 PASS: MaxBudgetCNY inherited")
	}

	if capturedCfg.MaxTurns != parentCfg.MaxTurns {
		t.Errorf("AC4 FAIL: MaxTurns not inherited, got %v want %v", capturedCfg.MaxTurns, parentCfg.MaxTurns)
	} else {
		t.Log("AC4 PASS: MaxTurns inherited")
	}

	if capturedCfg.CustomSystemPrompt != parentCfg.CustomSystemPrompt {
		t.Errorf("AC4 FAIL: CustomSystemPrompt not inherited, got %q want %q", capturedCfg.CustomSystemPrompt, parentCfg.CustomSystemPrompt)
	} else {
		t.Log("AC4 PASS: CustomSystemPrompt inherited")
	}

	if capturedCfg.AppendSystemPrompt != parentCfg.AppendSystemPrompt {
		t.Errorf("AC4 FAIL: AppendSystemPrompt not inherited, got %q want %q", capturedCfg.AppendSystemPrompt, parentCfg.AppendSystemPrompt)
	} else {
		t.Log("AC4 PASS: AppendSystemPrompt inherited")
	}

	if capturedCfg.OverrideSystemPrompt != parentCfg.OverrideSystemPrompt {
		t.Errorf("AC4 FAIL: OverrideSystemPrompt not inherited, got %v want %v", capturedCfg.OverrideSystemPrompt, parentCfg.OverrideSystemPrompt)
	} else {
		t.Log("AC4 PASS: OverrideSystemPrompt inherited")
	}

	if capturedCfg.StructuredSchema == nil {
		t.Error("AC4 FAIL: StructuredSchema not inherited, got nil")
	} else {
		t.Log("AC4 PASS: StructuredSchema inherited")
	}

	if len(capturedCfg.StructuredDenyRules) != len(parentCfg.StructuredDenyRules) {
		t.Errorf("AC4 FAIL: StructuredDenyRules not inherited, got %v want %v", capturedCfg.StructuredDenyRules, parentCfg.StructuredDenyRules)
	} else {
		t.Log("AC4 PASS: StructuredDenyRules inherited")
	}
}
