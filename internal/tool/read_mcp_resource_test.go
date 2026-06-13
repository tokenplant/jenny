package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/mcp"
)

// fakeServer holds a running fake MCP server process.
type fakeServer struct {
	cmd    *exec.Cmd
	client *mcp.Client
}

func (s *fakeServer) close() {
	if s.client != nil {
		s.client.Disconnect()
	}
	if s.cmd != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
}

// startFakeServer starts a fake MCP server and creates a connected client.
func startFakeServer(t *testing.T, name string) *fakeServer {
	// Determine repo root by finding go.mod
	repoRoot := "."
	for {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		repoRoot = filepath.Join(repoRoot, "..")
		if len(repoRoot) > 100 {
			t.Skip("cannot find repo root (go.mod)")
			return nil
		}
	}

	// Build the fake server first
	fakeServerBinary := filepath.Join(repoRoot, "internal", "mcp", "testdata", "fake-mcp-server", "fake-mcp-server")
	if _, err := os.Stat(fakeServerBinary); os.IsNotExist(err) {
		// Try to build it
		buildCmd := exec.Command("go", "build", "-o", fakeServerBinary, "./internal/mcp/testdata/fake-mcp-server")
		buildCmd.Dir = repoRoot
		if err := buildCmd.Run(); err != nil {
			t.Skipf("fake MCP server not built: %v", err)
		}
	}

	fakeServerPath := fakeServerBinary

	cmd := exec.Command(fakeServerPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Skipf("failed to start fake MCP server: %v", err)
	}

	client := mcp.NewClient(name, fakeServerPath, nil, nil)
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Skipf("failed to connect to fake MCP server: %v", err)
	}

	// Register the client so GetClient can find it
	mcp.SetTestClient(name, client)

	return &fakeServer{cmd: cmd, client: client}
}

