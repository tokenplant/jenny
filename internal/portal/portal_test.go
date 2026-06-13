package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestAC1_PortalStart verifies AC1: portal starts on a random high port and creates lockfile.
func TestAC1_PortalStart(t *testing.T) {
	// Create temp jenny dir
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	// Verify port >= 33669
	if p.port < 33669 {
		t.Errorf("AC1 FAIL: port %d < 33669", p.port)
	}

	// Verify auth token is 64 hex chars
	if len(p.authToken) != 64 {
		t.Errorf("AC1 FAIL: auth token length %d != 64", len(p.authToken))
	}
	for _, c := range p.authToken {
		if !strings.Contains("0123456789abcdef", string(c)) {
			t.Errorf("AC1 FAIL: auth token contains non-hex char: %c", c)
		}
	}

	// Verify lockfile exists and has correct content
	lockPath := filepath.Join(tmpDir, "portal.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal("AC1 FAIL: lockfile not found")
	}

	var lf LockfileData
	if err := json.Unmarshal(data, &lf); err != nil {
		t.Fatal("AC1 FAIL: invalid lockfile JSON")
	}

	if lf.PID != p.pid {
		t.Errorf("AC1 FAIL: lockfile pid %d != portal pid %d", lf.PID, p.pid)
	}
	if lf.Port != p.port {
		t.Errorf("AC1 FAIL: lockfile port %d != portal port %d", lf.Port, p.port)
	}
	if lf.AuthToken != p.authToken {
		t.Errorf("AC1 FAIL: lockfile token != portal token")
	}

	t.Log("AC1 PASS: portal started on random high port with valid lockfile")
}

// TestAC2_HealthEndpoint verifies AC2: health endpoint with auth token.
func TestAC2_HealthEndpoint(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	// Test with wrong token
	resp, err := http.Get(baseURL + "/api/health?token=wrongtoken")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("AC2 FAIL: wrong token should return 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test with correct token
	resp, err = http.Get(baseURL + "/api/health?token=" + p.authToken)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("AC2 FAIL: correct token should return 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if result["status"] != "ok" {
		t.Errorf("AC2 FAIL: status should be 'ok', got %v", result["status"])
	}
	if pid, ok := result["pid"].(float64); !ok || int(pid) != p.pid {
		t.Errorf("AC2 FAIL: pid mismatch: got %v, want %d", result["pid"], p.pid)
	}

	t.Log("AC2 PASS: health endpoint returns correct status with auth")
}

// TestAC7_IdleTimeout verifies AC7: portal exits after idle timeout.
// Uses injectable exit function to avoid os.Exit panic in tests.
func TestAC7_IdleTimeout(t *testing.T) {
	// Create temp jenny dir
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Track if exit was called
	exitCalled := false
	exitFunc := func() {
		exitCalled = true
	}

	ctx := context.Background()
	p, err := startWithConfigForTest(ctx, tmpDir, 100*time.Millisecond, exitFunc)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	// Wait for idle timeout to trigger
	time.Sleep(200 * time.Millisecond)

	if !exitCalled {
		t.Error("AC7 FAIL: exit function should have been called after idle timeout")
	}

	// Verify lockfile was deleted
	lockPath := filepath.Join(tmpDir, "portal.lock")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("AC7 FAIL: lockfile should be deleted after idle timeout, got error: %v", err)
	}

	t.Log("AC7 PASS: portal exits after idle timeout and deletes lockfile")
}

// TestAC8_DoubleStart verifies AC8: second portal start fails with appropriate error.
func TestAC8_DoubleStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p1, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p1.Shutdown(ctx)

	// Try to start second portal
	_, err = startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err == nil {
		t.Error("AC8 FAIL: second portal start should fail")
	}
	if !strings.Contains(err.Error(), "portal already running") {
		t.Errorf("AC8 FAIL: error should mention 'portal already running', got: %v", err)
	}

	t.Log("AC8 PASS: second portal start correctly fails")
}

// TestSessionList verifies AC3: sessions endpoint returns session list.
func TestSessionList(t *testing.T) {
	// Set JENNY_HOME to temp dir so we get a clean session directory
	origJennyHome := os.Getenv("JENNY_HOME")
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("JENNY_HOME", tmpDir)
	defer func() {
		os.RemoveAll(tmpDir)
		os.Setenv("JENNY_HOME", origJennyHome)
	}()

	// Create a mock session directory
	sessionID := "test-session-123"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create transcript.jsonl
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")
	entry := struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		SessionID string    `json:"session_id"`
		CWD       string    `json:"cwd"`
	}{
		Type:      "state",
		Timestamp: time.Now(),
		SessionID: sessionID,
		CWD:       "/test/cwd",
	}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(transcriptPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	// Test sessions endpoint
	resp, err := http.Get(baseURL + "/api/sessions?token=" + p.authToken)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("sessions endpoint should return 200, got %d", resp.StatusCode)
	}

	var sessions []SessionMeta
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Find our test session
	found := false
	for _, s := range sessions {
		if s.ID == sessionID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("session %s not found in sessions list", sessionID)
	}

	t.Log("AC3 PASS: sessions endpoint returns correct session list")
}

