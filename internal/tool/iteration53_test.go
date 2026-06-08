package tool

import (
	"context"
	"encoding/json"
	"testing"
)

// ============================================================================
// AC2: Schema validation
// ============================================================================

func TestAC2_ValidSchema_ParsesSuccessfully(t *testing.T) {
	validSchema := `{"type": "object", "properties": {"name": {"type": "string"}}}`
	schema, err := ValidateStructuredSchema(validSchema)
	if err != nil {
		t.Fatalf("expected valid schema to parse, got error: %v", err)
	}
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

func TestAC2_InvalidJSON_FailsParsing(t *testing.T) {
	invalidJSON := `not json at all`
	_, err := ValidateStructuredSchema(invalidJSON)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' error, got: %v", err)
	}
}

func TestAC2_TypeNotString_Fails(t *testing.T) {
	invalidSchema := `{"type": 123}`
	_, err := ValidateStructuredSchema(invalidSchema)
	if err == nil {
		t.Fatal("expected error for non-string type, got nil")
	}
	if !contains(err.Error(), "type must be a string") {
		t.Errorf("expected 'type must be a string' error, got: %v", err)
	}
}

func TestAC2_TypeNotObject_Fails(t *testing.T) {
	invalidSchema := `{"type": "string"}`
	_, err := ValidateStructuredSchema(invalidSchema)
	if err == nil {
		t.Fatal("expected error for non-object type, got nil")
	}
	if !contains(err.Error(), "type must be 'object'") {
		t.Errorf("expected 'type must be object' error, got: %v", err)
	}
}

func TestAC2_PropertiesNotObject_Fails(t *testing.T) {
	invalidSchema := `{"type": "object", "properties": "not an object"}`
	_, err := ValidateStructuredSchema(invalidSchema)
	if err == nil {
		t.Fatal("expected error for non-object properties, got nil")
	}
	if !contains(err.Error(), "properties must be an object") {
		t.Errorf("expected 'properties must be an object' error, got: %v", err)
	}
}

func TestAC2_MissingType_Accepted(t *testing.T) {
	// Schema without type is technically valid (any JSON value allowed)
	schema := `{"properties": {"name": {"type": "string"}}}`
	parsed, err := ValidateStructuredSchema(schema)
	if err != nil {
		t.Fatalf("schema without type should be accepted, got error: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil schema")
	}
}

// ============================================================================
// AC1: StructuredOutputTool creation and interface
// ============================================================================

func TestAC1_StructuredOutputTool_Name(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tool := NewStructuredOutputTool(schema)
	if tool.Name() != "StructuredOutput" {
		t.Errorf("expected name 'StructuredOutput', got %q", tool.Name())
	}
}

func TestAC1_StructuredOutputTool_Description(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}
	tool := NewStructuredOutputTool(schema)
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !contains(desc, "structured") {
		t.Errorf("description should mention 'structured', got: %s", desc)
	}
}

func TestAC1_StructuredOutputTool_InputSchema(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tool := NewStructuredOutputTool(schema)
	inputSchema := tool.InputSchema()
	if inputSchema == nil {
		t.Fatal("expected non-nil input schema")
	}
	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in input schema")
	}
	if _, ok := props["value"]; !ok {
		t.Error("expected 'value' property in input schema")
	}
	if _, ok := props["format"]; !ok {
		t.Error("expected 'format' property in input schema")
	}
}

