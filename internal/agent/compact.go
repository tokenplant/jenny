// Package agent provides the core agent loop and query engine.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
)

const (
	// BLOCKING_BUFFER_TOKENS is the buffer subtracted from effective context window
	// to determine the blocking limit when auto-compact is disabled.
	BLOCKING_BUFFER_TOKENS = 3_000

	// WARNING_BUFFER_TOKENS is subtracted from autoCompactThreshold to determine
	// when to emit a warning event.
	WARNING_BUFFER_TOKENS = 20_000

	// MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES is the number of consecutive failures
	// before the circuit breaker trips and skips all further auto-compact attempts.
	MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3

	// SUMMARY_MAX_TOKENS is the maximum output tokens allocated for summary.
	SUMMARY_MAX_TOKENS = 20_000

	// minAutoCompactBuffer is the floor for the auto-compact buffer.
	minAutoCompactBuffer = 13_000
)

// CompactConfig holds configuration for compaction.
type CompactConfig struct {
	// Environment overrides
	DisableCompact     bool
	DisableAutoCompact bool
	AutoCompactWindow  int //0 means use modelContextWindow

	// Model parameters
	ModelContextWindow   int
	ModelMaxOutputTokens int

	// Session state
	CompactFailCount int

	// Feature flags
	SessionMemoryEnabled bool
}

// effectiveContextWindow returns the effective context window after accounting
// for model max output tokens reserve.
func (c CompactConfig) effectiveContextWindow() int {
	return c.ModelContextWindow - c.ModelMaxOutputTokens
}

// autoCompactBuffer returns the buffer for auto-compact threshold, scaled to
// the model's max output tokens: max(modelMaxOutputTokens + 5000, minAutoCompactBuffer).
func (c CompactConfig) autoCompactBuffer() int {
	return max(c.ModelMaxOutputTokens+5_000, minAutoCompactBuffer)
}

// autoCompactThreshold returns the token count at which auto-compact triggers.
func (c CompactConfig) autoCompactThreshold() int {
	return c.effectiveContextWindow() - c.autoCompactBuffer()
}

// warningThreshold returns the token count at which to emit a warning event.
func (c CompactConfig) warningThreshold() int {
	return c.autoCompactThreshold() - WARNING_BUFFER_TOKENS
}

// blockingLimit returns the token count at which to block API calls when
// auto-compact is disabled.
func (c CompactConfig) blockingLimit() int {
	return c.effectiveContextWindow() - BLOCKING_BUFFER_TOKENS
}

// checkCompactThreshold returns true if estimated tokens exceed the auto-compact
// threshold and auto-compact should trigger.
// querySource is checked to skip auto-compact for 'compact' and 'session_memory' sources.
func (c CompactConfig) checkCompactThreshold(estimatedTokens int, querySource string) bool {
	// AC1: Skip auto-compact when querySource is 'compact' or 'session_memory'
	if querySource == "compact" || querySource == "session_memory" {
		return false
	}
	if c.DisableCompact || c.DisableAutoCompact {
		return false
	}
	return estimatedTokens >= c.autoCompactThreshold()
}

// checkWarningThreshold returns true if estimated tokens exceed the warning
// threshold.
func (c CompactConfig) checkWarningThreshold(estimatedTokens int) bool {
	return estimatedTokens >= c.warningThreshold()
}

// blockIfOverLimit returns an error if estimated tokens exceed the blocking
// limit and auto-compact is disabled.
func (c CompactConfig) blockIfOverLimit(estimatedTokens int, querySource string) error {
	// compact and session_memory sources never hard-block
	if querySource == "compact" || querySource == "session_memory" {
		return nil
	}

	if c.DisableAutoCompact || c.DisableCompact {
		if estimatedTokens >= c.blockingLimit() {
			return &PromptTooLongError{EstimatedTokens: estimatedTokens, Limit: c.blockingLimit()}
		}
	}
	return nil
}

// PromptTooLongError is returned when the estimated tokens exceed the blocking limit.
type PromptTooLongError struct {
	EstimatedTokens int
	Limit           int
}

