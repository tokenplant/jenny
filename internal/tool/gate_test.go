package tool

import (
	"strings"
	"testing"
)

func TestCommandGate_CheckCommand(t *testing.T) {
	gate := NewCommandGate(false)

	tests := []struct {
		name    string
		command string
		wantErr bool
		errMsg  string
	}{
		// AC1: Command substitution blocked
		{
			name:    "command substitution with $(cat /etc/passwd)",
			command: "$(cat /etc/passwd)",
			wantErr: true,
			errMsg:  "command substitution",
		},
		{
			name:    "variable substitution with ${HOME}",
			command: "${HOME}",
			wantErr: true,
			errMsg:  "command substitution",
		},
		{
			name:    "backtick command substitution",
			command: "`ls`",
			wantErr: true,
			errMsg:  "backtick command substitution",
		},
		{
			name:    "echo hello without substitution",
			command: "echo hello",
			wantErr: false,
		},
		{
			name:    "ls without substitution",
			command: "ls",
			wantErr: false,
		},

		// Process/zsh substitution blocked
		{
			name:    "process substitution <()",
			command: "cat <(echo test)",
			wantErr: true,
			errMsg:  "process substitution",
		},
		{
			name:    "process substitution >()",
			command: "echo test >()",
			wantErr: true,
			errMsg:  "process substitution",
		},
		{
			name:    "zsh style =()",
			command: "=(echo test)",
			wantErr: true,
			errMsg:  "zsh style command execution",
		},
		{
			name:    "equals cmd pattern",
			command: "=cmd",
			wantErr: true,
			errMsg:  "command alias pattern",
		},
		{
			name:    "bash arithmetic $[...]",
			command: "$[1+1]",
			wantErr: true,
			errMsg:  "arithmetic expansion",
		},
		{
			name:    "zsh globbing ~[]",
			command: "ls ~[1]",
			wantErr: true,
			errMsg:  "zsh globbing",
		},

		// Carriage return smuggling
		{
			name:    "carriage return smuggling",
			command: "ls\rrm -rf /",
			wantErr: true,
			errMsg:  "carriage return",
		},

		// AC3: Git injection blocked
		{
			name:    "git -c injection",
			command: "git -c core.gitProxy=evil log",
			wantErr: true,
			errMsg:  "git config injection",
		},
		{
			name:    "git --exec-path injection",
			command: "git --exec-path=/tmp/evil diff",
			wantErr: true,
			errMsg:  "git --exec-path injection",
		},
		{
			name:    "git --config-env injection",
			command: "git --config-env=/tmp/evil log",
			wantErr: true,
			errMsg:  "git --config-env injection",
		},
		{
			name:    "git log allowed",
			command: "git log --oneline -5",
			wantErr: false,
		},
		{
			name:    "git diff allowed",
			command: "git diff",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gate.CheckCommand(tt.command)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCommandGate_CheckCommand_WithSkipPermissions(t *testing.T) {
	gate := NewCommandGate(true) // skipPermissions = true

	// All dangerous commands should be allowed when skipPermissions is true
	dangerousCommands := []string{
		"$(cat /etc/passwd)",
		"${HOME}",
		"`ls`",
		"git -c core.gitProxy=evil log",
		"cat /dev/urandom",
	}

	for _, cmd := range dangerousCommands {
		t.Run(cmd, func(t *testing.T) {
			err := gate.CheckCommand(cmd)
			if err != nil {
				t.Errorf("expected no error with skipPermissions=true, got: %v", err)
			}
		})
	}
}

func TestCommandGate_CheckPipelineSegments(t *testing.T) {
	gate := NewCommandGate(false)

	tests := []struct {
		name    string
		command string
		wantErr bool
		errMsg  string
	}{
		// AC2: Read-only pipeline validation
		{
			name:    "pipeline with mutating final segment",
			command: "echo ok | rm -rf /",
			wantErr: true,
			errMsg:  "not allowed",
		},
		{
			name:    "all read-only pipeline",
			command: "ls -la | grep foo",
			wantErr: false,
		},
		{
			name:    "single read-only command",
			command: "ls",
			wantErr: false,
		},
		{
			name:    "pipeline with output redirect",
			command: "cat file > /tmp/out",
			wantErr: true,
			errMsg:  "output redirection",
		},
		{
			name:    "pipeline with append redirect",
			command: "echo text >> /tmp/out",
			wantErr: true,
			errMsg:  "output redirection",
		},
		{
			name:    "cat to grep pipeline",
			command: "cat /etc/passwd | grep root",
			wantErr: false,
		},
		{
			name:    "find to head pipeline",
			command: "find /tmp -name '*.txt' | head -n 5",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gate.CheckPipelineSegments(tt.command)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCommandGate_CheckPipelineSegments_WithSkipPermissions(t *testing.T) {
	gate := NewCommandGate(true)

	// All pipelines should be allowed when skipPermissions is true
	pipelines := []string{
		"echo ok | rm -rf /",
		"cat file > /tmp/out",
	}

	for _, cmd := range pipelines {
		t.Run(cmd, func(t *testing.T) {
			err := gate.CheckPipelineSegments(cmd)
			if err != nil {
				t.Errorf("expected no error with skipPermissions=true, got: %v", err)
			}
		})
	}
}

func TestCommandGate_CheckDevicePath(t *testing.T) {
	gate := NewCommandGate(false)

	tests := []struct {
		name    string
		path    string
		wantErr bool
		errMsg  string
	}{
		// AC5: Device paths blocked
		{
			name:    "/dev/urandom blocked",
			path:    "/dev/urandom",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/zero blocked",
			path:    "/dev/zero",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/random blocked",
			path:    "/dev/random",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/full blocked",
			path:    "/dev/full",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/stdin blocked",
			path:    "/dev/stdin",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/stdout blocked",
			path:    "/dev/stdout",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/stderr blocked",
			path:    "/dev/stderr",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/fd/0 blocked",
			path:    "/dev/fd/0",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/dev/fd/1 blocked",
			path:    "/dev/fd/1",
			wantErr: true,
			errMsg:  "device path",
		},
		{
			name:    "/proc/self/environ blocked",
			path:    "/proc/self/environ",
			wantErr: true,
			errMsg:  "blocked",
		},
		{
			name:    "/proc/1/environ blocked",
			path:    "/proc/1/environ",
			wantErr: true,
			errMsg:  "blocked",
		},
		{
			name:    "normal file allowed",
			path:    "/tmp/test.txt",
			wantErr: false,
		},
		{
			name:    "normal path allowed",
			path:    "/usr/bin/ls",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gate.CheckDevicePath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCommandGate_CheckDevicePath_WithSkipPermissions(t *testing.T) {
	gate := NewCommandGate(true)

	// All device paths should be allowed when skipPermissions is true
	paths := []string{
		"/dev/urandom",
		"/dev/zero",
		"/proc/1/environ",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			err := gate.CheckDevicePath(path)
			if err != nil {
				t.Errorf("expected no error with skipPermissions=true, got: %v", err)
			}
		})
	}
}

func TestIsSegmentReadOnly(t *testing.T) {
	tests := []struct {
		segment string
		want    bool
	}{
		{"ls", true},
		{"ls -la", true},
		{"pwd", true},
		{"cat /etc/passwd", true},
		{"grep root /tmp/passwd", true},
		{"head -n 5 /tmp/file", true},
		{"tail -n 5 /tmp/file", true},
		{"find /tmp -name '*.txt'", true},
		{"wc -l /tmp/file", true},
		{"diff file1 file2", true},
		{"rm /tmp/file", false},
		{"rm -rf /", false},
		{"touch /tmp/file", false},
		{"echo hello > /tmp/file", false},
		{"chmod 777 /tmp/file", false},
		{"mv a b", false},
		{"cp a b", false},
	}

	for _, tt := range tests {
		t.Run(tt.segment, func(t *testing.T) {
			if got := isSegmentReadOnly(tt.segment); got != tt.want {
				t.Errorf("isSegmentReadOnly(%q) = %v, want %v", tt.segment, got, tt.want)
			}
		})
	}
}

// TestAC5_SecurityGateErrorMessages verifies error messages use "for security reasons"
// instead of "in read-only mode".
func TestAC5_SecurityGateErrorMessages(t *testing.T) {
	gate := NewCommandGate(false)

	// Redirection should say "for security reasons"
	err := gate.CheckPipelineSegments("cat file > /tmp/out")
	if err == nil {
		t.Fatal("expected error for output redirection")
	}
	if strings.Contains(err.Error(), "read-only mode") {
		t.Errorf("error should not mention 'read-only mode', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "for security reasons") {
		t.Errorf("error should mention 'for security reasons', got: %s", err.Error())
	}

	// Non-allowlisted command should say "for security reasons"
	err = gate.CheckPipelineSegments("rm -rf /")
	if err == nil {
		t.Fatal("expected error for non-allowlisted command")
	}
	if strings.Contains(err.Error(), "read-only mode") {
		t.Errorf("error should not mention 'read-only mode', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "for security reasons") {
		t.Errorf("error should mention 'for security reasons', got: %s", err.Error())
	}
}
