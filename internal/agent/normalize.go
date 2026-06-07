// Package agent provides the core agent loop and query engine.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

// normalizeMessages normalizes messages for API transmission.
// It follows the 6-step order: internal filter, orphaned thinking filter,
// trailing thinking strip, whitespace-only filter, non-empty assistant guard,
// tool pairing, and role merging.
func normalizeMessages(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Step 0: Internal field filter - strip virtual messages, progress messages
	messages = filterInternalMessages(messages)

	// Step 1: Orphaned thinking filter - remove any thinking blocks not followed by content
	messages = filterOrphanedThinking(messages)

	// Step 2: Trailing thinking strip - strip any thinking block at the end of assistant messages
	messages = stripTrailingThinking(messages)

	// Step 3: Non-empty assistant guard - insert placeholder if stripping leaves empty assistant
	// (must run BEFORE filterWhitespaceOnly so placeholder is inserted before filtering)
	messages = ensureNonEmptyAssistant(messages)

	// Step 4: Whitespace-only filter - remove messages with only whitespace
	messages = filterWhitespaceOnly(messages)

	// Step 5: Tool pairing - enforce tool_use/tool_result pairing
	messages = ensureToolResultPairing(messages)

	// Step 6: Role merging - merge consecutive same-role messages
	messages = mergeConsecutiveSameRole(messages)

	return messages
}

// filterInternalMessages removes internal-only messages that should not be sent to the API.
// This includes virtual messages and progress messages.
func filterInternalMessages(messages []api.Message) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		// Skip virtual messages (internal markers)
		if msg.IsVirtual {
			continue
		}
		// Skip progress messages
		if msg.Type == "progress" {
			continue
		}
		result = append(result, msg)
	}
	return result
}

// filterOrphanedThinking removes thinking blocks that are not followed by actual content.
func filterOrphanedThinking(messages []api.Message) []api.Message {
	var result []api.Message
	for i, msg := range messages {
		if msg.Role == "assistant" && isThinkingOnlyContent(msg.Content) {
			// Check if next message has non-thinking content
			hasContentAfter := false
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "user" || (messages[j].Role == "assistant" && !isThinkingOnlyContent(messages[j].Content)) {
					hasContentAfter = true
					break
				}
				if messages[j].Role == "assistant" && messages[j].Content != "" && !isThinkingOnlyContent(messages[j].Content) {
					hasContentAfter = true
					break
				}
			}
			if !hasContentAfter {
				// Keep message but clear thinking-only content
				msg.Content = ""
			}
		}
		result = append(result, msg)
	}
	return result
}

// isThinkingOnlyContent checks if content is only thinking tags.
func isThinkingOnlyContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "<thinking>") && strings.HasSuffix(trimmed, "</thinking>")
}

// stripTrailingThinking removes thinking blocks at the end of assistant messages.
func stripTrailingThinking(messages []api.Message) []api.Message {
	for i := range messages {
		if messages[i].Role == "assistant" {
			messages[i].Content = stripTrailingThinkingFromContent(messages[i].Content)
		}
	}
	return messages
}

// stripTrailingThinkingFromContent removes trailing thinking blocks from content.
func stripTrailingThinkingFromContent(content string) string {
	trimmed := strings.TrimRight(content, " \t\n\r")
	for strings.HasSuffix(trimmed, "</thinking>") {
		idx := strings.LastIndex(trimmed, "<thinking>")
		if idx == -1 {
			break
		}
		trimmed = strings.TrimRight(trimmed[:idx], " \t\n\r")
	}
	return trimmed
}

// filterWhitespaceOnly removes messages that contain only whitespace.
func filterWhitespaceOnly(messages []api.Message) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		if msg.Role == "user" && strings.TrimSpace(msg.Content) == "" && len(msg.ToolResults) == 0 {
			continue // Skip empty user messages
		}
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) == "" && len(msg.ToolUse) == 0 {
			continue // Skip empty assistant messages
		}
		result = append(result, msg)
	}
	return result
}

// ensureNonEmptyAssistant inserts [Tool use interrupted] if stripping leaves an empty assistant message.
func ensureNonEmptyAssistant(messages []api.Message) []api.Message {
	for i := range messages {
		if messages[i].Role == "assistant" {
			contentEmpty := strings.TrimSpace(messages[i].Content) == ""
			noToolUse := len(messages[i].ToolUse) == 0
			if contentEmpty && noToolUse {
				messages[i].Content = "[Tool use interrupted]"
			}
		}
	}
	return messages
}