func (e *PromptTooLongError) Error() string {
	return fmt.Sprintf("prompt too long: estimated %d tokens exceeds blocking limit %d", e.EstimatedTokens, e.Limit)
}

// estimateTokens estimates the token count for a message chain using a
// charset-aware heuristic. ASCII/Latin text uses ~4 chars/token; multi-byte
// characters (CJK, emoji) use ~1.5 chars/token.
func estimateTokens(messages []api.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateStringTokens(msg.Content)
		for _, tu := range msg.ToolUse {
			total += len(tu.Name) + 50
			inputBytes, _ := json.Marshal(tu.Input)
			total += estimateStringTokens(string(inputBytes))
		}
		for _, tr := range msg.ToolResults {
			total += estimateStringTokens(tr.Content) + 50
		}
	}
	return total
}

// estimateStringTokens estimates the token count for a string using charset-aware
// heuristics. ASCII chars → ~4 chars/token, multi-byte runes → ~1.5 chars/token.
func estimateStringTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	asciiChars := 0
	multiByteRunes := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r < 128 {
			asciiChars++
		} else {
			multiByteRunes++
		}
		i += size
	}
	asciiTokens := asciiChars * 10 / 40  // asciiChars / 4.0
	mbTokens := multiByteRunes * 10 / 15 // multiByteRunes / 1.5
	return asciiTokens + mbTokens
}

// newCompactConfigForModel creates a CompactConfig using model-specific parameters
// from api.ModelParams, with environment variable overrides.
func newCompactConfigForModel(model string) CompactConfig {
	params := api.ModelParams(model)
	cfg := CompactConfig{
		ModelContextWindow:   params.ContextWindow,
		ModelMaxOutputTokens: params.MaxOutputTokens,
		DisableCompact:       os.Getenv("DISABLE_COMPACT") != "",
		DisableAutoCompact:   os.Getenv("DISABLE_AUTO_COMPACT") != "",
		SessionMemoryEnabled: os.Getenv("ENABLE_SESSION_MEMORY") != "",
	}

	if envWindow := readEnvInt("AUTO_COMPACT_WINDOW", 0); envWindow > 0 {
		cfg.ModelContextWindow = envWindow
	}

	return cfg
}

// readEnvInt reads an integer from an environment variable.
func readEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

// compactMessages performs context compaction on the message chain.
// It returns the compacted messages and any error encountered.
func (e *QueryEngine) compactMessages(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt string) ([]api.Message, error) {
	log.Debug("Starting context compaction", "messageCount", len(messages))

	// Step 1: Try session-memory compaction first (when enabled)
	if cfg.SessionMemoryEnabled {
		compacted, err := e.trySessionMemoryCompact(ctx, messages, cfg, systemPrompt)
		if err == nil {
			return compacted, nil
		}
		log.Debug("Session memory compaction not available, falling back to summary agent")
	}

	// Step 2: Fork summary agent
	return e.forkSummaryAgent(ctx, messages, cfg, systemPrompt)
}

// trySessionMemoryCompact attempts compaction via session memory.
// Currently returns ErrNotImplemented.
func (e *QueryEngine) trySessionMemoryCompact(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt string) ([]api.Message, error) {
	// Session memory compaction is P3 - return not implemented
	return nil, fmt.Errorf("session memory compaction not implemented")
}

