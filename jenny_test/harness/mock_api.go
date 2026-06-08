// Package harness provides utilities for blackbox end-to-end tests of jenny.
//
// The mock API server intercepts requests to the Anthropic API and replays
// SSE cassettes that are committed alongside the tests. The cassette id is
// encoded as a URL path prefix (/cassette/<id>/v1/messages) so no changes
// to jenny's own code are required.
package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// APIRequest is one captured request received by the mock server.
type APIRequest struct {
	Body   map[string]any
	Header http.Header
}

// MockServer is an in-process mock of the Anthropic API.
//
// The mock server captures the JSON-decoded body of every incoming request
// and serves SSE cassette content as the response. The cassette to replay
// is selected from the URL path prefix `/cassette/<id>/v1/messages`; that
// prefix is the only contract between the test and the handler.
//
// A cassette id can be associated with an ordered sequence of cassette
// file names via SetSequence, which enables multi-turn flows (e.g. a
// model request that elicits a tool_use response on turn 1 and a final
// end_turn response on turn 2). The first request to a sequenced id
// streams the first cassette, the second streams the second, and
// exhaustion returns HTTP 400.
type MockServer struct {
	Server      *httptest.Server
	CassetteDir string

	mu       sync.Mutex
	requests []APIRequest

	// Sequence state. seqDef maps a cassette id (the URL path segment)
	// to the ordered list of cassette file names (without ".sse") to
	// serve. seqIdx tracks the next index to use per id. Both maps are
	// nil until SetSequence is first called.
	seqMu  sync.Mutex
	seqDef map[string][]string
	seqIdx map[string]int
}

// NewMockServer starts a new mock server that serves cassettes from
// cassetteDir. Call Close when done.
func NewMockServer(cassetteDir string) *MockServer {
	m := &MockServer{CassetteDir: cassetteDir}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// URL returns the base URL of the mock server.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close stops the mock server.
func (m *MockServer) Close() {
	m.Server.Close()
}

// Requests returns a copy of all requests captured by the mock server.
func (m *MockServer) Requests() []APIRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]APIRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// SetSequence registers an ordered list of cassette file names (without
// the ".sse" extension) to serve for a given cassette id. The first POST
// to /cassette/<id>/v1/messages streams the first cassette, the second
// the second, and so on. After the sequence is exhausted, the mock
// returns HTTP 400 with a JSON error body and does not block or panic.
//
// Single-cassette tests are unaffected: if no sequence is registered
// for a cassette id, the mock serves a single cassette named <id>.sse
// on every request, exactly as before.
func (m *MockServer) SetSequence(cassetteID string, cassettes []string) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	if m.seqDef == nil {
		m.seqDef = make(map[string][]string)
	}
	if m.seqIdx == nil {
		m.seqIdx = make(map[string]int)
	}
	m.seqDef[cassetteID] = cassettes
	m.seqIdx[cassetteID] = 0
}

func (m *MockServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		m.writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s not allowed", r.Method))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		m.writeError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return
	}

	m.mu.Lock()
	m.requests = append(m.requests, APIRequest{Body: decoded, Header: r.Header.Clone()})
	m.mu.Unlock()

	cassetteID, ok := extractCassetteID(r.URL.Path)
	if !ok {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("no cassette id in path %q; expected /cassette/<id>/v1/messages", r.URL.Path))
		return
	}

	// Resolve the effective cassette to serve. If a sequence is
	// registered for this id, advance the per-id index and serve the
	// corresponding entry. If the sequence is exhausted, fail with
	// HTTP 400 and a descriptive error body. When no sequence is
	// registered, fall through to the single-cassette path: serve
	// <cassetteID>.sse on every request.
	effectiveID := cassetteID
	m.seqMu.Lock()
	if seq, hasSeq := m.seqDef[cassetteID]; hasSeq {
		idx := m.seqIdx[cassetteID]
		if idx >= len(seq) {
			m.seqMu.Unlock()
			m.writeError(w, http.StatusBadRequest,
				fmt.Sprintf("cassette sequence %q exhausted after %d requests", cassetteID, len(seq)))
			return
		}
		effectiveID = seq[idx]
		m.seqIdx[cassetteID]++
	}
	m.seqMu.Unlock()

	cassettePath := filepath.Join(m.CassetteDir, effectiveID+".sse")
	cassetteData, err := os.ReadFile(cassettePath)
	if err != nil {
		m.writeError(w, http.StatusBadRequest, fmt.Sprintf("cassette not found: %s: %v", cassettePath, err))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	_, _ = w.Write(cassetteData)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (m *MockServer) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// extractCassetteID extracts the cassette id from a path like
// /cassette/<id>/v1/messages. Returns the id and true on success.
func extractCassetteID(path string) (string, bool) {
	const prefix = "/cassette/"
	const suffix = "/v1/messages"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	id, _, ok := strings.Cut(rest, suffix)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}
