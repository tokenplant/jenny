package toolresult

import (
	"encoding/json"
	"testing"
)

func TestToolResult_JSON(t *testing.T) {
	tr := ToolResult{Content: "hello"}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got ToolResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Content != "hello" {
		t.Errorf("Content = %q, want %q", got.Content, "hello")
	}
}

func TestToolResult_JSON_OptionalFields(t *testing.T) {
	tr := ToolResult{
		Content:    "error!",
		IsError:    true,
		Truncated:  true,
		OutputFile: "/tmp/result.txt",
		CacheHit:   false,
	}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got ToolResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Content != "error!" {
		t.Errorf("Content = %q, want %q", got.Content, "error!")
	}
	if !got.IsError {
		t.Error("IsError = false, want true")
	}
	if !got.Truncated {
		t.Error("Truncated = false, want true")
	}
	if got.OutputFile != "/tmp/result.txt" {
		t.Errorf("OutputFile = %q, want %q", got.OutputFile, "/tmp/result.txt")
	}
}

func TestToolResult_IsError_Omitempty(t *testing.T) {
	tr := ToolResult{Content: "ok"}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(data) == "" {
		t.Fatal("marshal produced empty string")
	}
	if contains(string(data), "is_error") {
		t.Errorf("json includes is_error for false value: %s", string(data))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > 0 && len(substr) > 0 && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			contains(s[1:], substr))))
}