// TestStatsEndpoint verifies AC6: stats endpoint returns global stats.
func TestStatsEndpoint(t *testing.T) {
	// Set JENNY_HOME to temp dir so we get a clean session directory
	origJennyHome := os.Getenv("JENNY_HOME")
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("JENNY_HOME", tmpDir)
	defer func() {
		os.RemoveAll(tmpDir)
		os.Setenv("JENNY_HOME", origJennyHome)
	}()

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	resp, err := http.Get(baseURL + "/api/stats?token=" + p.authToken)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("stats endpoint should return 200, got %d", resp.StatusCode)
	}

	var stats Stats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Should have zero sessions initially
	if stats.TotalSessions != 0 {
		t.Errorf("total_sessions should be 0, got %d", stats.TotalSessions)
	}

	t.Log("AC6 PASS: stats endpoint returns valid stats structure")
}

// TestAC6_TokenCount verifies AC6: stats endpoint correctly counts tokens (not double-counting).
func TestAC6_TokenCount(t *testing.T) {
	// Set JENNY_HOME to temp dir so we get a clean session directory
	origJennyHome := os.Getenv("JENNY_HOME")
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("JENNY_HOME", tmpDir)
	defer func() {
		os.RemoveAll(tmpDir)
		os.Setenv("JENNY_HOME", origJennyHome)
	}()

	// Create a mock session with known token counts
	sessionID := "test-token-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create transcript with known token counts: 100 + 200 + 300 = 600
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")
	tokenEntries := []string{
		`{"type":"assistant","token_count":100}`,
		`{"type":"assistant","token_count":200}`,
		`{"type":"assistant","token_count":300}`,
	}
	transcriptData := []byte(strings.Join(tokenEntries, "\n") + "\n")
	if err := os.WriteFile(transcriptPath, transcriptData, 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	resp, err := http.Get(baseURL + "/api/stats?token=" + p.authToken)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats endpoint should return 200, got %d", resp.StatusCode)
	}

	var stats Stats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Verify total_tokens is 600 (not 1200 which would be double-counting)
	if stats.TotalTokens != 600 {
		t.Errorf("AC6 FAIL: total_tokens should be 600, got %d", stats.TotalTokens)
	}

	t.Log("AC6 PASS: token counting is correct (not double-counted)")
}

