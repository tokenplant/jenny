package mockapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCassetteLookup(t *testing.T) {
	// Verify .sse lookup
	path, err := Lookup("anthropic/hello-world")
	if err != nil {
		t.Fatalf("Lookup(anthropic/hello-world) failed: %v", err)
	}
	if !strings.HasSuffix(path, "hello-world.sse") {
		t.Errorf("expected path ending with hello-world.sse, got %s", path)
	}

	// Verify .json lookup
	path, err = Lookup("openai/chat-basic")
	if err != nil {
		t.Fatalf("Lookup(openai/chat-basic) failed: %v", err)
	}
	if !strings.HasSuffix(path, "chat-basic.json") {
		t.Errorf("expected path ending with chat-basic.json, got %s", path)
	}

	// Verify error for nonexistent
	_, err = Lookup("nonexistent/nothing")
	if err == nil {
		t.Fatal("expected error for nonexistent cassette")
	}
}

func TestCassetteLoading(t *testing.T) {
	// Use the central testdata directory
	ms := NewMockServer(WithCassetteDir("testdata"))
	defer ms.Close()

	resp, err := http.Post(ms.URL()+"/cassette/anthropic/hello-world/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestMockServerStart(t *testing.T) {
	tmpDir := t.TempDir()
	// Write a minimal cassette
	cassetteContent := "event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	os.WriteFile(filepath.Join(tmpDir, "test-cassette.sse"), []byte(cassetteContent), 0644)

	s := NewMockServer(WithCassetteDir(tmpDir))
	defer s.Close()

	// POST to /cassette/test-cassette/v1/messages
	resp, err := http.Post(s.URL()+"/cassette/test-cassette/v1/messages", "application/json",
		strings.NewReader(`{"model":"test","max_tokens":100}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	reqs := s.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	if reqs[0].Body["model"] != "test" {
		t.Errorf("model = %v, want test", reqs[0].Body["model"])
	}
}

func TestMockServerCassetteNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMockServer(WithCassetteDir(tmpDir))
	defer s.Close()

	resp, err := http.Post(s.URL()+"/cassette/nonexistent/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("got %d, want 400", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if !strings.Contains(body["error"], "cassette not found") {
		t.Errorf("error = %q, want cassette not found", body["error"])
	}
}

func TestMockServerMethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewMockServer(WithCassetteDir(tmpDir))
	defer s.Close()

	resp, err := http.Get(s.URL() + "/cassette/test/v1/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 405 {
		t.Errorf("got %d, want 405", resp.StatusCode)
	}
}

func TestMockServerClearRequests(t *testing.T) {
	tmpDir := t.TempDir()
	cassetteContent := "event: message_start\ndata: {}\n\n"
	os.WriteFile(filepath.Join(tmpDir, "test.sse"), []byte(cassetteContent), 0644)

	s := NewMockServer(WithCassetteDir(tmpDir))
	defer s.Close()

	http.Post(s.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if len(s.Requests()) != 1 {
		t.Fatal("expected 1 request")
	}
	s.ClearRequests()
	if len(s.Requests()) != 0 {
		t.Fatal("expected 0 requests after ClearRequests")
	}
}

// E1 tests

func TestSetInlineResponse(t *testing.T) {
	ms := NewMockServer() // no cassette dir
	defer ms.Close()

	ms.SetInlineResponse("test", "event: inline\ndata: {}\n\n")

	resp, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "inline") {
		t.Errorf("body = %q, want inline", string(body))
	}

	// Empty clears — falls back to 400 because no file-based cassette
	ms.SetInlineResponse("test", "")
	resp2, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Errorf("after clear, got %d, want 400", resp2.StatusCode)
	}
}

func TestSetInlineResponseWithFileFallback(t *testing.T) {
	tmpDir := t.TempDir()
	cassetteContent := "event: message_start\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"
	os.WriteFile(filepath.Join(tmpDir, "test.sse"), []byte(cassetteContent), 0644)

	ms := NewMockServer(WithCassetteDir(tmpDir))
	defer ms.Close()

	// Set inline for same cassetteID — inline wins
	ms.SetInlineResponse("test", "event: inline\ndata: {}\n\n")

	resp, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "inline") {
		t.Errorf("expected inline response, got %q", string(body))
	}

	// Clear inline — falls back to file
	ms.SetInlineResponse("test", "")
	resp2, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "message_start") {
		t.Errorf("expected file-based response, got %q", string(body2))
	}
}

// E2 tests

func TestSetRequestInspector(t *testing.T) {
	tmpDir := t.TempDir()
	cassetteContent := "event: message_start\ndata: {}\n\n"
	os.WriteFile(filepath.Join(tmpDir, "test.sse"), []byte(cassetteContent), 0644)

	ms := NewMockServer(WithCassetteDir(tmpDir))
	defer ms.Close()

	ms.SetRequestInspector(func(r APIRequest) error {
		if r.Body["model"] == "bad-model" {
			return errors.New("rejecting bad model")
		}
		return nil
	})

	// Inspector passes
	resp, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"good-model"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}

	// Inspector rejects
	resp2, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"bad-model"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Errorf("got %d, want 400", resp2.StatusCode)
	}
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "rejecting bad model") {
		t.Errorf("expected error message, got %q", string(body))
	}
}

// E3 tests

func TestSetErrorResponse(t *testing.T) {
	ms := NewMockServer()
	defer ms.Close()

	ms.SetErrorResponse("rate-limit", 429)

	resp, err := http.Post(ms.URL()+"/cassette/rate-limit/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 429 {
		t.Errorf("got %d, want 429", resp.StatusCode)
	}

	// Clear override — falls back to 400 (no cassette file)
	ms.SetErrorResponse("rate-limit", 0)
	resp2, err := http.Post(ms.URL()+"/cassette/rate-limit/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 400 {
		t.Errorf("after clear: got %d, want 400", resp2.StatusCode)
	}
}

// E4 tests

func TestSetContentType(t *testing.T) {
	tmpDir := t.TempDir()
	cassetteContent := "event: message_start\ndata: {}\n\n"
	os.WriteFile(filepath.Join(tmpDir, "json-cassette.sse"), []byte(cassetteContent), 0644)

	ms := NewMockServer(WithCassetteDir(tmpDir))
	defer ms.Close()

	ms.SetContentType("json-cassette", "application/json")

	resp, err := http.Post(ms.URL()+"/cassette/json-cassette/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// E5 tests

func TestSetPathHandler(t *testing.T) {
	ms := NewMockServer()
	defer ms.Close()

	ms.SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chat-123"}`))
	})

	resp, err := http.Post(ms.URL()+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"gpt-4"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "chat-123") {
		t.Errorf("expected chat-123 response, got %q", string(body))
	}

	// GET to same path should return 405 (no handler for GET)
	resp2, err := http.Get(ms.URL() + "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 405 {
		t.Errorf("GET got %d, want 405", resp2.StatusCode)
	}
}

