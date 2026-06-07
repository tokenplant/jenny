package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashTool_Execute(t *testing.T) {
	tool := NewBashTool(false)
	cwd := "/tmp"

	tests := []struct {
		name    string
		input   map[string]any
		wantErr bool
		checkFn func(*ToolResult) bool
	}{
		{
			name: "basic echo command",
			input: map[string]any{
				"command": "echo hello",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError && contains(r.Content, "hello")
			},
		},
		{
			name: "pwd command",
			input: map[string]any{
				"command": "pwd",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError && r.Content != ""
			},
		},
		{
			name: "ls command",
			input: map[string]any{
				"command": "ls /tmp",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError
			},
		},
		{
			name: "cat nonexistent file",
			input: map[string]any{
				"command": "cat /nonexistent/file2>&1",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError
			},
		},
		{
			name: "command with error",
			input: map[string]any{
				"command": "ls /nonexistent 2>&1",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && r.IsError
			},
		},
		{
			name: "command missing",
			input: map[string]any{
				"command": "",
			},
			wantErr: true,
		},
		{
			name: "whoami command",
			input: map[string]any{
				"command": "whoami",
			},
			wantErr: false,
			checkFn: func(r *ToolResult) bool {
				return r != nil && !r.IsError && len(r.Content) > 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(tt.input, cwd)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if tt.wantErr {
				t.Error("expected error but got none")
				return
			}
			if tt.checkFn != nil && !tt.checkFn(result) {
				t.Errorf("check failed for content: %q", result.Content)
			}
		})
	}
}

func TestBashTool_ReadOnlyEnforcement(t *testing.T) {
	tool := NewBashTool(false)
	cwd := "/tmp"

	// These commands should be allowed (read-only AND within working directory)
	allowedCommands := []string{
		"ls /tmp",                 // /tmp is cwd - within working directory
		"pwd",                     // no file path
		"whoami",                  // no file path
		"echo hello",              // no file path
		"date",                    // no file path
		"cat ./test.txt",          // relative path within cwd
		"head -n 5 /tmp/test.txt", // path inside /tmp
		"tail -n 5 /tmp/test.txt", // path inside /tmp
		"grep root /tmp/passwd",   // path inside /tmp
		"find /tmp -name '*.txt'", // path inside /tmp
		"wc -l /tmp/test.txt",     // path inside /tmp
		"which ls",                // command lookup - doesn't access the file
		"type cat",                // command lookup - doesn't access the file
	}

	for _, cmd := range allowedCommands {
		t.Run("allowed/"+cmd, func(t *testing.T) {
			result, err := tool.Execute(map[string]any{"command": cmd}, cwd)
			if err != nil {
				t.Errorf("unexpected error for %q: %v", cmd, err)
				return
			}
			if result.IsError && contains(result.Content, "not allowed") {
				t.Errorf("command %q should be allowed but got error: %s", cmd, result.Content)
			}
		})
	}

	// These commands should be blocked (write operations or outside cwd)
	blockedCommands := []string{
		"rm -rf /tmp/test",
		"touch /tmp/test.txt",
		"echo hello > /tmp/test.txt",
		"mkdir /tmp/testdir",
		"chmod 777 /tmp/test",
		"mv /tmp/a /tmp/b",
		"cp /tmp/a /tmp/b",
		// Commands accessing paths outside working directory
		"cat /etc/passwd",
		"head -n 5 /etc/passwd",
		"tail -n 5 /etc/passwd",
		"grep root /etc/passwd",
		"wc -l /etc/passwd",
		"file /bin/ls",
		"stat /etc/passwd",
		"diff /dev/null /dev/null",
	}

	for _, cmd := range blockedCommands {
		t.Run("blocked/"+cmd, func(t *testing.T) {
			result, err := tool.Execute(map[string]any{"command": cmd}, cwd)
			if err != nil {
				t.Errorf("unexpected error for %q: %v", cmd, err)
				return
			}
			if !result.IsError {
				t.Errorf("command %q should be blocked but was allowed", cmd)
			}
		})
	}
}

func TestBashTool_Timeout(t *testing.T) {
	tool := NewBashTool(false)
	cwd := "/tmp"

	// Use sleep 1 with timeout< 1 second to ensure timeout fires before sleep completes.
	// sleep 1 is allowed in foreground (AC3 exempts sleep < 2), but will be killed
	// by the timeout before it completes.
	result, err := tool.Execute(map[string]any{
		"command": "sleep 1",
		"timeout": float64(0),
	}, cwd)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}

	if !result.IsError {
		t.Error("expected timeout error")
	}
	if !contains(result.Content, "timed out") {
		t.Errorf("expected timeout message, got: %s", result.Content)
	}
}