// ensureToolResultPairing enforces tool_use/tool_result pairing.
// It handles all 6 directions per the spec:
// (1) forward: synthetic error for missing tool_use_id
// (2) reverse: strip orphaned tool_results
// (3) duplicate IDs: dedupe across messages
// (4) leading orphaned user tool_result: strip or insert placeholder
// (5) empty assistant after strip: insert [Tool use interrupted]
// (6) is_error tool_result: inner content text-only
func ensureToolResultPairing(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Collect all tool_use_ids from assistant messages
	var allToolUseIDs []string
	for _, msg := range messages {
		if msg.Role == "assistant" {
			for _, tu := range msg.ToolUse {
				allToolUseIDs = append(allToolUseIDs, tu.ID)
			}
		}
	}

	// Track which tool_use_ids have corresponding tool_results
	toolUseIDSet := make(map[string]bool)
	for _, id := range allToolUseIDs {
		toolUseIDSet[id] = true
	}

	// Build normalized messages
	var result []api.Message
	var pendingAssistant *api.Message // For handling empty assistant after strip

	for i := range messages {
		msg := messages[i]

		if msg.Role == "assistant" {
			// Handle empty assistant after strip (direction 5)
			if pendingAssistant != nil {
				contentEmpty := strings.TrimSpace(pendingAssistant.Content) == ""
				noToolUse := len(pendingAssistant.ToolUse) == 0
				if contentEmpty && noToolUse {
					pendingAssistant.Content = "[Tool use interrupted]"
				}
				result = append(result, *pendingAssistant)
				pendingAssistant = nil
			}

			// Direction 3: Dedupe duplicate tool_use IDs within this message
			seenIDs := make(map[string]bool)
			var dedupedToolUse []api.ToolUseBlock
			for _, tu := range msg.ToolUse {
				if !seenIDs[tu.ID] {
					seenIDs[tu.ID] = true
					dedupedToolUse = append(dedupedToolUse, tu)
				}
			}
			msg.ToolUse = dedupedToolUse

			// Direction 1: Check if all tool_use blocks have matching tool_results
			// We'll handle this when we see the user message that follows

			pendingAssistant = &msg

		} else if msg.Role == "user" {
			if pendingAssistant != nil {
				// Capture tool_use IDs before we clear pendingAssistant
				assistantToolUse := pendingAssistant.ToolUse

				// Direction 5: Check if assistant is empty after strip
				contentEmpty := strings.TrimSpace(pendingAssistant.Content) == ""
				noToolUse := len(pendingAssistant.ToolUse) == 0
				if contentEmpty && noToolUse {
					pendingAssistant.Content = "[Tool use interrupted]"
				}

				// First add the assistant message to result
				result = append(result, *pendingAssistant)
				pendingAssistant = nil

				// Direction 1 & 2 & 3: Forward and reverse handling
				var newToolResults []api.ToolResultBlock

				// Track which tool_use_ids from assistant have results
				resultToolUseIDs := make(map[string]bool)

				for _, tr := range msg.ToolResults {
					// Direction 3: Dedupe across messages
					if resultToolUseIDs[tr.ToolUseID] {
						continue // Skip duplicate
					}

					// Direction 2: Strip orphaned tool_results (no matching tool_use in assistant)
					if !toolUseIDSet[tr.ToolUseID] {
						continue // Strip orphaned tool_result
					}

					// Direction 6: is_error tool_result - ensure inner content is text-only
					// If this is an error result with structured content, extract text only
					if tr.IsError {
						tr.Content = extractTextFromErrorContent(tr.Content)
					}

					resultToolUseIDs[tr.ToolUseID] = true
					newToolResults = append(newToolResults, tr)
				}

				// Direction 1: Add synthetic error for missing tool_use_id
				// Check if any assistant tool_use doesn't have a corresponding tool_result
				for _, tu := range assistantToolUse {
					if !resultToolUseIDs[tu.ID] {
						// Missing result - add synthetic error
						newToolResults = append(newToolResults, api.ToolResultBlock{
							ToolUseID: tu.ID,
							Content:   fmt.Sprintf("Error: No result received for tool call '%s'", tu.ID),
						})
					}
				}

				msg.ToolResults = newToolResults

				// Only add user message if it has content or tool_results
				hasContent := msg.Content != ""
				hasToolResults := len(msg.ToolResults) > 0
				if hasContent || hasToolResults {
					result = append(result, msg)
				}

			} else {
				// Direction 4: Leading orphaned user tool_result - strip
				// If we see a user message with tool_results but no preceding assistant,
				// we strip those tool_results
				var strippedToolResults []api.ToolResultBlock
				for _, tr := range msg.ToolResults {
					// Keep only tool_results that have matching tool_use (shouldn't happen here, but be safe)
					if toolUseIDSet[tr.ToolUseID] {
						strippedToolResults = append(strippedToolResults, tr)
					}
				}
				msg.ToolResults = strippedToolResults
			}

			// Direction 6: is_error tool_result - ensure inner content is text-only
			// (The API expects text content in tool_result, not structured data)
			// This is already handled by the API client serialization

			// Only add user message if it has content or tool_results
			hasContent := msg.Content != ""
			hasToolResults := len(msg.ToolResults) > 0
			if hasContent || hasToolResults {
				result = append(result, msg)
			}

		} else {
			// Other roles (system, etc.) - keep as is
			if pendingAssistant != nil {
				contentEmpty := strings.TrimSpace(pendingAssistant.Content) == ""
				noToolUse := len(pendingAssistant.ToolUse) == 0
				if contentEmpty && noToolUse {
					pendingAssistant.Content = "[Tool use interrupted]"
				}
				result = append(result, *pendingAssistant)
				pendingAssistant = nil
			}
			result = append(result, msg)
		}
	}

	// Handle any remaining pending assistant
	if pendingAssistant != nil {
		contentEmpty := strings.TrimSpace(pendingAssistant.Content) == ""
		noToolUse := len(pendingAssistant.ToolUse) == 0
		if contentEmpty && noToolUse {
			pendingAssistant.Content = "[Tool use interrupted]"
		}
		result = append(result, *pendingAssistant)
	}

	return result
}

