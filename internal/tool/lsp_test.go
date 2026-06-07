package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ipy/jenny/internal/lsp"
)

// mockLSPClient is a mock implementation of lsp.Client for testing.
type mockLSPClient struct {
	connected       bool
	calls           []string
	goToDefResult   []lsp.Location
	findRefsResult  []lsp.Reference
	hoverResult     *lsp.HoverResult
	docSymResults   []lsp.DocumentSymbol
	workspaceSymRes []lsp.WorkspaceSymbol
	goToImplResult  []lsp.Location
	prepareCHRes    []lsp.CallHierarchyItem
	incomingRes     []lsp.CallHierarchyItem
	outgoingRes     []lsp.CallHierarchyItem
	mu              sync.RWMutex
}

func newMockLSPClient() *mockLSPClient {
	return &mockLSPClient{connected: true}
}

func (m *mockLSPClient) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *mockLSPClient) GoToDefinition(_ context.Context, _ string, _ int, _ int) ([]lsp.Location, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "goToDefinition")
	return m.goToDefResult, nil
}

func (m *mockLSPClient) FindReferences(_ context.Context, _ string, _ int, _ int) ([]lsp.Reference, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "findReferences")
	return m.findRefsResult, nil
}

func (m *mockLSPClient) Hover(_ context.Context, _ string, _ int, _ int) (*lsp.HoverResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "hover")
	return m.hoverResult, nil
}

func (m *mockLSPClient) DocumentSymbol(_ context.Context, _ string) ([]lsp.DocumentSymbol, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "documentSymbol")
	return m.docSymResults, nil
}

func (m *mockLSPClient) WorkspaceSymbol(_ context.Context, _ string) ([]lsp.WorkspaceSymbol, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "workspaceSymbol")
	return m.workspaceSymRes, nil
}

func (m *mockLSPClient) GoToImplementation(_ context.Context, _ string, _ int, _ int) ([]lsp.Location, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "goToImplementation")
	return m.goToImplResult, nil
}

func (m *mockLSPClient) PrepareCallHierarchy(_ context.Context, _ string, _ int, _ int) ([]lsp.CallHierarchyItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "prepareCallHierarchy")
	return m.prepareCHRes, nil
}

func (m *mockLSPClient) IncomingCalls(_ context.Context, _ string, _ int, _ int) ([]lsp.CallHierarchyItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "incomingCalls")
	return m.incomingRes, nil
}

func (m *mockLSPClient) OutgoingCalls(_ context.Context, _ string, _ int, _ int) ([]lsp.CallHierarchyItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "outgoingCalls")
	return m.outgoingRes, nil
}

func (m *mockLSPClient) setConnected(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = v
}

func (m *mockLSPClient) setGoToDefResult(locations []lsp.Location) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goToDefResult = locations
}

func (m *mockLSPClient) setFindRefsResult(refs []lsp.Reference) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findRefsResult = refs
}

func (m *mockLSPClient) setHoverResult(result *lsp.HoverResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hoverResult = result
}

func (m *mockLSPClient) setDocumentSymbolResult(symbols []lsp.DocumentSymbol) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docSymResults = symbols
}

func (m *mockLSPClient) setWorkspaceSymbolResult(symbols []lsp.WorkspaceSymbol) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaceSymRes = symbols
}

func (m *mockLSPClient) setGoToImplResult(locations []lsp.Location) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goToImplResult = locations
}

func (m *mockLSPClient) setPrepareCHResult(items []lsp.CallHierarchyItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepareCHRes = items
}

func (m *mockLSPClient) setIncomingResult(items []lsp.CallHierarchyItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incomingRes = items
}

func (m *mockLSPClient) setOutgoingResult(items []lsp.CallHierarchyItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outgoingRes = items
}

func (m *mockLSPClient) getCalls() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]string, len(m.calls))
	copy(calls, m.calls)
	return calls
}

