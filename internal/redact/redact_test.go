package redact

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestNewSecretRedactor_EnabledByDefault(t *testing.T) {
	// Clear any existing env var
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	if !redactor.Enabled() {
		t.Error("Expected redaction to be enabled by default")
	}
}

func TestNewSecretRedactor_DisabledByEnv(t *testing.T) {
	// Set the disable env var
	os.Setenv("JENNY_REDACT_DISABLE", "1")
	defer os.Unsetenv("JENNY_REDACT_DISABLE")

	redactor := NewSecretRedactor()
	if redactor.Enabled() {
		t.Error("Expected redaction to be disabled when JENNY_REDACT_DISABLE=1")
	}
}

func TestRedact_ReplacesOpenAIKey(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Use valid OpenAI pattern: sk-{20}T3BlbkFJ{20}
	input := "API key is sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	result := redactor.Redact(input)

	// Should have placeholder
	if !strings.Contains(result, "[REDACTED:") {
		t.Error("Expected redaction placeholder in result")
	}
	// Original should not be present (redacted portion)
	if strings.Contains(result, "sk-abcdefghijklmnopqrstT3BlbkFJ") {
		t.Error("Original secret should not be in result")
	}
}

func TestRedact_ReplacesGitHubToken(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Use valid GitHub PAT pattern: ghp_ + exactly 36 alphanumeric chars
	input := "GitHub token: ghp_abcdefghijklmnopqrstuvwxyz123456789012ab"
	result := redactor.Redact(input)

	// Should have placeholder
	if !strings.Contains(result, "[REDACTED:") {
		t.Error("Expected redaction placeholder in result")
	}
	// Original should not be present
	if strings.Contains(result, "ghp_abcdefghijklmnopqrstuvwxyz") {
		t.Error("Original secret should not be in result")
	}
}

func TestRedact_PreservesLongBase64(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Legitimate base64 content (like image data) should NOT be redacted
	// This is a 48+ char base64 string without any secret prefix
	legitBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	input := "Image data: " + legitBase64
	result := redactor.Redact(input)

	if result != input {
		t.Errorf("Legitimate base64 content should not be redacted, got: %s", result)
	}
}

func TestRedact_ReplacesAWSKey(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	input := "AWS key: AKIAIOSFODNN7EXAMPLE"
	result := redactor.Redact(input)

	// Should have placeholder
	if !strings.Contains(result, "[REDACTED:") {
		t.Error("Expected redaction placeholder in result")
	}
	// Original should not be present
	if strings.Contains(result, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("Original secret should not be in result")
	}
}

func TestRedact_PreservesNonSecrets(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	input := "This is just regular text with no secrets"
	result := redactor.Redact(input)

	if result != input {
		t.Errorf("Expected non-secret text to be unchanged, got: %s", result)
	}
}

func TestRedact_SameSecretSameID(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Use valid OpenAI pattern
	secret := "sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	input1 := "Key1: " + secret
	input2 := "Key2: " + secret

	result1 := redactor.Redact(input1)
	result2 := redactor.Redact(input2)

	// Extract placeholder IDs
	idx1 := strings.Index(result1, "[REDACTED:")
	id1 := ""
	if idx1 >= 0 {
		end1 := strings.Index(result1[idx1:], "]")
		if end1 > 0 {
			id1 = result1[idx1 : idx1+end1+1]
		}
	}

	idx2 := strings.Index(result2, "[REDACTED:")
	id2 := ""
	if idx2 >= 0 {
		end2 := strings.Index(result2[idx2:], "]")
		if end2 > 0 {
			id2 = result2[idx2 : idx2+end2+1]
		}
	}

	if id1 != id2 {
		t.Errorf("Same secret should produce same ID, got %s vs %s", id1, id2)
	}
}

func TestRedact_DifferentSecretsDifferentIDs(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Use two different valid OpenAI patterns
	secret1 := "sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	secret2 := "sk-12345678901234567890T3BlbkFJ12345678901234567890"

	result1 := redactor.Redact("Key: " + secret1)
	result2 := redactor.Redact("Key: " + secret2)

	// Extract placeholder IDs
	idx1 := strings.Index(result1, "[REDACTED:")
	id1 := ""
	if idx1 >= 0 {
		end1 := strings.Index(result1[idx1:], "]")
		if end1 > 0 {
			id1 = result1[idx1 : idx1+end1+1]
		}
	}

	idx2 := strings.Index(result2, "[REDACTED:")
	id2 := ""
	if idx2 >= 0 {
		end2 := strings.Index(result2[idx2:], "]")
		if end2 > 0 {
			id2 = result2[idx2 : idx2+end2+1]
		}
	}

	if id1 == id2 {
		t.Errorf("Different secrets should produce different IDs, got same: %s", id1)
	}
}

