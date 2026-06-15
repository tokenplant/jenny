#!/bin/bash
# Compare stream-json output between jenny and claude using e2e framework
#
# Usage:
#   ./scripts/compare-stream-json.sh              # compare both binaries
#   ./scripts/compare-stream-json.sh --jenny-only  # run only with jenny
#   ./scripts/compare-stream-json.sh --claude-only # run only with claude
#
# Environment:
#   JENNY_BIN       - path to jenny binary (default: ./jenny)
#   REFERENCE_BIN   - path to claude binary (default: claude)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
cd "$REPO_DIR"

# Defaults
JENNY_BIN="${JENNY_BIN:-$REPO_DIR/jenny}"
REFERENCE_BIN="${REFERENCE_BIN:-$(which claude)}"

# Parse flags
RUN_MODE="both"
while [[ $# -gt 0 ]]; do
    case $1 in
        --jenny-only)
            RUN_MODE="jenny"
            shift
            ;;
        --claude-only)
            RUN_MODE="claude"
            shift
            ;;
        *)
            shift
            ;;
    esac
done

GO_TEST_FLAGS="-v -count=1"

echo "=== Stream-JSON Comparison via E2E Framework ==="
echo "Jenny:     $JENNY_BIN"
echo "Claude:   $REFERENCE_BIN"
echo "Mode:     $RUN_MODE"
echo ""

# Verify binaries exist
check_bin() {
    local bin="$1"
    local name="$2"
    if [[ ! -x "$bin" ]]; then
        echo "Error: $name not found or not executable: $bin"
        exit 1
    fi
}

# Build jenny if needed
if [[ "$JENNY_BIN" == "$REPO_DIR/jenny" && ! -x "$REPO_DIR/jenny" ]]; then
    echo "Building jenny..."
    go build -o "$REPO_DIR/jenny" ./cmd/jenny
fi

run_tests() {
    local name="$1"
    local bin="$2"
    local test_filter="${3:-Stream}"
    echo "--- Running with $name (JENNY_BIN=$bin) ---"
    JENNY_BIN="$bin" go test ./e2e/... $GO_TEST_FLAGS -run "$test_filter"
    echo ""
}

case "$RUN_MODE" in
    jenny)
        check_bin "$JENNY_BIN" "Jenny"
        # Run core stream-json tests (skip Gap tests - they reveal jenny gaps vs claude)
        run_tests "jenny" "$JENNY_BIN" "TestStream(IsTrue|JSONEnvelope|JSONInitEvent|JSONResultEvent|JSONAssistantEvent|JSONEventSequence|JSONToolCallEvents|JSONUserToolResult|JSONSessionIDMatch|_ReferenceAlignment|UserToolResultFormat)"
        ;;
    claude)
        check_bin "$REFERENCE_BIN" "Claude"
        # Run core stream-json tests that both jenny and claude should pass
        run_tests "claude" "$REFERENCE_BIN" "TestStream(IsTrue|JSONEnvelope|JSONInitEvent|JSONResultEvent|JSONAssistantEvent|JSONEventSequence|JSONToolCallEvents|JSONUserToolResult|JSONSessionIDMatch|_ReferenceAlignment|Gap_ResultExtendedFields|Gap_InitExtendedFields|UserToolResultFormat)"
        ;;
    both)
        check_bin "$JENNY_BIN" "Jenny"
        check_bin "$REFERENCE_BIN" "Claude"
        echo ""
        echo "=== Running jenny (core stream-json tests only) ==="
        run_tests "jenny" "$JENNY_BIN" "TestStream(IsTrue|JSONEnvelope|JSONInitEvent|JSONResultEvent|JSONAssistantEvent|JSONEventSequence|JSONToolCallEvents|JSONUserToolResult|JSONSessionIDMatch|_ReferenceAlignment|UserToolResultFormat)"
        echo ""
        echo "=== Running claude (core + extended fields) ==="
        run_tests "claude" "$REFERENCE_BIN" "TestStream(IsTrue|JSONEnvelope|JSONInitEvent|JSONResultEvent|JSONAssistantEvent|JSONEventSequence|JSONToolCallEvents|JSONUserToolResult|JSONSessionIDMatch|_ReferenceAlignment|Gap_ResultExtendedFields|Gap_InitExtendedFields|UserToolResultFormat)"
        ;;
esac

echo "=== Done ==="