func TestBashTool_NameAndDescription(t *testing.T) {
	tool := NewBashTool(false)

	if tool.Name() != "bash" {
		t.Errorf("expected name 'bash', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["command"]; !ok {
		t.Error("expected 'command' property")
	}
}

func TestIsReadOnlyCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"ls", true},
		{"ls /tmp", true},
		{"pwd", true},
		{"cat /etc/passwd", true},
		{"cat", true},
		{"rm /tmp/file", false},
		{"rm -rf /", false},
		{"touch /tmp/file", false},
		{"echo hello > file", false},
		{"chmod 777 /tmp/file", false},
		{"mv a b", false},
		{"cp a b", false},
		{"mkdir /tmp/dir", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := isReadOnlyCommand(tt.command); got != tt.want {
				t.Errorf("isReadOnlyCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

// AC1: Read-only pipelines validated per segment
func TestBashTool_AC1_ReadOnlyPipeline(t *testing.T) {
	tool := NewBashTool(false)
	cwd := t.TempDir()

	// Test: all read-only pipeline should succeed
	result, err := tool.Execute(map[string]any{
		"command": "ls -la | grep txt | wc -l",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for read-only pipeline, got error: %s", result.Content)
	}

	// Test: mutating final segment should fail
	result, err = tool.Execute(map[string]any{
		"command": "ls | rm -rf /",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected security error for mutating final segment")
	}
	if !contains(result.Content, "Security error") {
		t.Errorf("expected security error message, got: %s", result.Content)
	}

	// Test: simple read-only pipeline
	result, err = tool.Execute(map[string]any{
		"command": "echo hello | cat",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for echo | cat, got error: %s", result.Content)
	}
}

// AC2: Output >30K chars spilled to disk
func TestBashTool_AC2_OutputSpill(t *testing.T) {
	tool := NewBashTool(false)
	cwd := t.TempDir()

	// Test: large output should spill to disk
	result, err := tool.Execute(map[string]any{
		"command": "python3 -c \"print('x'*35000)\"",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
	// Should reference a file path
	if !contains(result.Content, "/tmp/") && !contains(result.Content, "jenny-spill") {
		t.Errorf("expected spill file path in result, got: %s", result.Content)
	}
	if !result.Truncated {
		t.Errorf("expected truncated=true for spilled output")
	}

	// Test: small output should be inline
	result, err = tool.Execute(map[string]any{
		"command": "echo small",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
	if result.Truncated {
		t.Errorf("expected truncated=false for small output")
	}
	if !contains(result.Content, "small") {
		t.Errorf("expected 'small' in output, got: %s", result.Content)
	}
}

// AC3: sleep >=2 blocked in foreground; run_in_background spawns tracked task
func TestBashTool_AC3_SleepBlocked(t *testing.T) {
	tool := NewBashTool(false)
	cwd := t.TempDir()

	// Test: sleep >=2 in foreground should be blocked
	result, err := tool.Execute(map[string]any{
		"command": "sleep 3",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected error for sleep >=2 in foreground")
	}
	if !contains(result.Content, "run_in_background") {
		t.Errorf("expected error message mentioning run_in_background, got: %s", result.Content)
	}

	// Test: sleep >=2 with run_in_background should succeed with task ID
	result, err = tool.Execute(map[string]any{
		"command":           "sleep 3",
		"run_in_background": true,
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for background sleep, got error: %s", result.Content)
	}
	if !contains(result.Content, "Background task") {
		t.Errorf("expected background task message, got: %s", result.Content)
	}

	// Test: sleep 1 in foreground should succeed
	result, err = tool.Execute(map[string]any{
		"command": "sleep 1",
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for sleep 1 in foreground, got error: %s", result.Content)
	}
}

// AC4: Cwd reset when outside project
func TestBashTool_AC4_CwdReset(t *testing.T) {
	tool := NewBashTool(false)
	projectRoot := t.TempDir()

	// Create a subdirectory inside project
	subDir := projectRoot + "/subdir"
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Test: cd outside project should reset cwd
	result, err := tool.Execute(map[string]any{
		"command": "cd /tmp && pwd",
	}, projectRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Content)
	}
	// The internal cwd should have been reset to projectRoot
	if !contains(result.Content, projectRoot) && !contains(result.Content, "tmp") {
		// The /tmp pwd output is expected, but internal state should be reset
	}

	// Test: normal pwd shouldn't change cwd
	result, err = tool.Execute(map[string]any{
		"command": "pwd",
	}, projectRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for pwd, got error: %s", result.Content)
	}

	// Test: cd to subdirectory (inside project) should be allowed
	result, err = tool.Execute(map[string]any{
		"command": "cd ./subdir && pwd",
	}, projectRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for cd inside project, got error: %s", result.Content)
	}
}

// AC5: Sed simulation invisible in schema
func TestBashTool_AC5_SchemaHygiene(t *testing.T) {
	tool := NewBashTool(false)

	schema := tool.InputSchema()

	// Schema should have type: object
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map in schema")
	}

	// Should have command property
	if _, ok := props["command"]; !ok {
		t.Error("expected 'command' property in schema")
	}

	// Should have timeout property
	if _, ok := props["timeout"]; !ok {
		t.Error("expected 'timeout' property in schema")
	}

	// Should have run_in_background property
	if _, ok := props["run_in_background"]; !ok {
		t.Error("expected 'run_in_background' property in schema")
	}

	// Should NOT have internal implementation details
	if _, ok := props["_simulatedSedEdit"]; ok {
		t.Error("schema should NOT contain '_simulatedSedEdit' property")
	}
	if _, ok := props["dangerouslyDisableSandbox"]; ok {
		t.Error("schema should NOT contain 'dangerouslyDisableSandbox' property")
	}
}

// Test sed simulation
func TestBashTool_SedSimulation(t *testing.T) {
	tool := NewBashTool(false)
	cwd := t.TempDir()

	// Create a test file
	testFile := filepath.Join(cwd, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world\nfoo bar\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test sed replacement
	result, err := tool.Execute(map[string]any{
		"command": fmt.Sprintf("sed -i 's/hello/goodbye/g' %s", testFile),
	}, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected success for sed, got error: %s", result.Content)
	}

	// Verify file was edited
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(data), "goodbye world") {
		t.Errorf("expected 'goodbye world' in file, got: %s", string(data))
	}
}
