#!/usr/bin/env bash
set -euo pipefail

# Creates a macOS .app bundle for Jenny Portal that launches without Terminal.
# Usage: bash scripts/make-portal-app.sh [path-to-jenny-binary]

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="${1:-$REPO_DIR/jenny}"
APP_NAME="Jenny Portal.app"
APP_DIR="$REPO_DIR/dist/$APP_NAME"

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "Error: binary not found at $BINARY" >&2
    echo "Usage: bash scripts/make-portal-app.sh [path-to-jenny-binary]" >&2
    exit 1
fi

mkdir -p "$APP_DIR/Contents/MacOS"
mkdir -p "$APP_DIR/Contents/Resources"

# Copy the binary
cp "$BINARY" "$APP_DIR/Contents/MacOS/jenny"
chmod +x "$APP_DIR/Contents/MacOS/jenny"

# Create Info.plist with LSUIElement=true (background app, no Dock/No Terminal)
cat > "$APP_DIR/Contents/Info.plist" << PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
 "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>jenny</string>
    <key>CFBundleIdentifier</key>
    <string>com.ipy.jenny-portal</string>
    <key>CFBundleName</key>
    <string>Jenny Portal</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>
PLIST_EOF

echo "Created: $APP_DIR"
echo "Double-click $APP_DIR to start the portal without Terminal."
