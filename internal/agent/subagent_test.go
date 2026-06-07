package agent

import (
	"reflect"
	"testing"
)

func TestBuiltinTypes(t *testing.T) {
	types := BuiltinTypes()
	if len(types) != 5 {
		t.Errorf("expected 5 builtin types, got %d", len(types))
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
			expected: []string{"Read", "Glob", "Grep", "Bash"},
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
			expectAbs: []string{"Read", "Write", "Edit", "Glob", "Grep", "WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource"},
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
			expectAbs: []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource"},
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
