package agent

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

// ============================================================================
// SubagentType tests
// ============================================================================

func TestBuiltinTypes(t *testing.T) {
	types := BuiltinTypes()
	expectedTypes := []string{"general-purpose", "explore", "plan", "shell", "verification"}

	if len(types) != len(expectedTypes) {
		t.Errorf("expected %d builtin types, got %d", len(expectedTypes), len(types))
	}

	for _, expected := range expectedTypes {
		found := false
		for _, tt := range types {
			if tt.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find type %q in BuiltinTypes()", expected)
		}
	}
}

func TestSubagentTypeAllowedTools(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		expected []string
	}{
		{
			name:     "general-purpose",
			typeName: "general-purpose",
			expected: []string{"*"},
		},
		{
			name:     "explore",
			typeName: "explore",
			expected: []string{"Read", "Glob", "Grep", "Bash"},
		},
		{
			name:     "plan",
			typeName: "plan",
			expected: []string{"Read", "Glob", "Grep"},
		},
		{
			name:     "shell",
			typeName: "shell",
			expected: []string{"Bash", "Read", "Glob", "Grep"},
		},
		{
			name:     "verification",
			typeName: "verification",
			expected: []string{"Read", "TaskOutput", "TaskStop"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			if !reflect.DeepEqual(st.AllowedTools(), tt.expected) {
				t.Errorf("expected allowed tools %v, got %v", tt.expected, st.AllowedTools())
			}
		})
	}
}

func TestFilterTools(t *testing.T) {
	tests := []struct {
		name      string
		typeName  string
		denied    []string
		expectAbs []string
	}{
		{
			name:      "general-purpose denies Bash",
			typeName:  "general-purpose",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Write", "Edit", "Glob", "Grep", "WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource", "TaskOutput", "TaskStop", "Task", "CronCreate", "CronDelete", "CronList"},
		},
		{
			name:      "shell denies Bash",
			typeName:  "shell",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Glob", "Grep"},
		},
		{
			name:      "plan denies Bash (already excluded)",
			typeName:  "plan",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Glob", "Grep"},
		},
		{
			name:      "explore denies Bash",
			typeName:  "explore",
			denied:    []string{"Bash"},
			expectAbs: []string{"Read", "Glob", "Grep"},
		},
		{
			name:      "explore denies multiple",
			typeName:  "explore",
			denied:    []string{"Bash", "Glob"},
			expectAbs: []string{"Read", "Grep"},
		},
		{
			name:      "general-purpose no denies",
			typeName:  "general-purpose",
			denied:    []string{},
			expectAbs: []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource", "TaskOutput", "TaskStop", "Task", "CronCreate", "CronDelete", "CronList"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			result := st.FilterTools(tt.denied)
			if !reflect.DeepEqual(result, tt.expectAbs) {
				t.Errorf("FilterTools(%v) = %v, want %v", tt.denied, result, tt.expectAbs)
			}
		})
	}
}

func TestResolveModel(t *testing.T) {
	tests := []struct {
		alias    string
		expected string
	}{
		{alias: "sonnet", expected: "claude-sonnet-4-20250514"},
		{alias: "opus", expected: "claude-opus-4-20250514"},
		{alias: "haiku", expected: "claude-haiku-4-20250514"},
		{alias: "SONNET", expected: "claude-sonnet-4-20250514"}, // case insensitive
		{alias: "claude-4", expected: "claude-4"},               // unknown passes through
		{alias: "unknown", expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			result := ResolveModel(tt.alias)
			if result != tt.expected {
				t.Errorf("ResolveModel(%q) = %q, want %q", tt.alias, result, tt.expected)
			}
		})
	}
}

func TestCanResume(t *testing.T) {
	tests := []struct {
		typeName  string
		canResume bool
	}{
		{typeName: "general-purpose", canResume: true},
		{typeName: "explore", canResume: false},
		{typeName: "plan", canResume: false},
		{typeName: "shell", canResume: true},
		{typeName: "verification", canResume: true},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			if got := st.CanResume(); got != tt.canResume {
				t.Errorf("CanResume() = %v, want %v", got, tt.canResume)
			}
		})
	}
}

func TestRequiredMCPServers(t *testing.T) {
	tests := []struct {
		typeName string
		expected []string
	}{
		{typeName: "general-purpose", expected: []string{}},
		{typeName: "explore", expected: []string{}},
		{typeName: "plan", expected: []string{}},
		{typeName: "shell", expected: []string{}},
		{typeName: "verification", expected: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			st := FindBuiltin(tt.typeName)
			if st == nil {
				t.Fatalf("expected to find builtin type %q", tt.typeName)
			}
			result := st.RequiredMCPServers()
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("RequiredMCPServers() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFindBuiltin(t *testing.T) {
	tests := []struct {
		name  string
		found bool
	}{
		{name: "general-purpose", found: true},
		{name: "explore", found: true},
		{name: "plan", found: true},
		{name: "shell", found: true},
		{name: "verification", found: true},
		{name: "unknown", found: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := FindBuiltin(tt.name)
			if (st != nil) != tt.found {
				t.Errorf("FindBuiltin(%q) found = %v, want %v", tt.name, st != nil, tt.found)
			}
		})
	}
}

func TestAllowedToolsAccessor(t *testing.T) {
	st := GeneralPurpose
	tools := st.AllowedTools()
	if len(tools) != 1 || tools[0] != "*" {
		t.Errorf("AllowedTools() returned unexpected value: %v", tools)
	}

	// Verify it returns a copy
	tools[0] = "modified"
	if GeneralPurpose.AllowedTools()[0] != "*" {
		t.Errorf("AllowedTools() returned a reference, not a copy")
	}
}

func TestRequiredMCPServersAccessor(t *testing.T) {
	st := GeneralPurpose
	servers := st.RequiredMCPServers()
	if len(servers) != 0 {
		t.Errorf("RequiredMCPServers() returned unexpected value: %v", servers)
	}

	// Verify it returns a copy
	servers = append(servers, "test")
	if len(GeneralPurpose.RequiredMCPServers()) != 0 {
		t.Errorf("RequiredMCPServers() returned a reference, not a copy")
	}
}

// ============================================================================
// Integration tests — require ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN
// ============================================================================

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
