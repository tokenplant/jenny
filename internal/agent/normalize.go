// Package agent provides the core agent loop and query engine.
package agent

import (
	"fmt"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

// NormalizeMessagesAPI normalizes messages for API transmission.
// It applies content-level fixes AND structural transforms (tool pairing, role merging).
// Used by the compaction path only — see normalizeNewMessage for per-turn use.
func NormalizeMessagesAPI(messages []api.Message) []api.Message {
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
	// NOTE: This is the primary cache buster in the normal turn path.
	// It is only called here (compaction path), not in the per-turn engine loop.
	messages = mergeConsecutiveSameRole(messages)

	return messages
}

// normalizeNewMessage applies content-level normalization to a single message
// without any structural transforms. This is safe to call on messages before
// appending them to the history, since it never changes message boundaries or
// merges adjacent messages — preserving cache continuity across turns.
func normalizeNewMessage(msg api.Message) api.Message {
	// Strip virtual marker
	msg.IsVirtual = false
	// Strip progress type
	if msg.Type == "progress" {
		msg.Type = ""
	}
	// Strip orphaned thinking-only content
	if msg.Role == api.RoleAssistant && isThinkingOnlyContent(msg.Content) {
		msg.Content = ""
	}
	// Strip trailing thinking
	msg.Content = stripTrailingThinkingFromContent(msg.Content)
	// Ensure non-empty assistant has a placeholder
	if msg.Role == api.RoleAssistant && strings.TrimSpace(msg.Content) == "" && len(msg.ToolUse) == 0 {
		msg.Content = "[Tool use interrupted]"
	}
	return msg
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
		if msg.Role == api.RoleAssistant && isThinkingOnlyContent(msg.Content) {
			// Check if next message has non-thinking content
			hasContentAfter := false
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == api.RoleUser || (messages[j].Role == api.RoleAssistant && !isThinkingOnlyContent(messages[j].Content)) {
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

// isThinkingOnlyContent checks if content consists solely of thinking blocks.
func isThinkingOnlyContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	// Strip all <thinking>...</thinking> blocks and check if anything remains
	for {
		start := strings.Index(trimmed, "<thinking>")
		if start == -1 {
			break
		}
		end := strings.Index(trimmed[start:], "</thinking>")
		if end == -1 {
			break
		}
		end += start + len("</thinking>")
		trimmed = trimmed[:start] + trimmed[end:]
	}
	return strings.TrimSpace(trimmed) == ""
}

// stripTrailingThinking removes thinking blocks at the end of assistant messages.
func stripTrailingThinking(messages []api.Message) []api.Message {
	for i := range messages {
		if messages[i].Role == api.RoleAssistant {
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
		if msg.Role == api.RoleUser && strings.TrimSpace(msg.Content) == "" && len(msg.ToolResults) == 0 {
			continue // Skip empty user messages
		}
		if msg.Role == api.RoleAssistant && strings.TrimSpace(msg.Content) == "" && len(msg.ToolUse) == 0 {
			continue // Skip empty assistant messages
		}
		result = append(result, msg)
	}
	return result
}

// ensureNonEmptyAssistant inserts [Tool use interrupted] if stripping leaves an empty assistant message.
func ensureNonEmptyAssistant(messages []api.Message) []api.Message {
	for i := range messages {
		if messages[i].Role == api.RoleAssistant {
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
		if msg.Role == api.RoleAssistant {
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

		if msg.Role == api.RoleAssistant {
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

		} else if msg.Role == api.RoleUser {
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
			if msg.Role == api.RoleAssistant {
				current.ToolUse = append(current.ToolUse, msg.ToolUse...)
			}
			// Merge tool_results for user (dedup by ToolUseID - last-writer-wins)
			if msg.Role == api.RoleUser {
				// Map ToolUseID -> index in current.ToolResults for last-writer-wins
				seenIDToIdx := make(map[string]int)
				for i, tr := range current.ToolResults {
					seenIDToIdx[tr.ToolUseID] = i
				}
				for _, tr := range msg.ToolResults {
					if idx, exists := seenIDToIdx[tr.ToolUseID]; exists {
						// Replace existing entry (last writer wins)
						current.ToolResults[idx] = tr
					} else {
						current.ToolResults = append(current.ToolResults, tr)
						seenIDToIdx[tr.ToolUseID] = len(current.ToolResults) - 1
					}
				}
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