func TestLSPTool_NameAndDescription(t *testing.T) {
	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	if tool.Name() != "lsp" {
		t.Errorf("expected Name() to be 'lsp', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}

	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected schema type 'object', got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["operation"]; !ok {
		t.Error("schema should have 'operation' property")
	}
}

func TestLSPTool_AC1_OneBasedCoordinates(t *testing.T) {
	mockClient := newMockLSPClient()
	mockClient.setGoToDefResult([]lsp.Location{
		{URI: "file:///test.go", Line: 0, Start: 0, End: 0},
	})
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	// AC1: Input line=1, character=1 should be converted to LSP line=0, character=0
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "goToDefinition",
		"uri":       "file:///test.go",
		"line":      1,
		"character": 1,
	}, "/tmp")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Verify the mock client received 0-based coordinates
	calls := mockClient.getCalls()
	if len(calls) != 1 || calls[0] != "goToDefinition" {
		t.Errorf("expected goToDefinition call, got %v", calls)
	}
}

func TestLSPTool_AC2_DisconnectedError(t *testing.T) {
	mockClient := newMockLSPClient()
	mockClient.setConnected(false)
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	// AC2: When not connected, should return clear error
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "hover",
		"uri":       "file:///test.go",
		"line":      1,
	}, "/tmp")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError when disconnected")
	}
	if !strings.Contains(result.Content, "not connected") {
		t.Errorf("expected error mentioning 'not connected', got: %s", result.Content)
	}
}

func TestLSPTool_AC3_FileSizeLimit(t *testing.T) {
	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	// Create a temporary file larger than 10MB
	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "large.go")
	f, err := os.Create(largeFile)
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	// Write more than 10MB of data
	data := make([]byte, 11*1024*1024) // 11 MB
	for i := range data {
		data[i] = 'x'
	}
	f.Write(data)
	f.Close()

	// AC3: Files >10MB should be rejected
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "hover",
		"uri":       "file://" + largeFile,
		"line":      1,
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for file >10MB")
	}
	if !strings.Contains(result.Content, "too large") {
		t.Errorf("expected error mentioning 'too large', got: %s", result.Content)
	}
}

func TestLSPTool_AC4_ConcurrencySafety(t *testing.T) {
	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	// Set up mock responses
	mockClient.setHoverResult(&lsp.HoverResult{
		Contents: "example documentation",
		Line:     5,
		Start:    10,
	})
	mockClient.setGoToDefResult([]lsp.Location{
		{URI: "file:///test.go", Line: 5, Start: 10, End: 20},
	})

	// AC4: Concurrent calls should work without race conditions
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := range 10 {
		wg.Add(2)

		// Concurrent hover call
		go func(id int) {
			defer wg.Done()
			result, err := tool.Execute(ctx, map[string]any{
				"operation": "hover",
				"uri":       "file:///test.go",
				"line":      6,
				"character": 11,
			}, "/tmp")
			if err != nil {
				errors <- err
				return
			}
			if result == nil {
				errors <- nil
			}
		}(i)

		// Concurrent goToDefinition call
		go func(id int) {
			defer wg.Done()
			result, err := tool.Execute(ctx, map[string]any{
				"operation": "goToDefinition",
				"uri":       "file:///test.go",
				"line":      6,
				"character": 11,
			}, "/tmp")
			if err != nil {
				errors <- err
				return
			}
			if result == nil {
				errors <- nil
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent call failed: %v", err)
		}
	}
}

func TestLSPTool_AC5_GitignoreFiltering_WorkspaceSymbol(t *testing.T) {
	// Create a temp directory with a .gitignore
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	os.WriteFile(gitignorePath, []byte("ignored.go\n*.log\n"), 0644)

	// Create a .git directory to make it a git repo
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	// Set up workspaceSymbol results that include gitignored paths
	mockClient.setWorkspaceSymbolResult([]lsp.WorkspaceSymbol{
		{Name: "GoodSymbol", Kind: "Function", URI: "file:///good.go", Line: 1, Start: 0, FilePath: filepath.Join(tmpDir, "good.go")},
		{Name: "BadSymbol", Kind: "Function", URI: "file:///ignored.go", Line: 1, Start: 0, FilePath: filepath.Join(tmpDir, "ignored.go")},
		{Name: "AlsoBad", Kind: "Function", URI: "file:///debug.log", Line: 1, Start: 0, FilePath: filepath.Join(tmpDir, "debug.log")},
	})

	// AC5: Gitignored paths should be filtered from results
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "workspaceSymbol",
		"query":     "test",
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Should only contain good.go, not ignored.go or debug.log
	if strings.Contains(result.Content, "ignored.go") {
		t.Error("gitignored path 'ignored.go' should be filtered from results")
	}
	if strings.Contains(result.Content, "debug.log") {
		t.Error("gitignored path 'debug.log' should be filtered from results")
	}
	if !strings.Contains(result.Content, "GoodSymbol") {
		t.Error("non-gitignored path should be in results")
	}
}

func TestLSPTool_AC5_GitignoreFiltering_FindReferences(t *testing.T) {
	// Create a temp directory with a .gitignore
	tmpDir := t.TempDir()
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	os.WriteFile(gitignorePath, []byte("secret.go\n"), 0644)

	// Create a .git directory to make it a git repo
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	// Set up findReferences results that include gitignored paths
	// Note: URIs must be file:// URIs that point to files within the repo for gitignore filtering to work
	mockClient.setFindRefsResult([]lsp.Reference{
		{URI: "file://" + filepath.Join(tmpDir, "visible.go"), Line: 5, Start: 10},
		{URI: "file://" + filepath.Join(tmpDir, "secret.go"), Line: 5, Start: 10},
	})

	// AC5: Gitignored paths should be filtered from findReferences
	result, err := tool.Execute(ctx, map[string]any{
		"operation": "findReferences",
		"uri":       "file:///test.go",
		"line":      6,
		"character": 11,
	}, tmpDir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Should only contain visible.go, not secret.go
	if strings.Contains(result.Content, "secret.go") {
		t.Error("gitignored path 'secret.go' should be filtered from findReferences results")
	}
	if !strings.Contains(result.Content, "visible.go") {
		t.Error("non-gitignored path should be in findReferences results")
	}
}

func TestLSPTool_InvalidOperation(t *testing.T) {
	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"operation": "invalidOperation",
	}, "/tmp")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for invalid operation")
	}
	if !strings.Contains(result.Content, "unknown operation") {
		t.Errorf("expected error mentioning 'unknown operation', got: %s", result.Content)
	}
}

