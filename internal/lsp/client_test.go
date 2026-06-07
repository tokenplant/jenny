package lsp

import (
	"context"
	"sync"
)

// MockClient is a mock implementation of the LSP Client interface for testing.
type MockClient struct {
	mu              sync.RWMutex
	connected       bool
	calls           []string
	goToDefResult   []Location
	findRefsResult  []Reference
	hoverResult     *HoverResult
	docSymResults   []DocumentSymbol
	workspaceSymRes []WorkspaceSymbol
	goToImplResult  []Location
	prepareCHRes    []CallHierarchyItem
	incomingRes     []CallHierarchyItem
	outgoingRes     []CallHierarchyItem
}

// NewMockClient creates a new mock LSP client.
func NewMockClient() *MockClient {
	return &MockClient{connected: true}
}

// SetConnected sets the connected state.
func (m *MockClient) SetConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected = connected
}

// SetGoToDefResult sets the result for goToDefinition.
func (m *MockClient) SetGoToDefResult(locations []Location) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goToDefResult = locations
}

// SetFindRefsResult sets the result for findReferences.
func (m *MockClient) SetFindRefsResult(refs []Reference) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findRefsResult = refs
}

// SetHoverResult sets the result for hover.
func (m *MockClient) SetHoverResult(result *HoverResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hoverResult = result
}

// SetDocumentSymbolResult sets the result for documentSymbol.
func (m *MockClient) SetDocumentSymbolResult(symbols []DocumentSymbol) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docSymResults = symbols
}

// SetWorkspaceSymbolResult sets the result for workspaceSymbol.
func (m *MockClient) SetWorkspaceSymbolResult(symbols []WorkspaceSymbol) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaceSymRes = symbols
}

// SetGoToImplResult sets the result for goToImplementation.
func (m *MockClient) SetGoToImplResult(locations []Location) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.goToImplResult = locations
}

// SetPrepareCallHierarchyResult sets the result for prepareCallHierarchy.
func (m *MockClient) SetPrepareCallHierarchyResult(items []CallHierarchyItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepareCHRes = items
}

// SetIncomingCallsResult sets the result for incomingCalls.
func (m *MockClient) SetIncomingCallsResult(items []CallHierarchyItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.incomingRes = items
}

// SetOutgoingCallsResult sets the result for outgoingCalls.
func (m *MockClient) SetOutgoingCallsResult(items []CallHierarchyItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outgoingRes = items
}

// GetCalls returns all the calls that were made (for verification).
func (m *MockClient) GetCalls() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]string, len(m.calls))
	copy(calls, m.calls)
	return calls
}

// Connected implements Client.Connected.
func (m *MockClient) Connected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

func (m *MockClient) recordCall(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, name)
}

// GoToDefinition implements Client.GoToDefinition.
func (m *MockClient) GoToDefinition(_ context.Context, _ string, _ int, _ int) ([]Location, error) {
	m.recordCall("GoToDefinition")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.goToDefResult, nil
}

// FindReferences implements Client.FindReferences.
func (m *MockClient) FindReferences(_ context.Context, _ string, _ int, _ int) ([]Reference, error) {
	m.recordCall("FindReferences")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.findRefsResult, nil
}

// Hover implements Client.Hover.
func (m *MockClient) Hover(_ context.Context, _ string, _ int, _ int) (*HoverResult, error) {
	m.recordCall("Hover")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hoverResult, nil
}

// DocumentSymbol implements Client.DocumentSymbol.
func (m *MockClient) DocumentSymbol(_ context.Context, _ string) ([]DocumentSymbol, error) {
	m.recordCall("DocumentSymbol")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.docSymResults, nil
}

// WorkspaceSymbol implements Client.WorkspaceSymbol.
func (m *MockClient) WorkspaceSymbol(_ context.Context, _ string) ([]WorkspaceSymbol, error) {
	m.recordCall("WorkspaceSymbol")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workspaceSymRes, nil
}

// GoToImplementation implements Client.GoToImplementation.
func (m *MockClient) GoToImplementation(_ context.Context, _ string, _ int, _ int) ([]Location, error) {
	m.recordCall("GoToImplementation")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.goToImplResult, nil
}

// PrepareCallHierarchy implements Client.PrepareCallHierarchy.
func (m *MockClient) PrepareCallHierarchy(_ context.Context, _ string, _ int, _ int) ([]CallHierarchyItem, error) {
	m.recordCall("PrepareCallHierarchy")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prepareCHRes, nil
}

// IncomingCalls implements Client.IncomingCalls.
func (m *MockClient) IncomingCalls(_ context.Context, _ string, _ int, _ int) ([]CallHierarchyItem, error) {
	m.recordCall("IncomingCalls")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.incomingRes, nil
}

// OutgoingCalls implements Client.OutgoingCalls.
func (m *MockClient) OutgoingCalls(_ context.Context, _ string, _ int, _ int) ([]CallHierarchyItem, error) {
	m.recordCall("OutgoingCalls")
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outgoingRes, nil
}
