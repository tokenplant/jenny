package api

import (
	"fmt"
	"os"
	"regexp"
)

// NormalizationLog records an action taken by the normalization pipeline.
type NormalizationLog struct {
	Pass    string // Name of the normalization pass
	Message string // Description of what was changed
}

// Capabilities represents the capabilities of the API endpoint.
// For this iteration, all capabilities default to enabled.
type Capabilities struct {
	// SupportsPromptCaching indicates whether the endpoint supports prompt caching.
	SupportsPromptCaching bool
	// OriginalAPIKey is the API key used when the session was created.
	// If non-empty and different from the current ANTHROPIC_API_KEY,
	// credential-bound artifacts (like redacted_thinking blocks) must be stripped.
	OriginalAPIKey string
}

// NormalizeMessages is the single gateway for all normalization before JSON serialization.
// It applies universal normalization passes to messages and tools, ensuring compatibility
// across all API providers without requiring provider-specific detection.
//
// Message normalization is handled by the agent package before calling SendMessage.
// This function performs tool normalization and final safety-net transformations.
func NormalizeMessages(messages []Message, tools []ToolParam, caps Capabilities) ([]Message, []ToolParam, []NormalizationLog) {
	var logs []NormalizationLog

	// Normalize tools: inject __arg__ placeholder for empty properties (universal)
	normalizedTools := normalizeToolsUniversal(tools, &logs)

	// Flatten tool_result content (universal)
	messages = flattenToolResultContent(messages)

	// Strip credential-bound artifacts (redacted_thinking) when key mismatch detected
	messages, stripLogs := stripCredentialBoundArtifacts(messages, caps)
	logs = append(logs, stripLogs...)

	// Final safety-net: deduplicate tool_results across all messages
	normalizedMessages := make([]Message, len(messages))
	for i, msg := range messages {
		normalizedMessages[i] = msg
		normalizedMessages[i].ToolResults = deduplicateToolResults(msg.ToolResults)
	}

	return normalizedMessages, normalizedTools, logs
}

// flattenToolResultContent ensures all tool_result blocks have plain string content.
// This is currently a pass-through because ToolResultBlock.Content is already a string,
// but it serves as the universal hook for ensuring provider-agnostic flattening
// before provider-specific SDK conversion.
func flattenToolResultContent(messages []Message) []Message {
	for i := range messages {
		for j := range messages[i].ToolResults {
			// tr.Content is already a string in our internal representation.
			// This pass ensures it stays that way if we ever support complex content.
			_ = messages[i].ToolResults[j].Content
		}
	}
	return messages
}

// normalizeToolsUniversal applies universal normalization to tools.
// Empty input_schema.properties get a __arg__ placeholder to satisfy provider requirements.
func normalizeToolsUniversal(tools []ToolParam, logs *[]NormalizationLog) []ToolParam {
	if len(tools) == 0 {
		return tools
	}

	result := make([]ToolParam, len(tools))
	for i, t := range tools {
		result[i] = t
		// Universal fix: empty properties get a placeholder
		// This was previously MiniMax-specific but is now universal
		if result[i].InputSchema.Properties == nil {
			result[i].InputSchema.Properties = make(map[string]any)
		}
		if len(result[i].InputSchema.Properties) == 0 {
			result[i].InputSchema.Properties["__arg__"] = map[string]any{
				"type":        "string",
				"description": "Placeholder argument for empty schema",
			}
			*logs = append(*logs, NormalizationLog{
				Pass:    "EmptySchemaPlaceholder",
				Message: "Added __arg__ placeholder for tool with empty properties",
			})
		}
	}
	return result
}

// redactedThinkingPattern matches <thinking type="redacted">...</thinking> blocks.
// The pattern captures the opening tag with type="redacted" and any content up to
// the closing </thinking> tag, handling both single-line and multi-line content.
var redactedThinkingPattern = regexp.MustCompile(`<thinking type="redacted"[^>]*>[\s\S]*?</thinking>`)

// stripCredentialBoundArtifacts removes redacted_thinking blocks from assistant messages
// when the session was resumed with a different API key. These signature-bearing blocks
// are bound to the original key and would cause API 400 errors if sent with a different key.
func stripCredentialBoundArtifacts(messages []Message, caps Capabilities) ([]Message, []NormalizationLog) {
	// Skip if no original key was recorded or if keys match
	if caps.OriginalAPIKey == "" || caps.OriginalAPIKey == os.Getenv("ANTHROPIC_API_KEY") {
		return messages, nil
	}

	var logs []NormalizationLog
	totalStripped := 0

	result := make([]Message, len(messages))
	for i, msg := range messages {
		result[i] = msg

		// Only strip from assistant messages
		if msg.Role != "assistant" {
			continue
		}

		original := msg.Content
		stripped := redactedThinkingPattern.ReplaceAllString(msg.Content, "")

		if stripped != original {
			result[i].Content = stripped

			// Count how many blocks were stripped (approximate based on occurrences)
			strippedCount := len(redactedThinkingPattern.FindAllStringIndex(original, -1))
			totalStripped += strippedCount
		}
	}

	if totalStripped > 0 {
		logs = append(logs, NormalizationLog{
			Pass:    "StripCredentialBoundArtifacts",
			Message: fmt.Sprintf("Stripped %d redacted_thinking block(s) from message history", totalStripped),
		})
	}

	return result, logs
}
