---
title: macOS Double-Click Portal Launch
slug: macos-portal-launch
priority: P1
status: complete
spec: complete
code: complete
package: cmd/jenny
depends_on:
  - webui-portal
---

# macOS Double-Click Portal Launch

## Overview

Fix for two bugs related to portal launch behavior:
1. `./jenny` (no arguments, terminal) was incorrectly launching the portal
2. Double-clicking the binary on macOS opened a Terminal window instead of running in background

## Root Cause

The previous fix (`len(os.Args) < 2`) was too aggressive - it couldn't distinguish terminal invocation from GUI double-click.

## Solution

### 1. Smart `shouldLaunchPortal()` Detection

Replace the unconditional auto-launch with path-based detection:

```go
func shouldLaunchPortal() bool {
    // Explicit "portal" subcommand always works
    if len(os.Args) >= 2 && os.Args[1] == "portal" {
        return true
    }
    // When no arguments, check if running from a macOS .app bundle
    if len(os.Args) < 2 {
        exe, err := os.Executable()
        if err != nil {
            return false
        }
        // macOS .app bundles have paths like: .../Jenny Portal.app/Contents/MacOS/jenny
        if strings.Contains(exe, ".app/Contents/MacOS/") {
            return true
        }
    }
    return false
}
```

### 2. macOS `.app` Bundle Generator

Create `scripts/make-portal-app.sh` that generates a minimal `.app` bundle with `LSUIElement=true` (background app, no Terminal, no Dock icon).

## Testable Acceptance Criteria

| AC | Description | Verification |
|----|-------------|--------------|
| AC1 | `./jenny` (no args, terminal) shows help text | Does NOT launch portal |
| AC2 | `./jenny portal` launches portal | Backward compatible |
| AC3 | Executable from `.app/Contents/MacOS/` path auto-launches portal | Detection works |
| AC4 | `make-portal-app.sh` creates valid `.app` bundle | Structure correct |
| AC5 | `go test ./internal/portal/` passes | Tests green |
| AC6 | `go test ./cmd/jenny/` passes | Tests green |

## Behavior Matrix

| Scenario | os.Args | exe path | Result |
|----------|---------|----------|--------|
| `./jenny portal` | [jenny, portal] | any | Portal launches |
| `./jenny` (terminal) | [jenny] | ./jenny or /usr/local/bin/jenny | CLI mode (help) |
| Double-click `Jenny Portal.app` | [jenny] | .../Jenny Portal.app/Contents/MacOS/jenny | Portal auto-launches |

## Cross-Platform Notes

- `.app` detection only triggers on macOS (path won't match on Linux/Windows)
- `os.Executable()` may return relative paths - `strings.Contains` on `./jenny` won't match `.app/Contents/MacOS/`
- The `make-portal-app.sh` script is macOS-specific but harmless on other platforms
