// Package agent provides the core agent loop and subagent types.
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/tool"
)

// LocalSubagentRunner runs subagents in the local process.
type LocalSubagentRunner struct {
	tools     []tool.Tool
	denyRules map[string]bool
}

// NewLocalSubagentRunner creates a new LocalSubagentRunner.
func NewLocalSubagentRunner(tools []tool.Tool, denyRules map[string]bool) *LocalSubagentRunner {
	if denyRules == nil {
		denyRules = make(map[string]bool)
	}
	return &LocalSubagentRunner{
		tools:     tools,
		denyRules: denyRules,
	}
}

// RunSubagent runs a subagent with the given parameters.
func (r *LocalSubagentRunner) RunSubagent(ctx context.Context, params tool.SubagentParams) (*tool.SubagentResult, error) {
	// Validate subagent type
	subagentType := FindBuiltin(params.SubagentType)
	if subagentType == nil {
		validTypes := make([]string, 0, len(BuiltinTypes()))
		for _, t := range BuiltinTypes() {
			validTypes = append(validTypes, t.Name)
		}
		return nil, fmt.Errorf("invalid subagent_type %q: valid types are [%s]", params.SubagentType, strings.Join(validTypes, ", "))
	}

	// Build deny list from runner's deny rules
	var denyList []string
	for name := range r.denyRules {
		denyList = append(denyList, name)
	}

	// Get filtered tool allowlist for this subagent type
	allowedToolNames := subagentType.FilterTools(denyList)

	// Build the tool list for the subagent
	var subagentTools []tool.Tool
	for _, toolName := range allowedToolNames {
		t := tool.FindTool(r.tools, toolName)
		if t != nil {
			subagentTools = append(subagentTools, t)
		}
	}

	// Build stream config for the subagent
	streamCfg := StreamConfig{
		Enabled: false, // Subagent runs without streaming
		Verbose: false,
	}

	// Run the subagent synchronously
	output, _, err := RunStream(ctx, params.Prompt, subagentTools, params.CWD, streamCfg, params.Model)
	if err != nil {
		return &tool.SubagentResult{
			Output: output,
		}, err
	}

	return &tool.SubagentResult{
		Output: output,
	}, nil
}

// AsyncSubagentRunner wraps a LocalSubagentRunner to provide async execution.
type AsyncSubagentRunner struct {
	runner *LocalSubagentRunner
}

// NewAsyncSubagentRunner creates a new AsyncSubagentRunner.
func NewAsyncSubagentRunner(tools []tool.Tool, denyRules map[string]bool) *AsyncSubagentRunner {
	return &AsyncSubagentRunner{
		runner: NewLocalSubagentRunner(tools, denyRules),
	}
}

// RunSubagentAsync launches a subagent asynchronously.
// It returns immediately with an AsyncResult without blocking.
func (r *AsyncSubagentRunner) RunSubagentAsync(params tool.SubagentParams) (*tool.AsyncResult, error) {
	// Generate agent ID
	agentID, err := session.NewSessionID()
	if err != nil {
		return nil, fmt.Errorf("generating agent ID: %w", err)
	}

	// Build output file path (placeholder until background tasks is implemented)
	// Format: transcripts/<agentID>.jsonl
	outputFile := agentID + ".jsonl"

	// Launch subagent in goroutine
	go func() {
		_, _ = r.runner.RunSubagent(context.Background(), params)
		// TODO: Store error in output file when background tasks is implemented
	}()

	return &tool.AsyncResult{
		Status:     "async_launched",
		AgentID:    agentID,
		OutputFile: outputFile,
	}, nil
}
