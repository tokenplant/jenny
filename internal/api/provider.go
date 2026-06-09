package api

import "strings"

// knownMiniMaxHost is the substring used to detect MiniMax provider.
const knownMiniMaxHost = "minimaxi"

// providerFromBaseURL returns the provider name based on the base URL.
// It inspects ANTHROPIC_BASE_URL for known alternate provider hosts.
// Returns "minimax" if the URL contains knownMiniMaxHost, otherwise "anthropic".
// Empty string URL returns "anthropic".
func providerFromBaseURL(baseURL string) string {
	if baseURL == "" {
		return "anthropic"
	}
	if strings.Contains(baseURL, knownMiniMaxHost) {
		return "minimax"
	}
	return "anthropic"
}