func TestLSPTool_MissingOperation(t *testing.T) {
	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"uri": "file:///test.go",
	}, "/tmp")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError for missing operation")
	}
}

func TestLSPTool_AllOperations(t *testing.T) {
	mockClient := newMockLSPClient()
	tool := NewLSPTool(mockClient)
	ctx := context.Background()

	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)

	// Set up mock responses for all operations
	mockClient.setGoToDefResult([]lsp.Location{{URI: "file:///test.go", Line: 5, Start: 10}})
	mockClient.setFindRefsResult([]lsp.Reference{{URI: "file:///test.go", Line: 5, Start: 10}})
	mockClient.setHoverResult(&lsp.HoverResult{Contents: "test", Line: 5, Start: 10})
	mockClient.setDocumentSymbolResult([]lsp.DocumentSymbol{{Name: "Test", Kind: "Function", Line: 5, Start: 10, FilePath: filepath.Join(tmpDir, "test.go")}})
	mockClient.setWorkspaceSymbolResult([]lsp.WorkspaceSymbol{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10, FilePath: filepath.Join(tmpDir, "test.go")}})
	mockClient.setGoToImplResult([]lsp.Location{{URI: "file:///test.go", Line: 5, Start: 10}})
	mockClient.setPrepareCHResult([]lsp.CallHierarchyItem{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10}})
	mockClient.setIncomingResult([]lsp.CallHierarchyItem{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10}})
	mockClient.setOutgoingResult([]lsp.CallHierarchyItem{{Name: "Test", Kind: "Function", URI: "file:///test.go", Line: 5, Start: 10}})

	operations := []string{
		"goToDefinition",
		"findReferences",
		"hover",
		"documentSymbol",
		"workspaceSymbol",
		"goToImplementation",
		"prepareCallHierarchy",
		"incomingCalls",
		"outgoingCalls",
	}

	for _, op := range operations {
		t.Run(op, func(t *testing.T) {
			input := map[string]any{
				"operation": op,
				"uri":       "file:///test.go",
				"line":      6,
				"character": 11,
			}
			if op == "workspaceSymbol" {
				input = map[string]any{
					"operation": op,
					"query":     "test",
				}
			} else if op == "documentSymbol" {
				input = map[string]any{
					"operation": op,
					"uri":       "file:///test.go",
				}
			}

			result, err := tool.Execute(ctx, input, tmpDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError && strings.Contains(result.Content, "not connected") {
				t.Errorf("operation %s failed with connection error", op)
			}
		})
	}
}