func TestAC1_InputSchema_DynamicUserSchema(t *testing.T) {
	// AC1: InputSchema should dynamically incorporate user schema properties
	userSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
		"required": []any{"name"},
	}
	tool := NewStructuredOutputTool(userSchema)
	inputSchema := tool.InputSchema()

	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in input schema")
	}

	valueProp, ok := props["value"].(map[string]any)
	if !ok {
		t.Fatal("expected 'value' property to be a map")
	}

	// AC1: The value property should have the user schema's properties
	// Check that 'properties' from user schema is in the value property
	valueProps, ok := valueProp["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected 'properties' in value property")
	}

	if _, ok := valueProps["name"]; !ok {
		t.Error("AC1 FAIL: 'value.properties' should include 'name' from user schema")
	} else {
		t.Log("AC1 PASS: 'value.properties' includes 'name' from user schema")
	}

	if _, ok := valueProps["age"]; !ok {
		t.Error("AC1 FAIL: 'value.properties' should include 'age' from user schema")
	} else {
		t.Log("AC1 PASS: 'value.properties' includes 'age' from user schema")
	}

	// AC1: The value property should have type "object" (inherited from user schema)
	if valueProp["type"] != "object" {
		t.Errorf("AC1 FAIL: 'value' property should have type 'object', got %v", valueProp["type"])
	} else {
		t.Log("AC1 PASS: 'value' property has type 'object'")
	}

	// AC1: The value property should have required: ["value"]
	if req, ok := valueProp["required"].([]any); ok {
		if len(req) != 1 || req[0] != "value" {
			t.Errorf("AC1 FAIL: 'value.required' should be [\"value\"], got %v", req)
		} else {
			t.Log("AC1 PASS: 'value.required' is [\"value\"]")
		}
	} else {
		t.Error("AC1 FAIL: 'value.required' should exist and be [\"value\"]")
	}
}

func TestAC1_StructuredOutputTool_Reset(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tool := NewStructuredOutputTool(schema)

	if tool.IsEmitted() {
		t.Error("expected IsEmitted() to be false initially")
	}

	// Simulate a call
	_, _ = tool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"result": "test"},
	}, "/tmp")

	if !tool.IsEmitted() {
		t.Error("expected IsEmitted() to be true after Execute")
	}

	// Reset
	tool.Reset()
	if tool.IsEmitted() {
		t.Error("expected IsEmitted() to be false after Reset")
	}
}

// ============================================================================
// AC3: Execute enforces single emission per turn
// ============================================================================

func TestAC3_SecondExecute_ReturnsError(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tool := NewStructuredOutputTool(schema)

	// First call succeeds
	result1, err1 := tool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"result": "first"},
	}, "/tmp")
	if err1 != nil {
		t.Fatalf("first Execute failed: %v", err1)
	}
	if result1.IsError {
		t.Error("first Execute should not be error")
	}

	// Second call fails
	result2, err2 := tool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"result": "second"},
	}, "/tmp")
	if err2 != nil {
		t.Fatalf("second Execute returned error: %v", err2)
	}
	if result2 == nil {
		t.Fatal("expected non-nil result")
	}
	if !result2.IsError {
		t.Error("second Execute should be error")
	}
	if !contains(result2.Content, "already emitted") {
		t.Errorf("expected 'already emitted' error, got: %s", result2.Content)
	}
}

func TestAC3_ExecuteWithMissingValue_ReturnsError(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tool := NewStructuredOutputTool(schema)

	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("Execute with missing value should be error")
	}
	if !contains(result.Content, "value field is required") {
		t.Errorf("expected 'value field is required' error, got: %s", result.Content)
	}
}

func TestAC3_ExecuteWithValidValue_ReturnsValidatedJSON(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tool := NewStructuredOutputTool(schema)

	result, err := tool.Execute(context.Background(), map[string]any{
		"value": map[string]any{
			"name": "test",
			"age":  42,
		},
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Errorf("Execute should not be error, got: %s", result.Content)
	}

	// Verify output is valid JSON
	var output any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Errorf("output should be valid JSON: %v", err)
	}
}

func TestAC3_ExecuteWithRequiredFields_ValidatesPresence(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"name"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	tool := NewStructuredOutputTool(schema)

	// Missing required field
	result, err := tool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"age": 42},
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("Execute with missing required field should be error")
	}
	if !contains(result.Content, "required field") {
		t.Errorf("expected 'required field' error, got: %s", result.Content)
	}

	// Present required field
	result2, err2 := tool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"name": "test"},
	}, "/tmp")
	if err2 != nil {
		t.Fatalf("Execute returned error: %v", err2)
	}
	if result2 == nil {
		t.Fatal("expected non-nil result")
	}
	if result2.IsError {
		t.Errorf("Execute with required field present should succeed, got: %s", result2.Content)
	}
}
