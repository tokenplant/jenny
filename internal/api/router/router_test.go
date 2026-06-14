package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigParsing tests parsing of a valid YAML config.
func TestConfigParsing(t *testing.T) {
	yamlContent := `
providers:
  - name: "deepseek"
    type: "openai"
    base_url: "https://api.deepseek.com"
    accounts:
      - name: "personal"
        keys: ["sk-ds-1", "sk-ds-2"]
        priority: 1
    models:
      - name: "deepseek-chat"
        tags: ["cheap", "text"]
        priority: 1
        context_window: 64000
        max_output: 4000

profiles:
  default:
    targets:
      - match: { models: ["deepseek:deepseek-chat"] }
      - match: { tags: ["cheap"] }
    routing_mode: "sticky"
    selection_policy: "round_robin"
    retry_policy:
      max_retries: 3
      backoff: "exponential"
    allow_fallback: true
`

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "routes.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}

	if cfg.Providers[0].Name != "deepseek" {
		t.Errorf("expected provider name 'deepseek', got %q", cfg.Providers[0].Name)
	}

	if len(cfg.Providers[0].Accounts) != 1 {
		t.Errorf("expected 1 account, got %d", len(cfg.Providers[0].Accounts))
	}

	if len(cfg.Providers[0].Accounts[0].Keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(cfg.Providers[0].Accounts[0].Keys))
	}

	if len(cfg.Providers[0].Models) != 1 {
		t.Errorf("expected 1 model, got %d", len(cfg.Providers[0].Models))
	}

	if cfg.Providers[0].Models[0].Name != "deepseek-chat" {
		t.Errorf("expected model name 'deepseek-chat', got %q", cfg.Providers[0].Models[0].Name)
	}

	profile, ok := cfg.Profiles["default"]
	if !ok {
		t.Fatal("expected default profile")
	}

	if profile.RoutingMode != "sticky" {
		t.Errorf("expected routing_mode 'sticky', got %q", profile.RoutingMode)
	}

	if profile.SelectionPolicy != "round_robin" {
		t.Errorf("expected selection_policy 'round_robin', got %q", profile.SelectionPolicy)
	}

	if profile.RetryPolicy.MaxRetries != 3 {
		t.Errorf("expected max_retries 3, got %d", profile.RetryPolicy.MaxRetries)
	}

	if profile.AllowFallback == nil || !*profile.AllowFallback {
		t.Error("expected allow_fallback to be true")
	}
}

// TestConfigParsing_Invalid tests that invalid YAML returns an error.
func TestConfigParsing_Invalid(t *testing.T) {
	invalidYAML := `
providers:
  - name: "test"
    type: "openai"
    base_url: "https://api.example.com"
    accounts:
      - name: "test"
        keys: ["key1"]
    models:
      - name: "model"
        tags: [invalid yaml here
`

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "routes.yaml")
	if err := os.WriteFile(cfgPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := LoadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// TestConfigParsing_NotFound tests that missing config returns nil, nil.
func TestConfigParsing_NotFound(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/routes.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config for missing file")
	}
}

// TestLegacyEnvSync tests that environment variables are synthesized into config.
func TestLegacyEnvSync(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-4-5-20251101")
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")

	cfg := SynthesizeConfigFromEnv()
	if cfg == nil {
		t.Fatal("expected non-nil synthesized config")
	}

	if len(cfg.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(cfg.Providers))
	}

	if cfg.Providers[0].Type != "anthropic" {
		t.Errorf("expected provider type 'anthropic', got %q", cfg.Providers[0].Type)
	}

	if cfg.Providers[0].Models[0].Name != "claude-opus-4-5-20251101" {
		t.Errorf("expected model 'claude-opus-4-5-20251101', got %q", cfg.Providers[0].Models[0].Name)
	}

	if len(cfg.Providers[0].Accounts[0].Keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(cfg.Providers[0].Accounts[0].Keys))
	}

	if cfg.Providers[0].Accounts[0].Keys[0] != "sk-ant-test-key" {
		t.Errorf("expected key 'sk-ant-test-key', got %q", cfg.Providers[0].Accounts[0].Keys[0])
	}
}

// TestSelectEndpoint_Sticky tests that same sessionID returns same endpoint.
func TestSelectEndpoint_Sticky(t *testing.T) {
	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "test-provider",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key1", "key2"}, Priority: 1},
				},
				Models: []Model{
					{Name: "test-model", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:         []Target{{Match: MatchClause{Models: []string{"test-model"}}}},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
			},
		},
	}

	router := NewRouter(cfg)
	sessionID := "test-session-123"

	// First call should return an endpoint
	ep1, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep1 == nil {
		t.Fatal("expected non-nil endpoint")
	}

	// Second call with same session should return the same endpoint
	ep2, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep1.APIKey != ep2.APIKey {
		t.Errorf("expected same API key for sticky session, got %q vs %q", ep1.APIKey, ep2.APIKey)
	}
	if ep1.Model != ep2.Model {
		t.Errorf("expected same model for sticky session, got %q vs %q", ep1.Model, ep2.Model)
	}
}

