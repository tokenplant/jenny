package api

import (
	"os"
	"testing"
)

func TestNewClientWithModelEnvVar(t *testing.T) {
	// Save original env var
	origModel := os.Getenv("ANTHROPIC_MODEL")
	defer func() {
		if origModel != "" {
			os.Setenv("ANTHROPIC_MODEL", origModel)
		} else {
			os.Unsetenv("ANTHROPIC_MODEL")
		}
	}()

	// Set ANTHROPIC_MODEL env var
	os.Setenv("ANTHROPIC_MODEL", "test-env-model")

	// Create client with empty model - should use env var
	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The client should have picked up the env var
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.GetModel() != "test-env-model" {
		t.Errorf("expected model 'test-env-model', got %q", client.GetModel())
	}
}

func TestNewClientWithModelEmpty(t *testing.T) {
	// Save original env var
	origModel := os.Getenv("ANTHROPIC_MODEL")
	defer func() {
		if origModel != "" {
			os.Setenv("ANTHROPIC_MODEL", origModel)
		} else {
			os.Unsetenv("ANTHROPIC_MODEL")
		}
	}()

	// Ensure ANTHROPIC_MODEL is not set
	os.Unsetenv("ANTHROPIC_MODEL")

	client, err := NewClientWithModel("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// Should use default model when env var is not set
	if client.GetModel() != defaultModel {
		t.Errorf("expected model %q, got %q", defaultModel, client.GetModel())
	}
}

func TestNewClientWithModelOverride(t *testing.T) {
	// Save original env var
	origModel := os.Getenv("ANTHROPIC_MODEL")
	defer func() {
		if origModel != "" {
			os.Setenv("ANTHROPIC_MODEL", origModel)
		} else {
			os.Unsetenv("ANTHROPIC_MODEL")
		}
	}()

	// Set ANTHROPIC_MODEL env var
	os.Setenv("ANTHROPIC_MODEL", "env-model")

	// Create client with model override - should use override
	client, err := NewClientWithModel("override-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	// Override should take precedence over env var
	if client.GetModel() != "override-model" {
		t.Errorf("expected model 'override-model', got %q", client.GetModel())
	}
}

func TestDefaultModelConstant(t *testing.T) {
	// Verify defaultModel constant is defined and non-empty
	if defaultModel == "" {
		t.Error("defaultModel should not be empty")
	}
	// Verify it matches the expected model string
	if defaultModel != "deepseek-v4-flash" {
		t.Errorf("expected defaultModel 'deepseek-v4-flash', got %q", defaultModel)
	}
}
