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
func (e *ToolExecutor) Execute(ctx context.Context, toolUseBlocks []toolUseBlock) ([]toolResult, error) {
	// Partition into groups
	groups := e.partitionGroups(toolUseBlocks)

	// Allocate results slice with same length as total tools
	totalTools := len(toolUseBlocks)
	results := make([]toolResult, totalTools)

	// Execute each group
	for _, group := range groups {
		if group.serial {
			e.executeSerial(ctx, group.tools, results)
		} else {
			e.executeParallel(ctx, group.tools, results)
		}
	}

	return results, nil
}

// partitionGroups partitions tool use blocks into parallel and serial groups.
// Consecutive concurrency-safe tools (read, glob, grep) go into parallel batches.
// Bash tools are accumulated into serial batches for sibling abort (AC3).
// Write/Edit tools are serialized individually.
func (e *ToolExecutor) partitionGroups(toolUseBlocks []toolUseBlock) []toolGroup {
	var groups []toolGroup
	var currentBatch []toolUseWithIndex
	var currentBatchType string // "bash", "readonly", or "" for empty

	flushBatch := func() {
		if len(currentBatch) > 0 {
			groups = append(groups, toolGroup{
				tools:  currentBatch,
				serial: currentBatchType == "bash",
			})
		}
		currentBatch = nil
		currentBatchType = ""
	}

	for i, block := range toolUseBlocks {
		var t tool.Tool
		if i < len(e.tools) && e.tools[i].Name() == block.Name {
			t = e.tools[i]
		} else {
			t = tool.FindTool(e.tools, block.Name)
		}
		// Fallback: "task" is an alias for "agent"
		if t == nil && block.Name == "task" {
			t = tool.FindTool(e.tools, "agent")
		}

		if t == nil {
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
			if currentBatchType != "" && currentBatchType != "bash" {
				flushBatch()
			}
			currentBatchType = "bash"
			currentBatch = append(currentBatch, toolUseWithIndex{
				block: block,
				index: i,
				tool:  t,
			})
		} else if isReadOnlyTool(block.Name) {
			if currentBatchType != "" && currentBatchType != "readonly" {
				flushBatch()
			}
			currentBatchType = "readonly"
			currentBatch = append(currentBatch, toolUseWithIndex{
				block: block,
				index: i,
				tool:  t,
			})
		} else {
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

	flushBatch()
	return groups
}

// executeParallel runs a batch of concurrency-safe tools in parallel.
func (e *ToolExecutor) executeParallel(parentCtx context.Context, batch []toolUseWithIndex, results []toolResult) {
	if len(batch) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxConcurrency)

	for _, tw := range batch {
		wg.Add(1)
		go func(tw toolUseWithIndex) {
			defer wg.Done()

			if ctx.Err() != nil {
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   "Tool execution aborted due to sibling failure",
					IsError:   true,
				}
				return
			}

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   "Tool execution aborted due to sibling failure",
					IsError:   true,
				}
				return
			}
			defer func() { <-sem }()

			execResult, err := tw.tool.Execute(ctx, tw.block.Input, e.cwd)

			if ctx.Err() != nil {
				results[tw.index] = toolResult{
					ToolUseID: tw.block.ID,
					Content:   "Tool execution aborted due to sibling failure",
					IsError:   true,
				}
				return
			}

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
// For bash batches, failure of one tool aborts subsequent bash tools in the same batch (AC3).
func (e *ToolExecutor) executeSerial(parentCtx context.Context, batch []toolUseWithIndex, results []toolResult) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	for _, tw := range batch {
		if ctx.Err() != nil {
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   "Tool execution aborted due to sibling failure",
				IsError:   true,
			}
			continue
		}

		if tw.tool == nil {
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   fmt.Sprintf("Error: No such tool available: %s", tw.block.Name),
				IsError:   true,
			}
			continue
		}

		execResult, err := tw.tool.Execute(ctx, tw.block.Input, e.cwd)

		if err != nil {
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   fmt.Sprintf("Error executing tool: %v", err),
				IsError:   true,
			}
			// Bash failure aborts siblings in same batch
			if isBashTool(tw.block.Name) {
				cancel()
			}
		} else {
			results[tw.index] = toolResult{
				ToolUseID: tw.block.ID,
				Content:   execResult.Content,
				IsError:   execResult.IsError,
			}
			// Also abort on logical error for bash if it's considered a "failure"
			if isBashTool(tw.block.Name) && execResult.IsError {
				cancel()
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