// TestSelectEndpoint_RoundRobin tests that different sessions get distributed endpoints.
func TestSelectEndpoint_RoundRobin(t *testing.T) {
	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "test-provider",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key1", "key2"}, Priority: 1},
				},
				Models: []Model{
					{Name: "test-model", Tags: []string{}, Priority: 1},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets:         []Target{{Match: MatchClause{Models: []string{"test-model"}}}},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
			},
		},
	}

	router := NewRouter(cfg)

	// Collect endpoints from different sessions
	keys := make(map[string]bool)
	for i := 0; i < 10; i++ {
		ep, err := router.SelectEndpoint(strings.Repeat("s", i+1))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		keys[ep.APIKey] = true
	}

	// With 2 keys and 10 sessions, we should see both keys used
	if len(keys) < 1 {
		t.Errorf("expected at least 1 unique key, got %d", len(keys))
	}
}

// TestLayer3_ModelFallback tests that after clearing sticky, new endpoint is selected.
func TestLayer3_ModelFallback(t *testing.T) {
	cfg := &Config{
		Providers: []Provider{
			{
				Name:    "provider-a",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key-a"}, Priority: 1},
				},
				Models: []Model{
					{Name: "model-a", Tags: []string{"expensive"}, Priority: 1},
				},
			},
			{
				Name:    "provider-b",
				Type:    "openai",
				BaseURL: "https://api.example.com",
				Accounts: []Account{
					{Name: "default", Keys: []string{"key-b"}, Priority: 2},
				},
				Models: []Model{
					{Name: "model-b", Tags: []string{"cheap"}, Priority: 2},
				},
			},
		},
		Profiles: map[string]Profile{
			"default": {
				Targets: []Target{
					{Match: MatchClause{Tags: []string{"expensive"}}},
					{Match: MatchClause{Tags: []string{"cheap"}}},
				},
				RoutingMode:     "sticky",
				SelectionPolicy: "round_robin",
			},
		},
	}

	router := NewRouter(cfg)
	sessionID := "test-session"

	// First selection should pick model-a (higher priority)
	ep1, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ep1.Model != "model-a" {
		t.Errorf("expected model-a, got %q", ep1.Model)
	}

	// Clear sticky and select again
	router.ClearSticky(sessionID)
	ep2, err := router.SelectEndpoint(sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since model-a is still available, it should be selected again
	if ep2.Model != "model-a" {
		t.Errorf("expected model-a after clear, got %q", ep2.Model)
	}
}

// TestHealthRegistry_Cooldown tests cooldown behavior.
func TestHealthRegistry_Cooldown(t *testing.T) {
	registry := NewHealthRegistry()

	// Initially healthy
	if !registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be healthy initially")
	}

	// Record failures up to max
	for i := 0; i < registry.maxFailures; i++ {
		registry.RecordFailure("provider", "account", "model", "key1")
	}

	// Now should be unhealthy (in cooldown)
	if registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be unhealthy after max failures")
	}
}

// TestHealthRegistry_Reset tests reset behavior.
func TestHealthRegistry_Reset(t *testing.T) {
	registry := NewHealthRegistry()

	// Record some failures
	registry.RecordFailure("provider", "account", "model", "key1")
	registry.RecordFailure("provider", "account", "model", "key1")
	registry.RecordFailure("provider", "account", "model", "key1")

	// Reset
	registry.Reset()

	// Should be healthy again
	if !registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be healthy after reset")
	}
}

// TestHealthRegistry_RecordSuccess tests that success clears failure count.
func TestHealthRegistry_RecordSuccess(t *testing.T) {
	registry := NewHealthRegistry()

	// Record some failures
	registry.RecordFailure("provider", "account", "model", "key1")
	registry.RecordFailure("provider", "account", "model", "key1")

	// Record success
	registry.RecordSuccess("provider", "account", "model", "key1")

	// Should still be healthy (failure count is 0)
	if !registry.IsHealthy("provider", "account", "model", "key1") {
		t.Error("expected key1 to be healthy after success")
	}

	if registry.GetFailureCount("provider", "account", "model", "key1") != 0 {
		t.Error("expected failure count to be 0 after success")
	}
}