func TestSetPathHandlerPathMismatch(t *testing.T) {
	ms := NewMockServer()
	defer ms.Close()

	// Register handler for a specific path
	ms.SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Unregistered path should fall through to cassette dispatcher
	// Since no cassette dir, it should return 400
	resp, err := http.Post(ms.URL()+"/cassette/test/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("unregistered path got %d, want 400", resp.StatusCode)
	}
}

// NewTestServer tests

func TestNewTestServer(t *testing.T) {
	// Verify that NewTestServer with the real hello-world fixture works.
	// If the fixture is missing, NewTestServer panics (fail-fast).
	ms := NewTestServer(t, "anthropic/hello-world")
	defer ms.Close()

	if ms.URL() == "" {
		t.Fatal("expected non-empty URL")
	}

	// Verify request capture works
	resp, err := http.Post(ms.URL()+"/cassette/anthropic/hello-world/v1/messages", "application/json",
		strings.NewReader(`{"model":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("got %d, want 200; body: %s", resp.StatusCode, string(body))
	}
}

func TestNewMockServerSignature(t *testing.T) {
	// Verify new constructor works with options pattern
	ms := NewMockServer(WithCassetteDir(t.TempDir()))
	defer ms.Close()
	if ms.URL() == "" {
		t.Fatal("expected non-empty URL")
	}
}