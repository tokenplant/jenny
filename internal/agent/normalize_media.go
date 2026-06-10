// Package agent provides the core agent loop and query engine.
package agent

import (
	"encoding/json"
	"strings"

	"github.com/ipy/jenny/internal/api"
)

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
