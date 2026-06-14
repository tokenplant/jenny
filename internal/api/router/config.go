// Package router provides multi-provider routing with three-layer fallback logic.
package router

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level routing configuration.
type Config struct {
	Providers []Provider `yaml:"providers"`
	Profiles  map[string]Profile `yaml:"profiles"`
}

// Provider defines a backend provider with credentials and models.
type Provider struct {
	Name     string    `yaml:"name"`
	Type     string    `yaml:"type"` // openai, anthropic, gemini
	BaseURL  string    `yaml:"base_url"`
	Accounts []Account `yaml:"accounts"`
	Models   []Model   `yaml:"models"`
}

// Account represents a set of API keys for a provider.
type Account struct {
	Name     string   `yaml:"name"`
	Keys     []string `yaml:"keys"`
	Priority int      `yaml:"priority"`
}

// Model represents a model configuration within a provider.
type Model struct {
	Name          string   `yaml:"name"`
	Tags          []string `yaml:"tags"`
	Priority      int      `yaml:"priority"`
	ContextWindow int      `yaml:"context_window"`
	MaxOutput     int      `yaml:"max_output"`
}

// Profile defines an execution policy with target chains and routing behavior.
type Profile struct {
	Targets         []Target    `yaml:"targets"`
	RoutingMode     string      `yaml:"routing_mode"`     // sticky, balanced
	SelectionPolicy string     `yaml:"selection_policy"` // round_robin, random
	RetryPolicy     RetryPolicy `yaml:"retry_policy"`
	AllowFallback   *bool       `yaml:"allow_fallback"`
}

// Target represents a matching rule within a profile's target chain.
type Target struct {
	Match MatchClause `yaml:"match"`
}

// MatchClause defines what models or tags a target matches.
type MatchClause struct {
	Models []string `yaml:"models"`
	Tags   []string `yaml:"tags"`
}

// RetryPolicy defines retry behavior for a profile.
type RetryPolicy struct {
	MaxRetries int    `yaml:"max_retries"`
	Backoff    string `yaml:"backoff"` // exponential, linear
}

// LoadConfig loads a YAML configuration file and returns the parsed Config.
// Returns nil, nil if the file does not exist.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	return &cfg, nil
}

// applyDefaults sets default values for omitted configuration fields.
func applyDefaults(cfg *Config) {
	for i := range cfg.Providers {
		for j := range cfg.Providers[i].Accounts {
			if cfg.Providers[i].Accounts[j].Priority == 0 {
				cfg.Providers[i].Accounts[j].Priority = 1
			}
		}
		for j := range cfg.Providers[i].Models {
			if cfg.Providers[i].Models[j].Priority == 0 {
				cfg.Providers[i].Models[j].Priority = 1
			}
		}
	}

	for name, profile := range cfg.Profiles {
		if profile.RoutingMode == "" {
			profile.RoutingMode = "sticky"
		}
		if profile.SelectionPolicy == "" {
			profile.SelectionPolicy = "round_robin"
		}
		if profile.RetryPolicy.MaxRetries == 0 {
			profile.RetryPolicy.MaxRetries = 3
		}
		if profile.RetryPolicy.Backoff == "" {
			profile.RetryPolicy.Backoff = "exponential"
		}
		if profile.AllowFallback == nil {
			defaultAllow := true
			profile.AllowFallback = &defaultAllow
		}
		cfg.Profiles[name] = profile
	}
}
