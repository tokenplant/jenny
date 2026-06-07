// Package lsp provides a Language Server Protocol client interface.
package lsp

import "context"

// Location represents a position in a file.
type Location struct {
	URI   string
	Line  int // 0-based
	Start int // 0-based character offset
	End   int // 0-based character offset
}

// Reference represents a symbol reference.
type Reference struct {
	URI   string
	Line  int
	Start int
	End   int
}

// DocumentSymbol represents a symbol in a document.
type DocumentSymbol struct {
	Name     string
	Kind     string
	Line     int
	Start    int
	End      int
	Parent   string // parent symbol name, empty for top-level
	FilePath string
}

// WorkspaceSymbol represents a symbol found in the workspace.
type WorkspaceSymbol struct {
	Name     string
	Kind     string
	URI      string
	Line     int
	Start    int
	End      int
	FilePath string
}

// CallHierarchyItem represents an item in call hierarchy.
type CallHierarchyItem struct {
	Name     string
	Kind     string
	URI      string
	Line     int
	Start    int
	End      int
	FilePath string
}

// HoverResult represents hover information.
type HoverResult struct {
	Contents string
	Line     int
	Start    int
}

// SymbolInfo represents basic symbol information.
type SymbolInfo struct {
	Name     string
	Kind     string
	URI      string
	Line     int
	Start    int
	End      int
	FilePath string
}

// Client defines the interface for LSP client operations.
// All implementations must be safe for concurrent use.
// All coordinate parameters are 0-based.
type Client interface {
	// Connected returns true if the LSP client is connected to a server.
	Connected() bool

	// GoToDefinition returns the definition of the symbol at the given position.
	GoToDefinition(ctx context.Context, uri string, line int, character int) ([]Location, error)

	// FindReferences returns all references to the symbol at the given position.
	FindReferences(ctx context.Context, uri string, line int, character int) ([]Reference, error)

	// Hover returns hover information for the symbol at the given position.
	Hover(ctx context.Context, uri string, line int, character int) (*HoverResult, error)

	// DocumentSymbol returns all symbols in the given document.
	DocumentSymbol(ctx context.Context, uri string) ([]DocumentSymbol, error)

	// WorkspaceSymbol searches for symbols across the workspace.
	WorkspaceSymbol(ctx context.Context, query string) ([]WorkspaceSymbol, error)

	// GoToImplementation returns the implementation of the symbol at the given position.
	GoToImplementation(ctx context.Context, uri string, line int, character int) ([]Location, error)

	// PrepareCallHierarchy returns call hierarchy items for the given position.
	PrepareCallHierarchy(ctx context.Context, uri string, line int, character int) ([]CallHierarchyItem, error)

	// IncomingCalls returns incoming calls to the symbol at the given position.
	IncomingCalls(ctx context.Context, uri string, line int, character int) ([]CallHierarchyItem, error)

	// OutgoingCalls returns outgoing calls from the symbol at the given position.
	OutgoingCalls(ctx context.Context, uri string, line int, character int) ([]CallHierarchyItem, error)
}