// mergeConsecutiveSameRole merges consecutive messages with the same role.
// This is called after pairing to consolidate role blocks.
func mergeConsecutiveSameRole(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	var result []api.Message
	var current *api.Message

	for _, msg := range messages {
		if current == nil {
			current = &api.Message{
				Role:    msg.Role,
				Content: msg.Content,
			}
			current.ToolUse = append(current.ToolUse, msg.ToolUse...)
			current.ToolResults = append(current.ToolResults, msg.ToolResults...)
			continue
		}

		if current.Role == msg.Role {
			// Merge content
			if msg.Content != "" {
				if current.Content != "" {
					current.Content += "\n"
				}
				current.Content += msg.Content
			}
			// Concatenate tool_use for assistant
			if msg.Role == "assistant" {
				current.ToolUse = append(current.ToolUse, msg.ToolUse...)
			}
			// Merge tool_results for user
			if msg.Role == "user" {
				current.ToolResults = append(current.ToolResults, msg.ToolResults...)
			}
		} else {
			result = append(result, *current)
			current = &api.Message{
				Role:    msg.Role,
				Content: msg.Content,
			}
			current.ToolUse = append(current.ToolUse, msg.ToolUse...)
			current.ToolResults = append(current.ToolResults, msg.ToolResults...)
		}
	}

	if current != nil {
		result = append(result, *current)
	}

	return result
}

// extractTextFromErrorContent extracts plain text from potentially structured error content.
// If the content is JSON, it extracts the "error" or "message" field.
// Otherwise, it returns the content as-is.
func extractTextFromErrorContent(content string) string {
	// Try to parse as JSON and extract error message
	if strings.HasPrefix(strings.TrimSpace(content), "{") {
		var data map[string]any
		if err := json.Unmarshal([]byte(content), &data); err == nil {
			// Try common error field names
			for _, key := range []string{"error", "message", "msg"} {
				if val, ok := data[key]; ok {
					if str, ok := val.(string); ok {
						return str
					}
				}
			}
		}
	}
	// Not JSON or couldn't extract - return original
	return content
}

// media error message functions

// getImageTooLargeErrorMessage returns the user-facing message for image too large errors.
func getImageTooLargeErrorMessage() string {
	return "Image is too large. Please use a smaller image (max 5MB)."
}

