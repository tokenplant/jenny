package tool

import (
	"testing"
)

func TestBashTool_Execute(t *testing.T) {
	tool := NewBashTool()
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
	tool := NewBashTool()
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
	tool := NewBashTool()
	cwd := "/tmp"

	result, err := tool.Execute(map[string]any{
		"command": "sleep 100",
		"timeout": float64(1),
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
	tool := NewBashTool()

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
