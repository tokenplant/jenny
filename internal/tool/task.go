package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// ForkChildKey is the context key for checking if we're in a fork child.
// Exported so the agent package can use the same key for consistent context lookups.
var ForkChildKey = "agent.forkChild"

// SubagentParams holds parameters for running a subagent.
type SubagentParams struct {
	Prompt          string
	Description     string
	SubagentType    string
	Model           string
	CWD             string
	Isolation       string
	RunInBackground bool
}

// SubagentResult holds the result of a subagent execution.
type SubagentResult struct {
	Output  string
	AgentID string
}

// SubagentRunner runs subagents with typed tool allowlists.
type SubagentRunner interface {
	RunSubagent(ctx context.Context, params SubagentParams) (*SubagentResult, error)
}

// AsyncRunner can launch subagents asynchronously.
type AsyncRunner interface {
	RunSubagentAsync(params SubagentParams) (*AsyncResult, error)
}

// AsyncResult holds the result of an async subagent launch.
type AsyncResult struct {
	Status     string `json:"status"`
	AgentID    string `json:"agent_id"`
	OutputFile string `json:"output_file"`
}

// AgentTool provides subagent spawning capability.
type AgentTool struct {
	runner      SubagentRunner
	asyncRunner AsyncRunner
}

// NewAgentTool creates a new AgentTool with the given subagent runner.
func NewAgentTool(runner SubagentRunner, asyncRunner AsyncRunner) *AgentTool {
	return &AgentTool{runner: runner, asyncRunner: asyncRunner}
}

// Name returns the tool name.
func (t *AgentTool) Name() string {
	return "agent"
}

// Description returns a description of the tool.
func (t *AgentTool) Description() string {
	return "Spawns a subagent with type-filtered tool allowlist. " +
		"Use subagent_type to select the agent type (explore, plan, shell, verification, general-purpose). " +
		"Legacy alias: task. " +
		"When run_in_background is false (default), waits for completion and returns text. " +
		"When run_in_background is true, returns immediately with async launch info."
}

// InputSchema returns the JSON schema for tool input.
func (t *AgentTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "The instruction prompt for the subagent",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Short label describing the task",
			},
			"subagent_type": map[string]any{
				"type":        "string",
				"description": "Built-in subagent type: explore, plan, shell, verification, general-purpose",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override (sonnet, opus, haiku, or full model name)",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "If true, launch subagent asynchronously without blocking",
			},
			"isolation": map[string]any{
				"type":        "string",
				"description": "Isolation mode: worktree for temp worktree, none otherwise",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Working directory override for the subagent",
			},
		},
		"required": []string{"prompt", "subagent_type"},
	}
}

// isForkChild checks if the current execution context indicates we're in a subagent.
// This is used to block recursive fork (AC1).
func isForkChild(ctx context.Context) bool {
	if v := ctx.Value(ForkChildKey); v != nil {
		if b, ok := v.(bool); ok && b {
			return true
		}
	}
	return false
}

// Execute runs the agent tool with the given input.
func (t *AgentTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	// AC1: Check for recursive fork blocking via context
	if isForkChild(ctx) {
		return &ToolResult{
			Content: "recursive fork not allowed",
			IsError: true,
		}, nil
	}

	// Extract required parameters
	prompt, ok := input["prompt"].(string)
	if !ok || prompt == "" {
		return &ToolResult{
			Content: "prompt is required",
			IsError: true,
		}, nil
	}

	subagentType, ok := input["subagent_type"].(string)
	if !ok || subagentType == "" {
		return &ToolResult{
			Content: "subagent_type is required",
			IsError: true,
		}, nil
	}

	// Extract optional parameters
	var description string
	if desc, ok := input["description"].(string); ok {
		description = desc
	}

	var model string
	if m, ok := input["model"].(string); ok {
		model = m
	}

	var runInBackground bool
	if rb, ok := input["run_in_background"].(bool); ok {
		runInBackground = rb
	}

	var isolation string
	if iso, ok := input["isolation"].(string); ok {
		isolation = iso
	}

	var subagentCWD string
	if c, ok := input["cwd"].(string); ok {
		subagentCWD = c
	}

	// Build subagent params
	params := SubagentParams{
		Prompt:          prompt,
		Description:     description,
		SubagentType:    subagentType,
		Model:           model,
		CWD:             subagentCWD,
		Isolation:       isolation,
		RunInBackground: runInBackground,
	}

	// Handle async mode
	if runInBackground {
		if t.asyncRunner != nil {
			result, err := t.asyncRunner.RunSubagentAsync(params)
			if err != nil {
				return &ToolResult{
					Content: fmt.Sprintf("async launch failed: %v", err),
					IsError: true,
				}, nil
			}
			// Marshal as JSON
			data, _ := json.Marshal(result)
			return &ToolResult{
				Content: string(data),
				IsError: false,
			}, nil
		}
		// Fallback: async not available
		return &ToolResult{
			Content: "async mode not available: background tasks not yet implemented",
			IsError: true,
		}, nil
	}

	// Run synchronously
	result, err := t.runner.RunSubagent(ctx, params)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("subagent error: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: result.Output,
		IsError: false,
	}, nil
}
