// Package tool provides the tool interface and implementations for the agent.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// StructuredOutputTool provides a synthetic tool that enforces structured JSON output.
// It is only available in non-interactive (streaming) sessions and validates output
// against a caller-supplied JSON schema.
type StructuredOutputTool struct {
	schema     map[string]any
	schemaJSON string // raw JSON string for tool description
	mu         sync.Mutex
	emitted    bool // tracks whether StructuredOutput was called this turn
}

// NewStructuredOutputTool creates a new StructuredOutputTool with the given schema.
// The schema must be a valid JSON object (map[string]any).
func NewStructuredOutputTool(schema map[string]any) *StructuredOutputTool {
	// Serialize schema to JSON for description
	schemaJSON, _ := json.Marshal(schema)
	return &StructuredOutputTool{
		schema:     schema,
		schemaJSON: string(schemaJSON),
	}
}

// Name returns the tool name.
func (t *StructuredOutputTool) Name() string {
	return "StructuredOutput"
}

// Description returns a description including the expected schema.
func (t *StructuredOutputTool) Description() string {
	return fmt.Sprintf(
		"Emits structured JSON output matching the caller-supplied schema. "+
			"Must be called exactly once at the end of the turn. "+
			"Schema: %s",
		t.schemaJSON,
	)
}

// InputSchema returns the JSON schema for tool input.
// The input is wrapped in an envelope with a "value" field containing the user schema
// and a "format" field indicating "json" or "text".
func (t *StructuredOutputTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": map[string]any{
				"type":        "object",
				"description": "The structured output value conforming to the schema",
				// Dynamically incorporate user schema properties
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Output format: 'json' for JSON, 'text' for plain text",
				"enum":        []any{"json", "text"},
			},
		},
		"required": []string{"value"},
	}
}

// Execute runs the StructuredOutput tool.
// It validates the input against the schema and marks the output as emitted.
// Returns an error if called more than once per turn.
func (t *StructuredOutputTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	t.mu.Lock()
	if t.emitted {
		t.mu.Unlock()
		return &ToolResult{
			Content: "structured output already emitted this turn",
			IsError: true,
		}, nil
	}
	t.mu.Unlock()

	// Extract value from input
	value, ok := input["value"]
	if !ok {
		return &ToolResult{
			Content: "value field is required",
			IsError: true,
		}, nil
	}

	// Validate value against schema using basic structural validation
	if err := validateAgainstSchema(value, t.schema); err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("schema validation failed: %v", err),
			IsError: true,
		}, nil
	}

	// Marshal the validated value to JSON
	outputJSON, err := json.Marshal(value)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("failed to marshal output: %v", err),
			IsError: true,
		}, nil
	}

	// Only mark as emitted after successful validation
	t.mu.Lock()
	t.emitted = true
	t.mu.Unlock()

	return &ToolResult{
		Content: string(outputJSON),
		IsError: false,
	}, nil
}

// Reset resets the emitted flag at the start of each turn.
func (t *StructuredOutputTool) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emitted = false
}

// IsEmitted returns whether the tool has already been called this turn.
func (t *StructuredOutputTool) IsEmitted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.emitted
}

// ValidateStructuredSchema validates that the given schema string is a valid JSON
// object that can serve as a JSON Schema Draft-07 compatible schema.
// It returns the parsed schema map on success.
func ValidateStructuredSchema(schemaStr string) (map[string]any, error) {
	// Step 1: Must be valid JSON
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaStr), &schema); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Step 2: Basic structural validation for JSON Schema Draft-07
	// The schema must be an object (already enforced by successful unmarshal)
	// Check that type is present and is "object" if specified
	if typeVal, ok := schema["type"]; ok {
		if typeStr, ok := typeVal.(string); ok {
			if typeStr != "object" {
				return nil, fmt.Errorf("type must be 'object', got %q", typeStr)
			}
		} else {
			return nil, fmt.Errorf("type must be a string, got %T", typeVal)
		}
	}

	// Step 3: If properties is present, it must be an object
	if props, ok := schema["properties"]; ok {
		if _, ok := props.(map[string]any); !ok {
			return nil, fmt.Errorf("properties must be an object")
		}
	}

	return schema, nil
}

// validateAgainstSchema performs basic validation of value against the schema.
// It checks that required properties are present and types match.
func validateAgainstSchema(value any, schema map[string]any) error {
	// Only do basic validation: check that value is a map if schema has type "object"
	if schemaType, ok := schema["type"]; ok {
		if schemaType == "object" {
			if _, ok := value.(map[string]any); !ok {
				return fmt.Errorf("expected object, got %T", value)
			}
		}
	}

	// Check required fields
	if required, ok := schema["required"].([]any); ok {
		valMap, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("value must be an object when schema has required fields")
		}
		for _, req := range required {
			reqStr, ok := req.(string)
			if !ok {
				continue
			}
			if _, exists := valMap[reqStr]; !exists {
				return fmt.Errorf("required field %q is missing", reqStr)
			}
		}
	}

	return nil
}
