// Package toolresult provides the ToolResult type used by tool implementations.
package toolresult

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	// Content is the text content of the tool result.
	Content string `json:"content"`
	// IsError indicates whether the tool execution resulted in an error.
	IsError bool `json:"is_error,omitempty"`
	// Truncated indicates the result was truncated due to size limits.
	Truncated bool `json:"truncated,omitempty"`
	// OutputFile is the path to a file containing the output (for large results).
	OutputFile string `json:"output_file,omitempty"`
	// CacheHit indicates the result was served from cache (file unchanged since last read).
	CacheHit bool `json:"cache_hit,omitempty"`
}
