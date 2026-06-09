// Package api provides the Anthropic API client.
package api

import (
	"encoding/base64"
	"strings"
)

// ValidateMessagesMedia validates media in messages before sending to the API.
// It checks for data URIs and raw base64 image headers, enforcing:
// - Maximum100 media items per request
// - Maximum 5 MB per base64-encoded image
// Returns a CannotRetryError if validation fails.
func ValidateMessagesMedia(messages []Message) error {
	totalMedia := 0
	for _, msg := range messages {
		for _, tr := range msg.ToolResults {
			count, maxSize, err := countMediaInContent(tr.Content)
			if err != nil {
				return &CannotRetryError{
					Message:    err.Error(),
					StatusCode: 400,
				}
			}
			totalMedia += count
			if maxSize > MaxBase64ImageSize {
				return &CannotRetryError{
					Message:    "image exceeds maximum allowed size of 5 MB",
					StatusCode: 400,
				}
			}
		}
	}
	if totalMedia > MaxMediaItemsPerRequest {
		return &CannotRetryError{
			Message:    "request contains too many media items (max 100)",
			StatusCode: 400,
		}
	}
	return nil
}

// countMediaInContent counts media items and finds the largest decoded size in content.
// Returns count, largest decoded size found, and any error.
func countMediaInContent(content string) (count int, largestSize int, err error) {
	if content == "" {
		return 0, 0, nil
	}

	const dataURIPrefix = "data:image/"
	const base64Marker = ";base64,"

	// Find all data URIs and count them, extracting size when possible
	// A data URI is identified by "data:image/<fmt>;base64,<payload>"
	for {
		idx := strings.Index(content, dataURIPrefix)
		if idx == -1 {
			break
		}

		count++

		rest := content[idx+len(dataURIPrefix):]
		// Find where the MIME type ends (semicolon before "base64,")
		semiIdx := strings.Index(rest, ";")
		if semiIdx == -1 {
			// Malformed; skip past this prefix
			content = content[idx+len(dataURIPrefix):]
			continue
		}

		base64Idx := strings.Index(rest[semiIdx:], base64Marker)
		if base64Idx == -1 {
			// Malformed; skip past this prefix
			content = content[idx+len(dataURIPrefix):]
			continue
		}

		// Start of base64 payload in rest
		payloadStartInRest := semiIdx + base64Idx + len(base64Marker)
		payload := rest[payloadStartInRest:]

		// Find end of base64 - look for either:
		// 1. A non-base64 character, OR
		// 2. The start of another "data:image/" (which would be inside the base64 as text)
		base64EndInPayload := 0
		for i := 0; i < len(payload); i++ {
			c := rune(payload[i])
			// Skip whitespace; allow newlines in MIME-formatted base64
			if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
				continue
			}
			if !isBase64Char(c) {
				base64EndInPayload = i
				break
			}
			// Check if this could be the start of "data:image/" inside the base64
			if i+11 <= len(payload) && strings.HasPrefix(payload[i:i+11], "data:image/") {
				base64EndInPayload = i
				break
			}
		}
		if base64EndInPayload == 0 {
			base64EndInPayload = len(payload)
		}

		// Calculate absolute positions in original string
		payloadStart := idx + len(dataURIPrefix) + payloadStartInRest
		payloadEnd := payloadStart + base64EndInPayload

		if base64EndInPayload > 0 {
			cleaned := cleanBase64Fragment(payload[:base64EndInPayload])
			decoded := make([]byte, base64.StdEncoding.DecodedLen(len(cleaned)))
			_, decodeErr := base64.StdEncoding.Decode(decoded, []byte(cleaned))
			if decodeErr == nil && len(decoded) > largestSize {
				largestSize = len(decoded)
			}
		}

		// Move past this data URI for next search
		content = content[payloadEnd:]
	}

	// Scan for raw image headers in remaining content (not inside data URIs)
	rawHeaders := []string{"/9j/", "iVBOR", "R0lGOD", "UklGR"}
	for _, header := range rawHeaders {
		idx := 0
		for {
			pos := strings.Index(content[idx:], header)
			if pos == -1 {
				break
			}
			absPos := idx + pos

			after := content[absPos+len(header):]

			// Extract base64 after header - also stop at "data:image/" inside base64
			base64End := 0
			for i := 0; i < len(after); i++ {
				c := rune(after[i])
				// Skip whitespace; allow newlines in MIME-formatted base64
				if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
					continue
				}
				if !isBase64Char(c) {
					base64End = i
					break
				}
				// Check for embedded data URI start
				if i+11 <= len(after) && strings.HasPrefix(after[i:i+11], "data:image/") {
					base64End = i
					break
				}
			}
			if base64End == 0 {
				base64End = len(after)
			}

			if base64End >= 20 {
				cleaned := cleanBase64Fragment(after[:base64End])
				decoded := make([]byte, base64.StdEncoding.DecodedLen(len(cleaned)))
				_, decodeErr := base64.StdEncoding.Decode(decoded, []byte(cleaned))
				if decodeErr == nil {
					count++
					if len(decoded) > largestSize {
						largestSize = len(decoded)
					}
				} else if len(cleaned) > 20 {
					// Decode failed but we have a substantial base64 fragment.
					// Estimate size: each base64 char encodes 6 bits; 4 chars encode 3 bytes.
					estimatedSize := (len(cleaned) * 3) / 4
					if estimatedSize > largestSize {
						largestSize = estimatedSize
					}
				}
			}

			idx = absPos + len(header)
		}
	}

	return count, largestSize, nil
}

// cleanBase64Fragment builds a whitespace-stripped base64 string for decoding.
// It iterates through s and collects only base64 chars (not \n, \r, \t, space).
func cleanBase64Fragment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if c == '\n' || c == '\r' || c == '\t' || c == ' ' {
			continue
		}
		if isBase64Char(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func isBase64Char(c rune) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '='
}

