package redact

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
)

// secretPattern represents a regex pattern for detecting secrets.
type secretPattern struct {
	pattern *regexp.Regexp
}

// Built-in regex patterns for common secret types not covered by gitleaks default.
var additionalPatterns = []secretPattern{
	// OpenAI API key - sk- prefix followed by base64-like characters
	{pattern: regexp.MustCompile(`\b(sk-[A-Za-z0-9]{20,})\b`)},
	// GitHub Personal Access Token (classic) - ghp_ prefix followed by 36-40 alphanumeric chars
	{pattern: regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{36,40})\b`)},
	// GitHub OAuth - gho_ prefix followed by 36-40 alphanumeric chars
	{pattern: regexp.MustCompile(`\b(gho_[A-Za-z0-9]{36,40})\b`)},
	// GitHub fine-grained PAT - github_pat_ prefix
	{pattern: regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{50,})\b`)},
	// GitHub refresh token - ghr_ prefix followed by 36-40 chars
	{pattern: regexp.MustCompile(`\b(ghr_[A-Za-z0-9]{36,40})\b`)},
	// AWS Access Key ID - AKIA prefix followed by 16 alphanumeric chars
	{pattern: regexp.MustCompile(`\b(AKIA[A-Za-z0-9]{16})\b`)},
	// AWS Secret Access Key - typically 40 chars of mixed alphanumeric/special
	{pattern: regexp.MustCompile(`\b(AWS|[Aa]ws|aws)[^"\']{0,20}([A-Za-z0-9/+=]{40})\b`)},
	// Slack token - xox[baprs]- prefix
	{pattern: regexp.MustCompile(`\b(xox[baprs]-[A-Za-z0-9-]{10,48})\b`)},
	// NPM token - npm_ prefix followed by various lengths
	{pattern: regexp.MustCompile(`\b(npm_[A-Za-z0-9]{30,})\b`)},
	// PyPI token - pypi_ prefix
	{pattern: regexp.MustCompile(`\b(pypi_[A-Za-z0-9_]{50,})\b`)},
	// High-entropy string detection - requires specific secret prefixes
	// Only matches sequences with known credential prefixes to avoid false positives
	// Matches: password=, secret=, token=, key=, api_key=, apikey=, auth=, bearer , sk-, ghp_, AKIA
	{pattern: regexp.MustCompile(`(?i)\b((?:password|secret|token|key|api[_-]?key|auth|bearer)[=:]\s*[A-Za-z0-9/+=]{20,})\b`)},
}

// SecretRedactor detects and redacts secrets in tool results.
type SecretRedactor struct {
	mu            sync.Mutex
	enabled       bool
	counter int
	replacements  map[string]string // placeholder -> original
	secretToID    map[string]string // secret -> placeholder ID (for deduplication)
}

// NewSecretRedactor creates a new SecretRedactor.
// Enabled by default unless JENNY_REDACT_DISABLE=1 is set.
func NewSecretRedactor() *SecretRedactor {
	enabled := os.Getenv("JENNY_REDACT_DISABLE") == ""
	return &SecretRedactor{
		enabled:     enabled,
		replacements: make(map[string]string),
		secretToID:   make(map[string]string),
	}
}

// Enabled returns whether redaction is active.
func (r *SecretRedactor) Enabled() bool {
	return r.enabled
}

// Redact scans content for secrets and replaces them with placeholders.
// Returns the content with all detected secrets replaced.
func (r *SecretRedactor) Redact(content string) string {
	if !r.enabled {
		return content
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	result := content

	// Apply additional regex patterns for secrets not covered by gitleaks default
	for _, sp := range additionalPatterns {
		result = r.replaceWithPattern(result, sp.pattern)
	}

	return result
}

// replaceWithPattern finds all matches of the pattern and replaces them with placeholders.
func (r *SecretRedactor) replaceWithPattern(content string, pattern *regexp.Regexp) string {
	matches := pattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content
	}

	result := content
	// Process matches in reverse order to preserve positions
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		if len(match) < 4 {
			continue
		}
		// Extract the full match (not just a subgroup)
		secret := content[match[0]:match[1]]

		// Generate placeholder
		var placeholder string
		if existingID, ok := r.secretToID[secret]; ok {
			placeholder = existingID
		} else {
			r.counter++
			placeholder = fmt.Sprintf("[REDACTED:ID_%05d]", r.counter)
			r.secretToID[secret] = placeholder
			r.replacements[placeholder] = secret
		}

		// Replace in result
		result = result[:match[0]] + placeholder + result[match[1]:]
	}

	return result
}

// Recover replaces placeholders with their original values.
// Unknown placeholders are left unchanged.
func (r *SecretRedactor) Recover(content string) string {
	if !r.enabled {
		return content
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	result := content
	for placeholder, original := range r.replacements {
		result = strings.ReplaceAll(result, placeholder, original)
	}
	return result
}

// Reset clears all stored mappings and resets the counter.
func (r *SecretRedactor) Reset() {
	if !r.enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replacements = make(map[string]string)
	r.secretToID = make(map[string]string)
	r.counter = 0
}