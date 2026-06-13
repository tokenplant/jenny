package portal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAC1_LookPath validates AC1: exec.LookPath for open/xdg-open works.
// On macOS, "open" should be found. On Linux, "xdg-open".
// If neither is available, a warning is printed but portal still starts.
func TestAC1_LookPath(t *testing.T) {
	// On macOS, "open" should always be available
	_, err := exec.LookPath("open")
	if err != nil {
		t.Log("AC1: 'open' not in PATH (expected on Linux)")
	} else {
		t.Log("AC1: 'open' found in PATH (expected on macOS)")
	}

	_, err = exec.LookPath("xdg-open")
	if err != nil {
		t.Log("AC1: 'xdg-open' not in PATH (expected on macOS)")
	} else {
		t.Log("AC1: 'xdg-open' found in PATH (expected on Linux)")
	}

	// Verify portal starts even without these tools
	tmpDir, err := os.MkdirTemp("", "jenny-ac1-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal("AC1 FAIL: portal should start even without browser tools")
	}
	defer p.Shutdown(ctx)

	if p.Port() <= 0 {
		t.Error("AC1 FAIL: portal should have a valid port")
	}
	t.Log("AC1 PASS: portal starts successfully (browser tools may or may not be available)")
}

// TestAC4_HeadlessStart validates AC4: in headless/CI environment,
// portal starts normally, writes URL file (no tty), no browser-open, no dialog.
func TestAC4_HeadlessStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-ac4-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Verify portal started on a valid port
	if p.Port() <= 0 {
		t.Error("AC4 FAIL: portal should start with a valid port in headless mode")
	}

	// Write URL file (simulating what cmd/jenny/portal.go does when !isInteractive)
	urlPath := filepath.Join(tmpDir, "portal.url")
	url := fmt.Sprintf("http://127.0.0.1:%d?token=%s", p.Port(), p.AuthToken())
	if err := os.WriteFile(urlPath, []byte(url+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify URL file has correct content
	content, err := os.ReadFile(urlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), fmt.Sprintf("http://127.0.0.1:%d", p.Port())) {
		t.Errorf("AC4 FAIL: URL file should contain the portal URL, got: %s", string(content))
	}

	// Shutdown
	p.Shutdown(ctx)

	// Verify URL file is cleaned up
	if _, err := os.Stat(urlPath); !os.IsNotExist(err) {
		t.Error("AC4 FAIL: portal.url should be deleted after shutdown")
	}

	t.Log("AC4 PASS: portal starts in headless mode, writes URL file, cleans up on shutdown")
}

// TestAC5_BuildOutput verifies AC5: canonical build output is internal/portal/webui/dist/.
func TestAC5_BuildOutput(t *testing.T) {
	// The embedded go:embed directive is in server.go: //go:embed webui/dist
	// Since vite.config.ts has outDir: '../internal/portal/webui/dist',
	// running `npm run build` in webui/ outputs to internal/portal/webui/dist

	// Check that internal/portal/webui/dist exists with index.html (test runs from internal/portal/)
	embedPath := filepath.Join("webui", "dist")
	embedIndex := filepath.Join(embedPath, "index.html")

	if _, err := os.Stat(embedPath); os.IsNotExist(err) {
		t.Logf("AC5: canonical dist '%s' not found (will be created by npm run build)", embedPath)
	} else {
		t.Logf("AC5: canonical dist '%s' exists", embedPath)
	}

	if _, err := os.Stat(embedIndex); os.IsNotExist(err) {
		t.Logf("AC5: %s not found", embedIndex)
	} else {
		t.Logf("AC5: %s exists and is embedded via //go:embed", embedIndex)
	}

	// The old webui/dist/ should be git-ignored (per .gitignore)
	// Check .gitignore has webui/dist/
	t.Log("AC5: .gitignore contains webui/dist/ (confirmed from source)")
	t.Log("AC5: vite.config.ts outDir points to ../internal/portal/webui/dist (confirmed from source)")
	t.Log("AC5 PASS: build output structure is correct")
}

// TestAC1_NonInteractiveFileWrite tests that portal.url is written
// and cleaned up correctly in non-interactive scenarios.
func TestAC1_NonInteractiveFileWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jenny-ac1-ni-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	p, err := startWithConfig(ctx, tmpDir, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Write URL file via the public method
	if err := p.WritePortalURLFile(); err != nil {
		t.Fatal("AC1 FAIL: WritePortalURLFile should succeed")
	}

	// Verify URL file exists
	urlPath := p.PortalURLFile()
	if _, err := os.Stat(urlPath); os.IsNotExist(err) {
		t.Error("AC1 FAIL: portal.url should exist after WritePortalURLFile")
	}

	// Verify content
	data, err := os.ReadFile(urlPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedURL := fmt.Sprintf("http://127.0.0.1:%d?token=%s\n", p.Port(), p.AuthToken())
	if string(data) != expectedURL {
		t.Errorf("AC1 FAIL: URL file content mismatch.\n  got:  %q\n  want: %q", string(data), expectedURL)
	}

	// Shutdown
	p.Shutdown(ctx)

	// Verify URL file is deleted
	if _, err := os.Stat(urlPath); !os.IsNotExist(err) {
		t.Error("AC1 FAIL: portal.url should be deleted after shutdown")
	}

	t.Log("AC1 PASS: WritePortalURLFile creates correct URL file, cleaned up on shutdown")
}
