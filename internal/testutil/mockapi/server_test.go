package mockapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMockServerStart(t *testing.T) {
	tmpDir := t.TempDir()
	// Write a minimal cassette
	cassetteContent := "event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	os.WriteFile(filepath.Join(tmpDir, "test-cassette.sse"), []byte(cassetteContent), 0644)

	s := NewMockServer(tmpDir)
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
	s := NewMockServer(tmpDir)
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
	s := NewMockServer(tmpDir)
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

	s := NewMockServer(tmpDir)
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