// TestReadMcpResourceTool_NameAndDescription tests basic tool metadata.
func TestReadMcpResourceTool_NameAndDescription(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	if readTool.Name() != "read_mcp_resource" {
		t.Errorf("expected name 'read_mcp_resource', got %q", readTool.Name())
	}

	desc := readTool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	schema := readTool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

// TestReadMcpResourceTool_AC1_UnknownServer tests AC1: unknown server returns error with available names.
func TestReadMcpResourceTool_AC1_UnknownServer(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	result, err := readTool.Execute(context.Background(), map[string]any{
		"server": "definitely-nonexistent-server-12345",
		"uri":    "file:///test.txt",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for invalid server")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content")
	}
	// Should mention the invalid server name
	if !strings.Contains(result.Content, "definitely-nonexistent-server-12345") {
		t.Errorf("error should mention server name, got: %s", result.Content)
	}
	// Should list available servers
	if !strings.Contains(result.Content, "Available servers") {
		t.Errorf("error should list available servers, got: %s", result.Content)
	}
}

// TestReadMcpResourceTool_MissingServer tests error when server parameter is missing.
func TestReadMcpResourceTool_MissingServer(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	result, err := readTool.Execute(context.Background(), map[string]any{
		"uri": "file:///test.txt",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing server")
	}
	if !strings.Contains(result.Content, "server parameter is required") {
		t.Errorf("expected 'server parameter is required' error, got: %s", result.Content)
	}
}

// TestReadMcpResourceTool_MissingURI tests error when uri parameter is missing.
func TestReadMcpResourceTool_MissingURI(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	result, err := readTool.Execute(context.Background(), map[string]any{
		"server": "test-server",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for missing uri")
	}
	if !strings.Contains(result.Content, "uri parameter is required") {
		t.Errorf("expected 'uri parameter is required' error, got: %s", result.Content)
	}
}

// TestReadMcpResourceTool_ExecuteInterface tests that the tool implements the tool.Tool interface.
func TestReadMcpResourceTool_ExecuteInterface(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	// Verify it can be called with the correct signature
	result, err := readTool.Execute(context.Background(), map[string]any{
		"server": "any-server",
		"uri":    "file:///test.txt",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
	// Result should be an error since server doesn't exist
	if !result.IsError {
		t.Error("expected error for non-existent server")
	}
}

// TestReadMcpResourceTool_ImplementsToolInterface verifies ReadMcpResourceTool implements Tool interface.
func TestReadMcpResourceTool_ImplementsToolInterface(t *testing.T) {
	readTool := NewReadMcpResourceTool()

	// Compile-time check: verify ReadMcpResourceTool implements Tool
	var _ Tool = readTool
}

// TestReadMcpResourceTool_InputSchemaRequiredFields verifies required fields in schema.
func TestReadMcpResourceTool_InputSchemaRequiredFields(t *testing.T) {
	readTool := NewReadMcpResourceTool()
	schema := readTool.InputSchema()

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	serverProp, ok := props["server"]
	if !ok {
		t.Fatal("expected server property")
	}
	serverSchema := serverProp.(map[string]any)
	if serverSchema["type"] != "string" {
		t.Errorf("expected server type string, got %v", serverSchema["type"])
	}

	uriProp, ok := props["uri"]
	if !ok {
		t.Fatal("expected uri property")
	}
	uriSchema := uriProp.(map[string]any)
	if uriSchema["type"] != "string" {
		t.Errorf("expected uri type string, got %v", uriSchema["type"])
	}

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required field")
	}
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got %d", len(required))
	}
}

// TestReadMcpResourceTool_AC2_TextInline tests AC2: text content is returned inline via fake MCP server.
func TestReadMcpResourceTool_AC2_TextInline(t *testing.T) {
	server := startFakeServer(t, "test-server")
	defer server.close()

	cwd := t.TempDir()
	tool := NewReadMcpResourceTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "test-server",
		"uri":    "file:///text.txt",
	}, cwd)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Parse output JSON
	var output struct {
		Contents []struct {
			URI         string `json:"uri"`
			MimeType    string `json:"mimeType,omitempty"`
			Text        string `json:"text,omitempty"`
			BlobSavedTo string `json:"blobSavedTo,omitempty"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	if len(output.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(output.Contents))
	}
	if output.Contents[0].Text != "Hello, World!" {
		t.Errorf("expected text 'Hello, World!', got %q", output.Contents[0].Text)
	}
	if output.Contents[0].BlobSavedTo != "" {
		t.Errorf("expected no blobSavedTo for text content, got %s", output.Contents[0].BlobSavedTo)
	}
	if output.Contents[0].MimeType != "text/plain" {
		t.Errorf("expected mimeType 'text/plain', got %q", output.Contents[0].MimeType)
	}
}

// TestReadMcpResourceTool_AC3_BlobPersist tests AC3: binary content is decoded and saved to disk via fake MCP server.
func TestReadMcpResourceTool_AC3_BlobPersist(t *testing.T) {
	server := startFakeServer(t, "blob-server")
	defer server.close()

	cwd := t.TempDir()

	// Override JennyHomeDir to use temp dir/.jenny for testing
	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string {
		return filepath.Join(cwd, constants.ProjectDirName)
	}
	defer func() {
		constants.JennyHomeDirFunc = originalFunc
	}()

	tool := NewReadMcpResourceTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "blob-server",
		"uri":    "file:///image.png",
	}, cwd)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Parse output JSON
	var output struct {
		Contents []struct {
			URI         string `json:"uri"`
			MimeType    string `json:"mimeType,omitempty"`
			BlobSavedTo string `json:"blobSavedTo,omitempty"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	if len(output.Contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(output.Contents))
	}
	if output.Contents[0].BlobSavedTo == "" {
		t.Fatal("expected blobSavedTo to be set for blob content")
	}

	// Verify file exists and contains decoded data
	data, err := os.ReadFile(output.Contents[0].BlobSavedTo)
	if err != nil {
		t.Fatalf("failed to read saved blob file: %v", err)
	}
	if string(data) != "Hello" {
		t.Errorf("expected file content 'Hello', got %q", string(data))
	}

	// Verify file is in the correct directory (under JennyHomeDir/mcp-resources)
	expectedDir := filepath.Join(cwd, constants.ProjectDirName, "mcp-resources")
	if filepath.Dir(output.Contents[0].BlobSavedTo) != expectedDir {
		t.Errorf("expected file in %s, got %s", expectedDir, filepath.Dir(output.Contents[0].BlobSavedTo))
	}
}

// TestReadMcpResourceTool_AC4_PersistFailure tests AC4: disk write failure returns error, not base64.
func TestReadMcpResourceTool_AC4_PersistFailure(t *testing.T) {
	server := startFakeServer(t, "fail-server")
	defer server.close()

	// Override JennyHomeDir to return a path that cannot be created
	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string {
		return "/nonexistent/path/that/cannot/be/created"
	}
	defer func() {
		constants.JennyHomeDirFunc = originalFunc
	}()

	tool := NewReadMcpResourceTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "fail-server",
		"uri":    "file:///image.png",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for persist failure")
	}
	if !strings.Contains(result.Content, "Error saving binary content to disk") {
		t.Errorf("expected persist error message, got: %s", result.Content)
	}
	// AC4: The result must NOT contain the raw base64 data
	if strings.Contains(result.Content, "SGVsbG8=") {
		t.Error("result must NOT contain raw base64 data when persist fails")
	}
}

// TestReadMcpResourceTool_AC5_ConcurrentCalls tests AC5: concurrent calls are safe via race detector.
func TestReadMcpResourceTool_AC5_ConcurrentCalls(t *testing.T) {
	// Start multiple fake servers, one per client to avoid stdin/stdout conflicts
	const numServers = 5
	var servers []*fakeServer
	for i := range numServers {
		server := startFakeServer(t, fmt.Sprintf("concurrent-server-%d", i))
		servers = append(servers, server)
	}
	defer func() {
		for _, s := range servers {
			s.close()
		}
	}()

	cwd := t.TempDir()

	var wg sync.WaitGroup
	const numGoroutines = 10
	for i := range numGoroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tool := NewReadMcpResourceTool()
			// Round-robin across servers to distribute load
			server := servers[idx%numServers]
			result, err := tool.Execute(context.Background(), map[string]any{
				"server": server.client.Name,
				"uri":    "file:///text.txt",
			}, cwd)
			if err != nil {
				t.Errorf("Execute returned error: %v", err)
				return
			}
			if result.IsError {
				t.Errorf("unexpected error: %s", result.Content)
			}
		}(i)
	}
	wg.Wait()
}

// TestReadMcpResourceTool_AC5_ConcurrentCalls_SingleClient tests AC5 with a single client
// to verify the mutex actually protects concurrent access to the transport.
func TestReadMcpResourceTool_AC5_ConcurrentCalls_SingleClient(t *testing.T) {
	server := startFakeServer(t, "single-server")
	defer server.close()

	cwd := t.TempDir()
	tool := NewReadMcpResourceTool()

	var wg sync.WaitGroup
	const numGoroutines = 10
	for range numGoroutines {
		wg.Go(func() {
			result, err := tool.Execute(context.Background(), map[string]any{
				"server": "single-server",
				"uri":    "file:///text.txt",
			}, cwd)
			if err != nil {
				t.Errorf("Execute returned error: %v", err)
				return
			}
			if result.IsError {
				t.Errorf("unexpected error: %s", result.Content)
			}
		})
	}
	wg.Wait()
}

var _ = mcp.GetClient // Reference mcp package to ensure it compiles