// forkSummaryAgent forks a single-turn API call to generate a summary of the
// conversation history.
func (e *QueryEngine) forkSummaryAgent(ctx context.Context, messages []api.Message, cfg CompactConfig, systemPrompt string) ([]api.Message, error) {
	// Prepare messages for summary call (strip images/documents)
	summaryMessages := prepareSummaryMessages(messages)

	// Create a summary system prompt
	summarySystemPrompt := "You are a helpful assistant that summarizes conversations concisely. Provide a brief summary of the key points from the conversation above. Focus on the essential information, decisions made, and any outstanding tasks or questions."

	// Make the summary API call with retries
	var lastErr error
	maxRetries := 3

	for attempt := range maxRetries {
		if attempt > 0 {
			log.Debug("Retrying summary agent", "attempt", attempt+1)
			// Drop oldest API-round group from head
			summaryMessages = dropOldestAPIRoundGroup(summaryMessages)
			if len(summaryMessages) == 0 {
				return nil, fmt.Errorf("cannot retry summary: no messages remaining after dropping oldest group")
			}
		}

		resp, err := e.client.SendMessage(ctx, summaryMessages, nil, nil, summarySystemPrompt, "")
		if err != nil {
			lastErr = err
			// Check if it's a prompt-too-long error
			if isPromptTooLongError(err) {
				continue // Retry with fewer messages
			}
			return nil, err
		}

		// Extract summary text from response
		var summaryText strings.Builder
		for _, block := range resp.Content {
			if block.Type == "text" {
				summaryText.WriteString(block.Text)
			}
		}

		if summaryText.String() == "" {
			lastErr = fmt.Errorf("empty summary response")
			continue
		}

		// Build compacted chain:
		// boundaryMarker → summaryMessages → messagesToKeep → attachments → hookResults
		return buildCompactedChain(messages, summaryText.String()), nil
	}

	return nil, fmt.Errorf("summary agent failed after %d attempts: %w", maxRetries, lastErr)
}

// isPromptTooLongError checks if an error indicates a prompt-too-long condition.
func isPromptTooLongError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "prompt too long") ||
		strings.Contains(errStr, "too many tokens") ||
		strings.Contains(errStr, "context length") ||
		strings.Contains(errStr, "413")
}

// buildSummaryPrompt builds a prompt for the summary agent describing what to summarize.
func buildSummaryPrompt(messages []api.Message) string {
	var sb strings.Builder
	sb.WriteString("Please summarize the following conversation concisely. ")
	sb.WriteString("Focus on key points, decisions, and any outstanding tasks.\n\n")

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			sb.WriteString(fmt.Sprintf("User: %s\n", truncateContent(msg.Content, 500)))
		case "assistant":
			sb.WriteString(fmt.Sprintf("Assistant: %s\n", truncateContent(msg.Content, 1000)))
		}
	}

	return sb.String()
}

// truncateContent truncates content to a maximum number of runes.
func truncateContent(content string, maxLen int) string {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return content
	}
	return string(runes[:maxLen]) + "..."
}

// prepareSummaryMessages prepares messages for the summary API call by stripping
// images and documents and replacing them with markers.
func prepareSummaryMessages(messages []api.Message) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		// Make a copy to avoid modifying original
		processedMsg := msg

		// Strip image/document content from user messages
		if msg.Role == "user" {
			processedMsg.Content = stripMediaMarkers(msg.Content)
		}

		result = append(result, processedMsg)
	}
	return result
}

// Pre-compiled regexps for stripMediaMarkers (avoid recompilation on every call).
var (
	base64ImageRe   = regexp.MustCompile(`data:image/[^;]+;base64,[A-Za-z0-9+/=]{100,}`)
	base64PdfRe     = regexp.MustCompile(`data:application/pdf[^,;]*,[A-Za-z0-9+/=]{100,}`)
	markdownImageRe = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	imageURLRe      = regexp.MustCompile(`https?://[^)\s"']+\.(png|jpg|jpeg|gif|bmp|webp|svg)(\?[^)\s"']*)?`)
	pdfURLRe        = regexp.MustCompile(`https?://[^)\s"']+\.pdf(\?[^)\s"']*)?`)
)

// stripMediaMarkers replaces image/document content with markers.
// This prevents large media from being sent to the summary agent.
func stripMediaMarkers(content string) string {
	if content == "" {
		return content
	}

	content = base64ImageRe.ReplaceAllString(content, "[image]")
	content = base64PdfRe.ReplaceAllString(content, "[document]")
	content = markdownImageRe.ReplaceAllString(content, "[image]")
	content = imageURLRe.ReplaceAllString(content, "[image]")
	content = pdfURLRe.ReplaceAllString(content, "[document]")

	return content
}

