package tool

import "unicode/utf8"

// utf8SafeTruncate returns s truncated to at most maxBytes bytes,
// ensuring the result is valid UTF-8 with no split code points.
// If maxBytes < 0, returns "".
func utf8SafeTruncate(s string, maxBytes int) string {
	if maxBytes < 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// Try truncating at maxBytes; if that's not a valid UTF-8 boundary,
	// back up to the nearest rune start.
	result := s[:maxBytes]
	if utf8.ValidString(result) {
		return result
	}
	// Binary-search for the largest valid UTF-8 prefix within maxBytes.
	// In practice maxBytes is small (e.g. 200), so a simple loop is fine.
	for len(result) > 0 {
		if utf8.ValidString(result) {
			return result
		}
		result = result[:len(result)-1]
	}
	return ""
}