func TestRedact_NoOpWhenDisabled(t *testing.T) {
	// Set the disable env var
	os.Setenv("JENNY_REDACT_DISABLE", "1")
	defer os.Unsetenv("JENNY_REDACT_DISABLE")

	redactor := NewSecretRedactor()
	// Use valid OpenAI pattern
	input := "Key: sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	result := redactor.Redact(input)

	if result != input {
		t.Errorf("When disabled, input should be unchanged, got: %s", result)
	}
}

func TestRecover_RestoresOriginal(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Use valid OpenAI pattern
	secret := "sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	redacted := redactor.Redact("Key: " + secret)

	// Now recover
	recovered := redactor.Recover(redacted)

	// Original should be restored
	if !strings.Contains(recovered, secret) {
		t.Errorf("Expected original secret to be restored, got: %s", recovered)
	}
}

func TestRecover_UnknownPlaceholder(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	input := "Unknown placeholder: [REDACTED:ID_99999]"
	result := redactor.Recover(input)

	if result != input {
		t.Errorf("Unknown placeholder should be unchanged, got: %s", result)
	}
}

func TestReset_ClearsMappings(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Use valid OpenAI pattern
	secret := "sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst"
	redacted := redactor.Redact("Key: " + secret)

	// Reset
	redactor.Reset()

	// After reset, recover should not find the mapping
	recovered := redactor.Recover(redacted)

	// The placeholder should still be there (not replaced) since the mapping was cleared
	if !strings.Contains(recovered, "[REDACTED:") {
		t.Errorf("After reset, placeholder should be unchanged, got: %s", recovered)
	}
}

func TestRedact_ConcurrentSafety(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Use valid OpenAI patterns with different suffixes
			secret := "sk-abcdefghijklmnopqrstT3BlbkFJabcdefghijklmnopqrst" + string(rune('a'+n))
			for range 100 {
				redacted := redactor.Redact("Key: " + secret)
				_ = redactor.Recover(redacted)
			}
		}(i)
	}

	wg.Wait()
	// If we get here without panic or data race, test passes
}

func TestRedact_ReplacesLongToken(t *testing.T) {
	origVal := os.Getenv("JENNY_REDACT_DISABLE")
	os.Unsetenv("JENNY_REDACT_DISABLE")
	defer func() {
		if origVal != "" {
			os.Setenv("JENNY_REDACT_DISABLE", origVal)
		}
	}()

	redactor := NewSecretRedactor()
	// Token from .env.freemodel: 54 chars, "fe_oa_" prefix, high entropy
	token := "fe_oa_7066b4cf68b1daf66206986fb5d16d45c466d74d39f0d52e"
	input := "ANTHROPIC_AUTH_TOKEN=" + token
	result := redactor.Redact(input)

	if !strings.Contains(result, "[REDACTED:") {
		t.Error("Expected redaction placeholder for long token")
	}
	if strings.Contains(result, token) {
		t.Error("Original token should not be in result")
	}
}

func TestRecover_NoOpWhenDisabled(t *testing.T) {
	// Set the disable env var
	os.Setenv("JENNY_REDACT_DISABLE", "1")
	defer os.Unsetenv("JENNY_REDACT_DISABLE")

	redactor := NewSecretRedactor()
	input := "Placeholder: [REDACTED:ID_00001]"
	result := redactor.Recover(input)

	if result != input {
		t.Errorf("When disabled, input should be unchanged, got: %s", result)
	}
}

func TestReset_NoOpWhenDisabled(t *testing.T) {
	// Set the disable env var
	os.Setenv("JENNY_REDACT_DISABLE", "1")
	defer os.Unsetenv("JENNY_REDACT_DISABLE")

	redactor := NewSecretRedactor()
	// Should not panic
	redactor.Reset()
}
