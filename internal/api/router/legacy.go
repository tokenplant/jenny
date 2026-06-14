// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"os"
	"sort"
)

// SynthesizeConfigFromEnv creates a Config from legacy environment variables,
// sorted by priority (OpenAI > GenAI > Anthropic).
func SynthesizeConfigFromEnv() *Config {
	cfg := &Config{
		Providers: []Provider{},
		Profiles:  make(map[string]Profile),
	}

	// Check OpenAI
	if openAIKey := os.Getenv("OPENAI_API_KEY"); openAIKey != "" {
		model := os.Getenv("OPENAI_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		cfg.Providers = append(cfg.Providers, Provider{
			Name:    "legacy-openai",
			Type:    "openai",
			BaseURL: baseURL,
			Accounts: []Account{{
				Name:     "default",
				Keys:     []string{openAIKey},
				Priority: 1,
			}},
			Models: []Model{{
				Name:          model,
				Tags:          []string{},
				Priority:      1,
				ContextWindow: 128000,
				MaxOutput:     16384,
			}},
		})
	}

	// Check GenAI (Gemini/Vertex AI)
	if isGenAIEnvSet() {
		model := os.Getenv("GENAI_MODEL")
		if model == "" {
			model = os.Getenv("GEMINI_MODEL")
		}
		if model == "" {
			model = "gemini-2.0-flash"
		}
		baseURL := os.Getenv("GENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}

		var apiKeys []string
		if key := os.Getenv("GENAI_API_KEY"); key != "" {
			apiKeys = append(apiKeys, key)
		}
		if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
			apiKeys = append(apiKeys, key)
		}
		if key := os.Getenv("GEMINI_API_KEY"); key != "" {
			apiKeys = append(apiKeys, key)
		}

		if len(apiKeys) > 0 {
			cfg.Providers = append(cfg.Providers, Provider{
				Name:    "legacy-genai",
				Type:    "gemini",
				BaseURL: baseURL,
				Accounts: []Account{{
					Name:     "default",
					Keys:     apiKeys,
					Priority: 2,
				}},
				Models: []Model{{
					Name:          model,
					Tags:          []string{},
					Priority:      2,
					ContextWindow: 1000000,
					MaxOutput:     8192,
				}},
			})
		}
	}

	// Check Anthropic
	if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
		model := os.Getenv("ANTHROPIC_MODEL")
		if model == "" {
			model = "claude-opus-4-5-20251101"
		}
		baseURL := os.Getenv("ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}

		cfg.Providers = append(cfg.Providers, Provider{
			Name:    "legacy-anthropic",
			Type:    "anthropic",
			BaseURL: baseURL,
			Accounts: []Account{{
				Name:     "default",
				Keys:     []string{anthropicKey},
				Priority: 3,
			}},
			Models: []Model{{
				Name:          model,
				Tags:          []string{},
				Priority:      3,
				ContextWindow: 200000,
				MaxOutput:     20000,
			}},
		})
	}

	// Sort providers by account priority
	sort.Slice(cfg.Providers, func(i, j int) bool {
		return cfg.Providers[i].Accounts[0].Priority < cfg.Providers[j].Accounts[0].Priority
	})

	// Create default profile
	defaultAllow := true
	cfg.Profiles["default"] = Profile{
		RoutingMode:     "sticky",
		SelectionPolicy: "round_robin",
		RetryPolicy: RetryPolicy{
			MaxRetries: 3,
			Backoff:    "exponential",
		},
		AllowFallback: &defaultAllow,
	}

	return cfg
}

// isGenAIEnvSet reports whether any GenAI-related environment variables are set.
func isGenAIEnvSet() bool {
	if os.Getenv("GENAI_API_KEY") != "" {
		return true
	}
	if os.Getenv("GENAI_BASE_URL") != "" {
		return true
	}
	if os.Getenv("GOOGLE_API_KEY") != "" || os.Getenv("GEMINI_API_KEY") != "" {
		return true
	}
	if os.Getenv("GOOGLE_CLOUD_PROJECT") != "" &&
		(os.Getenv("GOOGLE_CLOUD_LOCATION") != "" || os.Getenv("GOOGLE_CLOUD_REGION") != "") {
		return true
	}
	if os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "1" || os.Getenv("GOOGLE_GENAI_USE_VERTEXAI") == "true" {
		return true
	}
	return false
}