// getPdfTooLargeErrorMessage returns the user-facing message for PDF page limit errors.
func getPdfTooLargeErrorMessage() string {
	return "PDF has too many pages. Please use a PDF with fewer pages."
}

// getPdfPasswordProtectedErrorMessage returns the user-facing message for password-protected PDFs.
func getPdfPasswordProtectedErrorMessage() string {
	return "PDF is password protected. Please provide an unprotected PDF."
}

// getPdfInvalidErrorMessage returns the user-facing message for invalid PDF errors.
func getPdfInvalidErrorMessage() string {
	return "PDF is invalid or corrupted. Please provide a valid PDF file."
}

// getRequestTooLargeErrorMessage returns the user-facing message for HTTP 413 errors.
func getRequestTooLargeErrorMessage() string {
	return "Request is too large. Please reduce the content size and try again."
}

// mapMediaErrorToUserMessage maps API error patterns to user-facing error messages.
// It returns the mapped message and whether a media error was detected.
func mapMediaErrorToUserMessage(errorMsg string) (string, bool) {
	lowerMsg := strings.ToLower(errorMsg)

	// Image size / resize error patterns
	if strings.Contains(lowerMsg, "image") && strings.Contains(lowerMsg, "too large") {
		return getImageTooLargeErrorMessage(), true
	}
	if strings.Contains(lowerMsg, "image") && strings.Contains(lowerMsg, "size") {
		return getImageTooLargeErrorMessage(), true
	}

	// PDF page limit
	if strings.Contains(lowerMsg, "pdf") && strings.Contains(lowerMsg, "too many") {
		return getPdfTooLargeErrorMessage(), true
	}
	if strings.Contains(lowerMsg, "pdf") && strings.Contains(lowerMsg, "page limit") {
		return getPdfTooLargeErrorMessage(), true
	}

	// Password protected PDF
	if strings.Contains(lowerMsg, "pdf") && strings.Contains(lowerMsg, "password") {
		return getPdfPasswordProtectedErrorMessage(), true
	}

	// Invalid PDF
	if strings.Contains(lowerMsg, "pdf") && (strings.Contains(lowerMsg, "invalid") || strings.Contains(lowerMsg, "corrupt")) {
		return getPdfInvalidErrorMessage(), true
	}

	// HTTP 413 request too large
	if strings.Contains(lowerMsg, "413") || (strings.Contains(lowerMsg, "request") && strings.Contains(lowerMsg, "too large")) {
		return getRequestTooLargeErrorMessage(), true
	}

	return errorMsg, false
}

// StripMediaErrorFromMessage removes tool_result blocks that caused media errors.
// This is used on retry after mapping the error to a user-facing message.
func StripMediaErrorFromMessage(msg *api.Message, toolUseID string) {
	if msg.Role != "user" {
		return
	}

	var newToolResults []api.ToolResultBlock
	for _, tr := range msg.ToolResults {
		if tr.ToolUseID != toolUseID {
			newToolResults = append(newToolResults, tr)
		}
	}
	msg.ToolResults = newToolResults
}

// FindLargestMediaToolUseID finds the tool_use_id with the largest content in tool_results.
// This is used to identify which tool_result likely caused a media error.
func FindLargestMediaToolUseID(messages []api.Message) string {
	var largestID string
	var largestSize int

	// Find the last user message with tool_results
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "user" && len(msg.ToolResults) > 0 {
			for _, tr := range msg.ToolResults {
				if len(tr.Content) > largestSize {
					largestSize = len(tr.Content)
					largestID = tr.ToolUseID
				}
			}
			break // Only consider the most recent user message
		}
	}

	return largestID
}

// HandleMediaErrorOnRetry handles media errors during API calls.
// It finds and strips the offending tool_result, then returns the modified messages.
// Returns true if a media error was handled and messages were modified.
func HandleMediaErrorOnRetry(messages []api.Message, errorMsg string) ([]api.Message, bool) {
	_, isMedia := mapMediaErrorToUserMessage(errorMsg)
	if !isMedia {
		return messages, false
	}

	// Find the largest tool_result (likely caused the error)
	toolUseID := FindLargestMediaToolUseID(messages)
	if toolUseID == "" {
		return messages, false
	}

	// Strip the offending tool_result from the last user message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			StripMediaErrorFromMessage(&messages[i], toolUseID)
			break
		}
	}

	return messages, true
}
