// Package agent provides the core agent loop and execution engine.
package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/ipy/jenny/internal/tool"
)

// defaultMaxConcurrency is the default maximum parallel tool execution count.
const defaultMaxConcurrency = 10

// toolGroup represents a batch of tools to execute together.
type toolGroup struct {
	// tools is the list of tool use blocks in this group.
	tools []toolUseWithIndex
	// serial indicates whether this group must run serially.
	serial bool
}

// toolUseWithIndex holds a tool use block with its original position.
type toolUseWithIndex struct {
	block  toolUseBlock
	index  int
	tool   tool.Tool
	cancel context.CancelFunc
}

// toolUseBlock is a local copy of api.ToolUseBlock with the tool instance.
type toolUseBlock struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolExecutor manages parallel tool execution with serialized mutation.
type ToolExecutor struct {
	tools          []tool.Tool
	cwd            string
	maxConcurrency int
}

// NewToolExecutor creates a new ToolExecutor.
func NewToolExecutor(tools []tool.Tool, cwd string) *ToolExecutor {
	return &ToolExecutor{
		tools:          tools,
		cwd:            cwd,
		maxConcurrency: defaultMaxConcurrency,
	}
}

// Execute runs all tool use blocks according to concurrency rules.
// It partitions tools into parallel batches (for concurrency-safe tools) and
// serial execution (for Write/Edit/Bash), collects results in request order.
func (e *ToolExecutor) Execute(toolUseBlocks []toolUseBlock) ([]toolResult, error) {
	// Partition into groups
	groups := e.partitionGroups(toolUseBlocks)

	// Allocate results slice with same length as total tools
	totalTools := len(toolUseBlocks)
	results := make([]toolResult, totalTools)

	// Execute each group
	for _, group := range groups {
		if group.serial {
			e.executeSerial(group.tools, results)
		} else {
			e.executeParallel(group.tools, results)
		}
	}

	return results, nil
}

// partitionGroups partitions tool use blocks into parallel and serial groups.
// Consecutive concurrency-safe tools (read, glob, grep) go into parallel batches.
// Bash tools run in parallel within a batch (with sibling abort on failure).
// Write/Edit tools are serialized individually.
func (e *ToolExecutor) partitionGroups(toolUseBlocks []toolUseBlock) []toolGroup {
	var groups []toolGroup
	var currentBatch []toolUseWithIndex
	var currentBatchType string // "bash", "readonly", or "" for empty

	flushBatch := func() {
		if len(currentBatch) > 0 {
			groups = append(groups, toolGroup{
				tools:  currentBatch,
				serial: false,
			})
		}
		currentBatch = nil
		currentBatchType = ""
	}

	for i, block := range toolUseBlocks {
		t := tool.FindTool(e.tools, block.Name)

		if t == nil {
			// Unknown tool - serial error
			flushBatch()
			groups = append(groups, toolGroup{
				tools: []toolUseWithIndex{{
					block: block,
					index: i,
					tool:  nil,
				}},
				serial: true,
			})
		} else if isSerialTool(block.Name) {
			// Write/Edit - serial execution, flushed before and after
			flushBatch()
			groups = append(groups, toolGroup{
				tools: []toolUseWithIndex{{
					block: block,
					index: i,
					tool:  t,
				}},
				serial: true,
			})
		} else if isBashTool(block.Name) {
			// Bash - batch together for sibling abort (AC3), but use semaphore=1 for serial-like behavior (AC2)
			// This allows bash sibling abort while ensuring only one bash runs at a time
			flushBatch()
			currentBatchType = "bash"
			currentBatch = append(currentBatch, toolUseWithIndex{
				block: block,
				index: i,
				tool:  t,
			})
		} else if isReadOnlyTool(block.Name) {
			// Read/Glob/Grep - flush if previous was bash, then add to batch
			if currentBatchType == "bash" {
				flushBatch()
			}
			currentBatchType = "readonly"
			currentBatch = append(currentBatch, toolUseWithIndex{
				block: block,
				index: i,
				tool:  t,
			})
		} else {
			// Unknown non-serial tool - treat as serial
			flushBatch()
			groups = append(groups, toolGroup{
				tools: []toolUseWithIndex{{
					block: block,
					index: i,
					tool:  t,
				}},
				serial: true,
			})
		}
	}

	// Flush remaining batch
	flushBatch()

	return groups
}

// executeParallel runs a batch of concurrency-safe tools in parallel.
func (e *ToolExecutor) executeParallel(batch []toolUseWithIndex, results []toolResult) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxConcurrency)

	// Track bash failure for sibling abort
	var bashFailed bool
	var bashMu sync.Mutex

	for _, tw := range batch {
		wg.Add(1)
		go func(tw toolUseWithIndex) {
			defer wg.Done()

			// Acquire semaphore slot
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check if already cancelled before starting
			if ctx.Err() == context.Canceled {
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   "Tool execution aborted due to sibling failure",
					IsError:   true,
				}
				return
			}

			// Execute the tool with context for cancellation support
			execResult, err := tw.tool.ExecuteWithContext(ctx, tw.block.Input, e.cwd)

			// Check if cancelled (sibling abort) - check BEFORE storing result
			if ctx.Err() == context.Canceled {
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   "Tool execution aborted due to sibling failure",
					IsError:   true,
				}
				return
			}

			// Check for bash failure - abort siblings
			if isBashTool(tw.block.Name) && err != nil {
				bashMu.Lock()
				if !bashFailed {
					bashFailed = true
					cancel() // Cancel all siblings via context
				}
				bashMu.Unlock()
			}

			// Store result
			if err != nil {
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   fmt.Sprintf("Error executing tool: %v", err),
					IsError:   true,
				}
			} else {
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   execResult.Content,
					IsError:   execResult.IsError,
				}
			}
		}(tw)
	}

	wg.Wait()
}

// executeSerial runs tools one at a time in request order.
func (e *ToolExecutor) executeSerial(batch []toolUseWithIndex, results []toolResult) {
	for _, tw := range batch {
		if tw.tool == nil {
			// Unknown tool - immediate error
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   fmt.Sprintf("Error: No such tool available: %s", tw.block.Name),
				IsError:   true,
			}
			continue
		}

		// Execute the tool with context (background context for serial, no cancellation)
		execResult, err := tw.tool.ExecuteWithContext(context.Background(), tw.block.Input, e.cwd)

		if err != nil {
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   fmt.Sprintf("Error executing tool: %v", err),
				IsError:   true,
			}
		} else {
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   execResult.Content,
				IsError:   execResult.IsError,
			}
		}
	}
}

// isReadOnlyTool returns true if the tool is read-only (read, glob, grep).
func isReadOnlyTool(toolName string) bool {
	switch toolName {
	case "read", "Read", "Glob", "grep", "Grep":
		return true
	default:
		return false
	}
}

// isSerialTool returns true if the tool must run serially (write, edit).
func isSerialTool(toolName string) bool {
	switch toolName {
	case "write", "edit", "Write", "Edit":
		return true
	default:
		return false
	}
}

// isBashTool returns true if the tool is a bash tool.
func isBashTool(toolName string) bool {
	return toolName == "bash" || toolName == "Bash"
}

// toolResult represents a tool execution result.
type toolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}