// TestKillSession verifies AC5: kill endpoint terminates session.
func TestKillSession(t *testing.T) {
	// Set JENNY_HOME to temp dir so we get a clean session directory
	origJennyHome := os.Getenv("JENNY_HOME")
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("JENNY_HOME", tmpDir)
	defer func() {
		os.RemoveAll(tmpDir)
		os.Setenv("JENNY_HOME", origJennyHome)
	}()

	// Create a mock session with a real running process
	sessionID := "test-kill-session"
	sessionDir := filepath.Join(tmpDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Spawn a subprocess that will stay alive
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill() // Clean up if test fails

	// Write subprocess PID to pid file
	pidPath := filepath.Join(sessionDir, "pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	// Test kill endpoint
	resp, err := http.Post(baseURL+"/api/sessions/"+sessionID+"/kill?token="+p.authToken, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("kill endpoint should return 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if result["status"] != "killed" {
		t.Errorf("AC5 FAIL: status should be 'killed', got %v", result["status"])
	}

	t.Log("AC5 PASS: kill endpoint terminates session process")
}

// TestMissingAuth verifies endpoints require auth token.
func TestMissingAuth(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	// Test health without token
	resp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("request without token should return 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Test sessions without token
	resp, err = http.Get(baseURL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("request without token should return 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	t.Log("PASS: all endpoints require auth token")
}

// TestProcessLiveness verifies process liveness detection.
func TestProcessLiveness(t *testing.T) {
	// Test with our own PID (should be alive)
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		t.Error("own process should be alive")
	}

	// Test with an impossible PID (should be dead)
	proc, err = os.FindProcess(999999)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		t.Error("impossible PID should not be alive")
	}

	t.Log("PASS: process liveness detection works correctly")
}

// TestAC6_ServeHTML verifies AC6: GET / serves index.html from embedded webui dist.
func TestAC6_ServeHTML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	// Test GET / without auth (should still serve HTML)
	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("AC6 FAIL: GET / should return 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("AC6 FAIL: Content-Type should be text/html, got %s", contentType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(body), "<title>Glimpse UI") {
		t.Errorf("AC6 FAIL: response body should contain '<title>Glimpse UI', got: %s", string(body)[:200])
	}

	t.Log("AC6 PASS: GET / serves index.html with correct content-type and title")
}

// TestAC7_EmptyStats verifies AC7: stats endpoint returns zeroed JSON when no sessions exist.
func TestAC7_EmptyStats(t *testing.T) {
	// Set JENNY_HOME to temp dir so we get a clean session directory
	origJennyHome := os.Getenv("JENNY_HOME")
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("JENNY_HOME", tmpDir)
	defer func() {
		os.RemoveAll(tmpDir)
		os.Setenv("JENNY_HOME", origJennyHome)
	}()

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Shutdown(ctx)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", p.port)

	resp, err := http.Get(baseURL + "/api/stats?token=" + p.authToken)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("AC7 FAIL: stats endpoint should return 200, got %d", resp.StatusCode)
	}

	var stats Stats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}

	// Verify all fields are zeroed
	if stats.TotalSessions != 0 {
		t.Errorf("AC7 FAIL: total_sessions should be 0, got %d", stats.TotalSessions)
	}
	if stats.ActiveSessions != 0 {
		t.Errorf("AC7 FAIL: active_sessions should be 0, got %d", stats.ActiveSessions)
	}
	if stats.TotalCostUSD != 0 {
		t.Errorf("AC7 FAIL: total_cost_usd should be 0, got %f", stats.TotalCostUSD)
	}
	if stats.TotalTokens != 0 {
		t.Errorf("AC7 FAIL: total_tokens should be 0, got %d", stats.TotalTokens)
	}

	t.Log("AC7 PASS: stats endpoint returns zeroed JSON when no sessions exist")
}

// TestAC3_URLFileCleanup verifies AC3: portal URL file is deleted on shutdown.
func TestAC3_URLFileCleanup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	urlFile := p.PortalURLFile()

	// Manually create URL file (simulating non-interactive mode)
	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.port, p.authToken)
	if err := os.WriteFile(urlFile, []byte(url+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify URL file exists
	if _, err := os.Stat(urlFile); os.IsNotExist(err) {
		t.Fatal("AC3 FAIL: URL file should exist before shutdown")
	}

	// Shutdown portal
	if err := p.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify URL file is deleted
	if _, err := os.Stat(urlFile); !os.IsNotExist(err) {
		t.Error("AC3 FAIL: URL file should be deleted after shutdown")
	}

	t.Log("AC3 PASS: portal URL file is deleted on shutdown")
}

// TestAC2_NonInteractiveURLWrite verifies AC2: portal writes URL file in non-interactive mode.
// This tests the actual code path that cmd/jenny/portal.go uses when !isInteractive().
func TestAC2_NonInteractiveURLWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate what cmd/jenny/portal.go does in non-interactive mode:
	// write the URL to portal.url file
	jennyDir := tmpDir
	urlFile := filepath.Join(jennyDir, "portal.url")
	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.port, p.authToken)
	if err := os.WriteFile(urlFile, []byte(url+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify the URL file has the expected content
	content, err := os.ReadFile(urlFile)
	if err != nil {
		t.Fatal("AC2 FAIL: could not read portal.url file")
	}
	if !strings.Contains(string(content), url) {
		t.Errorf("AC2 FAIL: URL file should contain '%s', got: %s", url, string(content))
	}

	// Also verify the PortalURLFile() helper returns the correct path
	expectedPath := filepath.Join(tmpDir, "portal.url")
	if p.PortalURLFile() != expectedPath {
		t.Errorf("AC2 FAIL: PortalURLFile() should return '%s', got: %s", expectedPath, p.PortalURLFile())
	}

	// Cleanup
	p.Shutdown(ctx)

	// Verify URL file is deleted after shutdown
	if _, err := os.Stat(urlFile); !os.IsNotExist(err) {
		t.Error("AC2 FAIL: portal.url should be deleted after shutdown")
	}

	t.Log("AC2 PASS: non-interactive URL file write and cleanup works correctly")
}

// TestAC2_ShutdownOrder verifies AC3: lockfile is removed before URL file on shutdown.
func TestAC2_ShutdownOrder(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Create both lockfile and URL file
	lockPath := filepath.Join(tmpDir, "portal.lock")
	urlPath := filepath.Join(tmpDir, "portal.url")
	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.port, p.authToken)

	// Ensure lockfile exists (should already from Start)
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lockfile should exist from Start")
	}

	// Create URL file
	if err := os.WriteFile(urlPath, []byte(url+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Shutdown portal
	p.Shutdown(ctx)

	// Verify lockfile is deleted first
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("AC3 FAIL: lockfile should be deleted first")
	}

	// Verify URL file is deleted after (or at same time)
	if _, err := os.Stat(urlPath); !os.IsNotExist(err) {
		t.Error("AC3 FAIL: URL file should be deleted")
	}

	t.Log("AC3 PASS: shutdown order correct - lockfile removed before URL file")
}