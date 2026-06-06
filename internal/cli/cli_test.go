package cli

import (
	"os"
	"testing"
)

func TestParseNoArgs(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny"}

	flags, err := Parse()
	if err == nil {
		t.Error("expected error for no prompt")
	}
	if flags != nil {
		t.Error("expected nil flags on error")
	}
}

func TestParsePositionalArg(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "hello world"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello world" {
		t.Errorf("expected prompt 'hello world', got %q", flags.Prompt)
	}
}

func TestParsePFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "hello from -p"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello from -p" {
		t.Errorf("expected prompt 'hello from -p', got %q", flags.Prompt)
	}
}

func TestParseModelFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--model", "deepseek-v4-flash", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Model != "deepseek-v4-flash" {
		t.Errorf("expected model 'deepseek-v4-flash', got %q", flags.Model)
	}
}

func TestParseOutputFormatFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--output-format", "stream-json", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.OutputFormat != "stream-json" {
		t.Errorf("expected output-format 'stream-json', got %q", flags.OutputFormat)
	}
}

func TestParseVerboseFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--verbose", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.Verbose {
		t.Error("expected verbose to be true")
	}
}

func TestParseIncludePartialMessagesFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--include-partial-messages", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.IncludePartialMessages {
		t.Error("expected include-partial-messages to be true")
	}
}

func TestParseSkipPermissionsFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--dangerously-skip-permissions", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !flags.SkipPermissions {
		t.Error("expected skip-permissions to be true")
	}
}

func TestParseSessionResumeFlag(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-r", "sess_12345", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.SessionResume != "sess_12345" {
		t.Errorf("expected session-resume 'sess_12345', got %q", flags.SessionResume)
	}
}

func TestParseMultipleFlags(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--model", "gpt-4", "--output-format", "stream-json", "--verbose", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", flags.Model)
	}
	if flags.OutputFormat != "stream-json" {
		t.Errorf("expected output-format 'stream-json', got %q", flags.OutputFormat)
	}
	if !flags.Verbose {
		t.Error("expected verbose to be true")
	}
	if flags.Prompt != "hello" {
		t.Errorf("expected prompt 'hello', got %q", flags.Prompt)
	}
}

func TestParsePositionalWithPFlag(t *testing.T) {
	// When both -p and positional arg are provided, -p takes precedence
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "from -p", "from positional"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "from -p" {
		t.Errorf("expected prompt 'from -p' (p flag takes precedence), got %q", flags.Prompt)
	}
}

func TestParseDoubleDash(t *testing.T) {
	// Save original args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--", "hello world"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if flags.Prompt != "hello world" {
		t.Errorf("expected prompt 'hello world', got %q", flags.Prompt)
	}
}

func TestParseMCPConfigSingleFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--mcp-config", "/path/to/config.json", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) != 1 {
		t.Errorf("expected 1 MCPConfig path, got %d", len(flags.MCPConfig))
	}
	if flags.MCPConfig[0] != "/path/to/config.json" {
		t.Errorf("expected MCPConfig '/path/to/config.json', got %q", flags.MCPConfig[0])
	}
}

func TestParseMCPConfigMultipleFlags(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "--mcp-config", "/path/a.json", "--mcp-config", "/path/b.json", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) != 2 {
		t.Errorf("expected 2 MCPConfig paths, got %d", len(flags.MCPConfig))
	}
	if flags.MCPConfig[0] != "/path/a.json" {
		t.Errorf("expected MCPConfig[0] '/path/a.json', got %q", flags.MCPConfig[0])
	}
	if flags.MCPConfig[1] != "/path/b.json" {
		t.Errorf("expected MCPConfig[1] '/path/b.json', got %q", flags.MCPConfig[1])
	}
}

func TestParseMCPConfigNoFlag(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"jenny", "-p", "hello"}

	flags, err := Parse()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(flags.MCPConfig) > 0 {
		t.Errorf("expected nil or empty MCPConfig, got %v", flags.MCPConfig)
	}
}
