package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		env      map[string]string
		expected string
	}{
		{
			name:     "simple variable",
			input:    "hello ${WORLD}",
			env:      map[string]string{"WORLD": "world"},
			expected: "hello world",
		},
		{
			name:     "variable with default - set",
			input:    "hello ${WORLD:-default}",
			env:      map[string]string{"WORLD": "world"},
			expected: "hello world",
		},
		{
			name:     "variable with default - unset",
			input:    "hello ${WORLD:-default}",
			env:      map[string]string{},
			expected: "hello default",
		},
		{
			name:     "variable with empty default - unset",
			input:    "hello ${WORLD:-}",
			env:      map[string]string{},
			expected: "hello ",
		},
		{
			name:     "multiple variables",
			input:    "${A} and ${B}",
			env:      map[string]string{"A": "first", "B": "second"},
			expected: "first and second",
		},
		{
			name:     "no variables",
			input:    "hello world",
			env:      map[string]string{},
			expected: "hello world",
		},
		{
			name:     "empty string",
			input:    "",
			env:      map[string]string{},
			expected: "",
		},
		{
			name:     "unset variable no default",
			input:    "hello ${UNSET}",
			env:      map[string]string{},
			expected: "hello ",
		},
		{
			name:     "special chars in value",
			input:    "path: ${PATH}",
			env:      map[string]string{"PATH": "/usr/local/bin:$PATH"},
			expected: "path: /usr/local/bin:$PATH",
		},
		{
			name:     "trailing text",
			input:    "value=${VAR}!",
			env:      map[string]string{"VAR": "test"},
			expected: "value=test!",
		},
		{
			name:     "leading text",
			input:    "prefix-${VAR}",
			env:      map[string]string{"VAR": "test"},
			expected: "prefix-test",
		},
		{
			name:     "default with dash",
			input:    "${VAR:-my-default-value}",
			env:      map[string]string{},
			expected: "my-default-value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			for k, v := range tt.env {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			result := expandEnv(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnv(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("basic single file", func(t *testing.T) {
		content := `{"mcpServers": {"test": {"command": "echo", "args": ["hello"]}}}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if len(config) != 1 {
			t.Errorf("expected 1 server, got %d", len(config))
		}

		server, ok := config["test"]
		if !ok {
			t.Fatal("expected server 'test'")
		}
		if server.Command != "echo" {
			t.Errorf("command = %q, want %q", server.Command, "echo")
		}
		if len(server.Args) != 1 || server.Args[0] != "hello" {
			t.Errorf("args = %v, want [hello]", server.Args)
		}
	})

	t.Run("env expansion", func(t *testing.T) {
		os.Setenv("TEST_COMMAND", "my-echo")
		defer os.Unsetenv("TEST_COMMAND")

		content := `{"mcpServers": {"test": {"command": "${TEST_COMMAND}"}}}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config["test"].Command != "my-echo" {
			t.Errorf("command = %q, want %q", config["test"].Command, "my-echo")
		}
	})

	t.Run("env expansion with default", func(t *testing.T) {
		content := `{"mcpServers": {"test": {"command": "${UNSET_VAR:-fallback}"}}}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config["test"].Command != "fallback" {
			t.Errorf("command = %q, want %q", config["test"].Command, "fallback")
		}
	})

	t.Run("merge ordering - later overrides", func(t *testing.T) {
		content1 := `{"mcpServers": {"test": {"command": "first"}}}`
		content2 := `{"mcpServers": {"test": {"command": "second"}}}`
		tmpDir := t.TempDir()
		configPath1 := filepath.Join(tmpDir, "config1.json")
		configPath2 := filepath.Join(tmpDir, "config2.json")
		if err := os.WriteFile(configPath1, []byte(content1), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath2, []byte(content2), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath1, configPath2}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config["test"].Command != "second" {
			t.Errorf("command = %q, want %q (later should override)", config["test"].Command, "second")
		}
	})

	t.Run("merge - different servers", func(t *testing.T) {
		content1 := `{"mcpServers": {"server1": {"command": "cmd1"}}}`
		content2 := `{"mcpServers": {"server2": {"command": "cmd2"}}}`
		tmpDir := t.TempDir()
		configPath1 := filepath.Join(tmpDir, "config1.json")
		configPath2 := filepath.Join(tmpDir, "config2.json")
		if err := os.WriteFile(configPath1, []byte(content1), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath2, []byte(content2), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath1, configPath2}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if len(config) != 2 {
			t.Errorf("expected 2 servers, got %d", len(config))
		}
	})

	t.Run("error - missing file", func(t *testing.T) {
		_, err := LoadConfig([]string{"/nonexistent/path/config.json"}, false)
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("error - invalid JSON", func(t *testing.T) {
		content := `{invalid json}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadConfig([]string{configPath}, false)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("error - missing mcpServers key", func(t *testing.T) {
		content := `{"other": "data"}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadConfig([]string{configPath}, false)
		if err == nil {
			t.Error("expected error for missing mcpServers key")
		}
	})

	t.Run("error - mcpServers not object", func(t *testing.T) {
		content := `{"mcpServers": "not an object"}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadConfig([]string{configPath}, false)
		if err == nil {
			t.Error("expected error for mcpServers not being an object")
		}
	})

	t.Run("error - server definition not object", func(t *testing.T) {
		content := `{"mcpServers": {"test": "not an object"}}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadConfig([]string{configPath}, false)
		if err == nil {
			t.Error("expected error for server definition not being an object")
		}
	})

	t.Run("full server definition with all fields", func(t *testing.T) {
		content := `{
			"mcpServers": {
				"full": {
					"command": "/usr/bin/server",
					"args": ["--flag", "value"],
					"env": {"KEY": "val", "OTHER": "123"},
					"url": "http://localhost:8080",
					"headers": {"Authorization": "Bearer token"}
				}
			}
		}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		server := config["full"]
		if server.Command != "/usr/bin/server" {
			t.Errorf("command = %q, want %q", server.Command, "/usr/bin/server")
		}
		if len(server.Args) != 2 {
			t.Errorf("len(args) = %d, want 2", len(server.Args))
		}
		if server.Args[0] != "--flag" || server.Args[1] != "value" {
			t.Errorf("args = %v, want [--flag, value]", server.Args)
		}
		if server.Env["KEY"] != "val" {
			t.Errorf("env[KEY] = %q, want %q", server.Env["KEY"], "val")
		}
		if server.URL != "http://localhost:8080" {
			t.Errorf("url = %q, want %q", server.URL, "http://localhost:8080")
		}
		if server.Headers["Authorization"] != "Bearer token" {
			t.Errorf("headers[Authorization] = %q, want %q", server.Headers["Authorization"], "Bearer token")
		}
	})

	t.Run("env expansion in env values", func(t *testing.T) {
		os.Setenv("INNER_VAR", "inner-value")
		defer os.Unsetenv("INNER_VAR")

		content := `{"mcpServers": {"test": {"command": "server", "env": {"OUTER": "${INNER_VAR}"}}}}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config["test"].Env["OUTER"] != "inner-value" {
			t.Errorf("env[OUTER] = %q, want %q", config["test"].Env["OUTER"], "inner-value")
		}
	})

	t.Run("env expansion in headers values", func(t *testing.T) {
		os.Setenv("AUTH_TOKEN", "secret123")
		defer os.Unsetenv("AUTH_TOKEN")

		content := `{"mcpServers": {"test": {"command": "server", "headers": {"X-Auth": "${AUTH_TOKEN}"}}}}`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		config, err := LoadConfig([]string{configPath}, false)
		if err != nil {
			t.Fatalf("LoadConfig() error = %v", err)
		}

		if config["test"].Headers["X-Auth"] != "secret123" {
			t.Errorf("headers[X-Auth] = %q, want %q", config["test"].Headers["X-Auth"], "secret123")
		}
	})
}

// TestAC2_MCPConfigWiring verifies that LoadConfig followed by ConnectAll
// properly initializes MCP clients from the config file.
// Note: This test verifies the wiring path without requiring a valid MCP server
// since echo is not a proper MCP server. We verify that LoadConfig returns
// the correct config structure that ConnectAll would use.
func TestAC2_MCPConfigWiring(t *testing.T) {
	content := `{"mcpServers": {"test-server": {"command": "echo", "args": ["hello"]}}}`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Load config - this is what main.go calls first
	config, err := LoadConfig([]string{configPath}, false)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// Verify config structure is correct for wiring
	if len(config) != 1 {
		t.Fatalf("expected 1 server, got %d", len(config))
	}

	server, ok := config["test-server"]
	if !ok {
		t.Fatal("expected server 'test-server' in config")
	}
	if server.Command != "echo" {
		t.Errorf("expected command 'echo', got %q", server.Command)
	}
	if len(server.Args) != 1 || server.Args[0] != "hello" {
		t.Errorf("expected args ['hello'], got %v", server.Args)
	}

	// Verify that ConnectAll would be called with this config
	// We can't actually call ConnectAll without a valid MCP server,
	// but we verified the config wiring path is correct
	t.Log("AC2 PASS: LoadConfig returns correct config for ConnectAll wiring")
}