// dropOldestAPIRoundGroup drops the oldest API-round group from the messages.
// An API-round group consists of: user message, assistant message (with tool_use),
// and tool_result messages.
func dropOldestAPIRoundGroup(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Find the first user message (start of an API round)
	startIdx := -1
	for i, msg := range messages {
		if msg.Role == "user" {
			startIdx = i
			break
		}
	}

	if startIdx == -1 || startIdx >= len(messages)-1 {
		return messages
	}

	// Find the end of this API round (next user message or end of array)
	endIdx := startIdx + 1
	for endIdx < len(messages) {
		if messages[endIdx].Role == "user" {
			break
		}
		endIdx++
	}

	// Drop from startIdx to endIdx (exclusive)
	result := make([]api.Message, 0, len(messages)-(endIdx-startIdx))
	result = append(result, messages[:startIdx]...)
	result = append(result, messages[endIdx:]...)
	return result
}

// buildCompactedChain builds the new message chain after compaction.
// Order: boundaryMarker → summaryMessages → messagesToKeep → attachments → hookResults
//
// AC7: The cut point never splits a tool_use/tool_result pair. If the boundary
// falls between an assistant with tool_use and its user with tool_results,
// the boundary is moved earlier to include the full pair.
func buildCompactedChain(originalMessages []api.Message, summary string) []api.Message {
	messagesToKeep := 10
	if len(originalMessages) <= messagesToKeep {
		// Nothing to trim — keep everything
	} else {
		cutIdx := len(originalMessages) - messagesToKeep
		// If the message at cutIdx is a user with tool_results, the preceding
		// assistant (cutIdx-1) likely has the matching tool_use. Include both.
		if cutIdx > 0 && originalMessages[cutIdx].Role == "user" && len(originalMessages[cutIdx].ToolResults) > 0 {
			cutIdx--
		}
		originalMessages = originalMessages[cutIdx:]
	}

	var result []api.Message
	result = append(result, api.Message{
		Role:    "system",
		Content: fmt.Sprintf("[Context boundary: earlier conversation summarized]\n\nPrevious summary:\n%s", summary),
	})
	result = append(result, originalMessages...)
	return result
}

// normalizeCompactedChain applies post-compact normalization to ensure
// the compacted chain passes tool/thinking pairing rules.
func normalizeCompactedChain(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Step 1: Filter orphaned thinking-only messages
	messages = filterOrphanedThinking(messages)

	// Step 2: Strip trailing thinking from last assistant
	messages = stripTrailingThinking(messages)

	// Step 3: Ensure non-empty assistant (insert placeholder if stripped content left empty)
	// (must run BEFORE filterWhitespaceOnly so placeholder is inserted before filtering)
	messages = ensureNonEmptyAssistant(messages)

	// Step 4: Filter whitespace-only assistant messages
	messages = filterWhitespaceOnly(messages)

	// Step 5: Ensure tool result pairing
	messages = ensureToolResultPairing(messages)

	return messages
}

// EmitCompactWarning emits a warning event when estimated tokens approach
// the compact threshold.
func EmitCompactWarning(estimatedTokens int, threshold int) {
	log.Warn("Context compaction warning: token count approaching threshold",
		"estimatedTokens", estimatedTokens,
		"threshold", threshold,
		"buffer", threshold-estimatedTokens)
}

// isUserAbortError checks if an error message indicates a user-initiated abort.
// User aborts include context cancellation, Esc key, SIGINT, etc.
func isUserAbortError(errMsg string) bool {
	if errMsg == "" {
		return false
	}
	lowerMsg := strings.ToLower(errMsg)
	// Check for context cancellation patterns
	if strings.Contains(lowerMsg, "context canceled") ||
		strings.Contains(lowerMsg, "context cancelled") ||
		strings.Contains(lowerMsg, "canceled") ||
		strings.Contains(lowerMsg, "cancelled") {
		return true
	}
	// Check for user interrupt patterns (Esc, SIGINT, etc.)
	if strings.Contains(lowerMsg, "user interrupt") ||
		strings.Contains(lowerMsg, "interrupt") ||
		strings.Contains(lowerMsg, "sigint") ||
		strings.Contains(lowerMsg, "keyboard interrupt") {
		return true
	}
	return false
}
