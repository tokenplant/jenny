// Package api provides the Anthropic API client.
package api

import (
	"context"
	"time"
)

// ProviderKind represents the type of provider backend.
type ProviderKind string

const (
	ProviderAnthropic        ProviderKind = "anthropic"
	ProviderOpenAI           ProviderKind = "openai"
	ProviderOpenAIResponses  ProviderKind = "openai_responses"
)

// Provider defines the interface for AI backend providers.
// Each provider implements the SendMessage and SendMessageStream methods
// for communicating with a specific AI API backend.
type Provider interface {
	// SendMessage sends a non-streaming message and returns the response.
	// systemPrompt is the cached stable prefix; systemPromptSuffix is the per-turn dynamic part (no cache control).
	SendMessage(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string) (*Response, error)

	// SendMessageStream sends a streaming message and yields content blocks via the channel.
	// systemPrompt is the cached stable prefix; systemPromptSuffix is the per-turn dynamic part (no cache control).
	SendMessageStream(ctx context.Context, messages []Message, tools []ToolParam, toolResults []ToolResult, systemPrompt string, systemPromptSuffix string, idleTimeout time.Duration) (<-chan StreamContentBlock, *StreamResult)

	// Kind returns the provider kind for debugging/logging.
	Kind() ProviderKind
}

// ProviderWithRetryConfig allows providers to receive retry configuration.
type ProviderWithRetryConfig interface {
	Provider
	SetRetryConfig(cfg RetryConfig)
}

// ProviderWithThinkingConfig allows providers to receive thinking/reasoning configuration.
type ProviderWithThinkingConfig interface {
	Provider
	SetThinkingConfig(cfg ThinkingConfig)
}

// ThinkingConfig holds thinking/reasoning configuration for API providers.
type ThinkingConfig struct {
	BudgetTokens int    // For Anthropic thinking.budget_tokens
	Effort       string // For OpenAI reasoning_config.effort (low/medium/high)
}
