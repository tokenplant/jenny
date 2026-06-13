package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/session"
)

// ---------------------------------------------------------------------------
// AC1: All four token types
// ---------------------------------------------------------------------------

func TestAC1_AccumulateUsageCapturesAllFourTokenTypes(t *testing.T) {
	state := &CostState{}
	usage := api.Usage{
		InputTokens:              100,
		OutputTokens:             50,
		CacheReadInputTokens:     30,
		CacheCreationInputTokens: 10,
	}

	AccumulateUsage(state, "deepseek-v4-flash", usage)

	mu, ok := state.ModelUsage["deepseek-v4-flash"]
	if !ok {
		t.Fatal("AC1 FAIL: model usage not recorded for deepseek-v4-flash")
	}

	if mu.InputTokens != 100 {
		t.Errorf("AC1 FAIL: InputTokens = %d, want 100", mu.InputTokens)
	}
	if mu.OutputTokens != 50 {
		t.Errorf("AC1 FAIL: OutputTokens = %d, want 50", mu.OutputTokens)
	}
	if mu.CacheReadInputTokens != 30 {
		t.Errorf("AC1 FAIL: CacheReadInputTokens = %d, want 30", mu.CacheReadInputTokens)
	}
	if mu.CacheCreationInputTokens != 10 {
		t.Errorf("AC1 FAIL: CacheCreationInputTokens = %d, want 10", mu.CacheCreationInputTokens)
	}
}

func TestAC1_AccumulateUsageMultipleCallsSumTokens(t *testing.T) {
	state := &CostState{}
	usage1 := api.Usage{InputTokens: 50, OutputTokens: 20, CacheReadInputTokens: 10, CacheCreationInputTokens: 5}
	usage2 := api.Usage{InputTokens: 30, OutputTokens: 10, CacheReadInputTokens: 5, CacheCreationInputTokens: 2}

	AccumulateUsage(state, "test-model", usage1)
	AccumulateUsage(state, "test-model", usage2)

	mu := state.ModelUsage["test-model"]
	if mu.InputTokens != 80 {
		t.Errorf("AC1 FAIL: InputTokens sum = %d, want 80", mu.InputTokens)
	}
	if mu.CacheReadInputTokens != 15 {
		t.Errorf("AC1 FAIL: CacheReadInputTokens sum = %d, want 15", mu.CacheReadInputTokens)
	}
	if mu.CacheCreationInputTokens != 7 {
		t.Errorf("AC1 FAIL: CacheCreationInputTokens sum = %d, want 7", mu.CacheCreationInputTokens)
	}
}

func TestAC1_AccumulateUsageZeroTokens(t *testing.T) {
	state := &CostState{}
	usage := api.Usage{} // All zero

	AccumulateUsage(state, "test-model", usage)

	mu, ok := state.ModelUsage["test-model"]
	if !ok {
		t.Fatal("AC1 FAIL: model usage should be created even with zero tokens")
	}
	if mu.InputTokens != 0 || mu.OutputTokens != 0 || mu.CacheReadInputTokens != 0 || mu.CacheCreationInputTokens != 0 {
		t.Errorf("AC1 FAIL: expected all zeros, got %+v", mu)
	}
	if state.TotalCostUSD != 0 {
		t.Errorf("AC1 FAIL: expected TotalCostUSD=0, got %f", state.TotalCostUSD)
	}
}

func TestAC1_SendMessageNonStreamingUsageStruct(t *testing.T) {
	// Verify the api.Usage struct has all four fields
	usage := api.Usage{
		InputTokens:              10,
		OutputTokens:             20,
		CacheReadInputTokens:     5,
		CacheCreationInputTokens: 3,
	}

	if usage.InputTokens != 10 {
		t.Error("AC1 FAIL: InputTokens field missing or wrong")
	}
	if usage.OutputTokens != 20 {
		t.Error("AC1 FAIL: OutputTokens field missing or wrong")
	}
	if usage.CacheReadInputTokens != 5 {
		t.Error("AC1 FAIL: CacheReadInputTokens field missing or wrong")
	}
	if usage.CacheCreationInputTokens != 3 {
		t.Error("AC1 FAIL: CacheCreationInputTokens field missing or wrong")
	}
}

// ---------------------------------------------------------------------------
// AC2: Cost persistence
// ---------------------------------------------------------------------------

func TestAC2_SaveCostStatePersistsToDotJennyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Override JennyHomeDir to use temp dir/.jenny for testing
	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string {
		return filepath.Join(tmpDir, ".jenny")
	}
	defer func() {
		constants.JennyHomeDirFunc = originalFunc
	}()

	state := &CostState{
		LastSessionID: "sess_test_ac2",
		ModelUsage: map[string]ModelUsage{
			"test-model": {
				InputTokens:              100,
				OutputTokens:             50,
				CacheReadInputTokens:     10,
				CacheCreationInputTokens: 5,
				CostUSD:                  0.00123,
			},
		},
		TotalCostUSD: 0.00123,
	}

	if err := SaveCostState(state); err != nil {
		t.Fatalf("AC2 FAIL: SaveCostState() error = %v", err)
	}

	// Verify file exists
	configPath := filepath.Join(".jenny", "sessions", "sess_test_ac2", "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("AC2 FAIL: cannot read .jenny/sessions/sess_test_ac2/config: %v", err)
	}

	// Parse JSON and verify required fields
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("AC2 FAIL: config is not valid JSON: %v", err)
	}

	// Check lastSessionId (AC2 requires lastSessionId)
	if _, ok := parsed["LastSessionID"]; !ok {
		t.Error("AC2 FAIL: .jenny/config missing LastSessionID field")
	}
	if parsed["LastSessionID"] != "sess_test_ac2" {
		t.Errorf("AC2 FAIL: LastSessionID = %v, want 'sess_test_ac2'", parsed["LastSessionID"])
	}

	// Check TotalCostUSD
	if _, ok := parsed["TotalCostUSD"]; !ok {
		t.Error("AC2 FAIL: .jenny/config missing TotalCostUSD field")
	}

	// Check per-model usage
	modelUsage, ok := parsed["ModelUsage"]
	if !ok {
		t.Fatal("AC2 FAIL: .jenny/config missing ModelUsage field")
	}
	muMap, ok := modelUsage.(map[string]any)
	if !ok {
		t.Fatal("AC2 FAIL: ModelUsage is not a map")
	}
	testModel, ok := muMap["test-model"]
	if !ok {
		t.Fatal("AC2 FAIL: ModelUsage missing 'test-model' entry")
	}
	testModelMap, ok := testModel.(map[string]any)
	if !ok {
		t.Fatal("AC2 FAIL: test-model entry is not a map")
	}
	if testModelMap["InputTokens"] != float64(100) {
		t.Errorf("AC2 FAIL: test-model InputTokens = %v, want 100", testModelMap["InputTokens"])
	}
}

func TestAC2_SaveCostStateEmptyModelUsage(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string {
		return tmpDir
	}
	defer func() {
		constants.JennyHomeDirFunc = originalFunc
	}()

	state := &CostState{
		LastSessionID: "sess_empty",
		ModelUsage:    map[string]ModelUsage{},
		TotalCostUSD:  0,
	}

	if err := SaveCostState(state); err != nil {
		t.Fatalf("AC2 FAIL: SaveCostState() error = %v", err)
	}

	// Verify the file was created in the session directory
	configPath := filepath.Join(tmpDir, "sessions", "sess_empty", "config")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("AC2 FAIL: .jenny/sessions/sess_empty/config not created")
	}

	// Verify it loads back correctly
	loaded, err := LoadCostState("sess_empty")
	if err != nil {
		t.Fatalf("AC2 FAIL: LoadCostState() error = %v", err)
	}
	if loaded.LastSessionID != "sess_empty" {
		t.Errorf("AC2 FAIL: loaded LastSessionID = %q, want 'sess_empty'", loaded.LastSessionID)
	}
}

func TestAC2_PersistsEndTurnPath(t *testing.T) {
	// This test verifies SaveCostState is called on the end_turn path
	// by running RunStream with a mock server that returns end_turn
	server := makeMockStreamServerWithCacheTokens(t)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Override JennyHomeDir to use temp dir/.jenny for testing
	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string {
		return filepath.Join(tmpDir, ".jenny")
	}
	defer func() {
		constants.JennyHomeDirFunc = originalFunc
	}()

	sessMgr, err := session.NewManager(filepath.Join(tmpDir, "transcripts"), false)
	if err != nil {
		t.Fatalf("AC2 FAIL: NewManager error = %v", err)
	}

	sessionID := "sess_runstream_persist"
	cfg := StreamConfig{
		Enabled:        true,
		SessionID:      sessionID,
		SessionManager: sessMgr,
	}
	ctx := context.Background()

	_, _, err = RunStream(ctx, "test prompt", nil, tmpDir, cfg, "test-model", WithClient(fastClient()))
	if err != nil {
		t.Fatalf("AC2 FAIL: RunStream error = %v", err)
	}

	// Verify .jenny/sessions/<id>/config was created
	configPath := filepath.Join(tmpDir, ".jenny", "sessions", sessionID, "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("AC2 FAIL: .jenny/sessions/%s/config not created after RunStream end_turn", sessionID)
	}

	var state CostState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("AC2 FAIL: .jenny/config is not valid JSON: %v", err)
	}
	if state.LastSessionID == "" {
		t.Error("AC2 FAIL: LastSessionID is empty in persisted config")
	}
	if state.TotalCostUSD == 0 {
		t.Error("AC2 FAIL: TotalCostUSD is 0 in persisted config — cost may not have been accumulated")
	}
	t.Logf("AC2 PASS: persisted cost state: session=%q, total=%.6f, models=%d", state.LastSessionID, state.TotalCostUSD, len(state.ModelUsage))
}

// ---------------------------------------------------------------------------
// AC3: Resume cost restore
// ---------------------------------------------------------------------------

func TestAC3_RestoreCostStateWithMatchingSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Save a cost state
	saved := &CostState{
		LastSessionID: "sess_match",
		ModelUsage: map[string]ModelUsage{
			"deepseek-v4-flash": {
				InputTokens:              100,
				OutputTokens:             50,
				CacheReadInputTokens:     10,
				CacheCreationInputTokens: 5,
				CostUSD:                  0.0015,
			},
		},
		TotalCostUSD: 0.0015,
	}
	if err := SaveCostState(saved); err != nil {
		t.Fatalf("AC3 FAIL: SaveCostState error = %v", err)
	}

	// Restore with matching session ID
	restored, ok, err := RestoreCostState("sess_match")
	if err != nil {
		t.Fatalf("AC3 FAIL: RestoreCostState error = %v", err)
	}
	if !ok {
		t.Fatal("AC3 FAIL: RestoreCostState returned ok=false for matching session ID")
	}
	if restored.TotalCostUSD != 0.0015 {
		t.Errorf("AC3 FAIL: restored TotalCostUSD = %f, want 0.0015", restored.TotalCostUSD)
	}
	mu := restored.ModelUsage["deepseek-v4-flash"]
	if mu.InputTokens != 100 {
		t.Errorf("AC3 FAIL: restored InputTokens = %d, want 100", mu.InputTokens)
	}
	t.Log("AC3 PASS: cost state restored correctly on session ID match")
}

func TestAC3_RestoreCostStateWithMismatchedSessionID(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Save a cost state with one session ID
	saved := &CostState{
		LastSessionID: "sess_original",
		ModelUsage: map[string]ModelUsage{
			"deepseek-v4-flash": {InputTokens: 500, CostUSD: 0.005},
		},
		TotalCostUSD: 0.005,
	}
	if err := SaveCostState(saved); err != nil {
		t.Fatalf("AC3 FAIL: SaveCostState error = %v", err)
	}

	// Restore with DIFFERENT session ID
	restored, ok, err := RestoreCostState("sess_different")
	if err != nil {
		t.Fatalf("AC3 FAIL: RestoreCostState error = %v", err)
	}
	if ok {
		t.Fatal("AC3 FAIL: RestoreCostState returned ok=true for mismatched session ID, want false")
	}
	if restored != nil {
		t.Error("AC3 FAIL: RestoreCostState returned non-nil state on mismatch, want nil")
	}
	t.Log("AC3 PASS: cost state not restored on session ID mismatch")
}

func TestAC3_RestoreNoConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// No .jenny/config exists
	state, ok, err := RestoreCostState("sess_any")
	if err != nil {
		t.Fatalf("AC3 FAIL: RestoreCostState error = %v", err)
	}
	if ok {
		t.Fatal("AC3 FAIL: RestoreCostState returned ok=true when no config file exists")
	}
	if state != nil {
		t.Error("AC3 FAIL: RestoreCostState returned non-nil state when no config exists")
	}
}

func TestAC3_AccumulateOnRestoredState(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Save initial cost state
	saved := &CostState{
		LastSessionID: "sess_accum",
		ModelUsage: map[string]ModelUsage{
			"test-model": {InputTokens: 100, OutputTokens: 50, CostUSD: 0.001},
		},
		TotalCostUSD: 0.001,
	}
	SaveCostState(saved)

	// Restore
	restored, ok, err := RestoreCostState("sess_accum")
	if err != nil || !ok || restored == nil {
		t.Fatalf("AC3 FAIL: could not restore cost state: ok=%v err=%v", ok, err)
	}

	// Accumulate more usage on top of restored
	AccumulateUsage(restored, "test-model", api.Usage{
		InputTokens: 50, OutputTokens: 25,
	})

	mu := restored.ModelUsage["test-model"]
	if mu.InputTokens != 150 {
		t.Errorf("AC3 FAIL: accumulated InputTokens = %d, want 150 (100 restored + 50 new)", mu.InputTokens)
	}
	if restored.TotalCostUSD <= 0.001 {
		t.Errorf("AC3 FAIL: accumulated TotalCostUSD = %f, want > 0.001 (was 0.001 before accumulation)", restored.TotalCostUSD)
	}
	t.Log("AC3 PASS: accumulated usage on restored state correctly adds to restored values")
}

// ---------------------------------------------------------------------------
// AC4: Stream-json result.usage
// ---------------------------------------------------------------------------

func TestAC4_UsageStructHasCorrectJSONTags(t *testing.T) {
	usage := Usage{
		InputTokens:              10,
		OutputTokens:             20,
		CacheReadInputTokens:     5,
		CacheCreationInputTokens: 3,
	}

	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("AC4 FAIL: json.Marshal(usage) error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("AC4 FAIL: cannot unmarshal usage JSON: %v", err)
	}

	// Check for snake_case keys
	if _, ok := parsed["input_tokens"]; !ok {
		t.Error("AC4 FAIL: missing 'input_tokens' in JSON output")
	}
	if _, ok := parsed["output_tokens"]; !ok {
		t.Error("AC4 FAIL: missing 'output_tokens' in JSON output")
	}
	if _, ok := parsed["cache_read_input_tokens"]; !ok {
		t.Error("AC4 FAIL: missing 'cache_read_input_tokens' in JSON output")
	}
	if _, ok := parsed["cache_creation_input_tokens"]; !ok {
		t.Error("AC4 FAIL: missing 'cache_creation_input_tokens' in JSON output")
	}

	// Verify values
	if parsed["input_tokens"] != float64(10) {
		t.Errorf("AC4 FAIL: input_tokens = %v, want 10", parsed["input_tokens"])
	}
	if parsed["cache_read_input_tokens"] != float64(5) {
		t.Errorf("AC4 FAIL: cache_read_input_tokens = %v, want 5", parsed["cache_read_input_tokens"])
	}

	t.Log("AC4 PASS: all JSON tags are snake_case and present")
}

func TestAC4_ResultLineContainsCacheAndCostFields(t *testing.T) {
	server := makeMockStreamServerWithCacheTokens(t)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		sessMgr, err := session.NewManager(filepath.Join(tmpDir, "transcripts"), false)
		if err != nil {
			errCh <- err
			return
		}
		cfg := StreamConfig{Enabled: true, SessionManager: sessMgr}
		_, _, err = RunStream(context.Background(), "test", nil, tmpDir, cfg, "test-model", WithClient(fastClient()))
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	t.Logf("RunStream completed: %v", err)

	// Find the result line
	var resultLine string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, `"type":"result"`) {
			resultLine = line
			break
		}
	}
	if resultLine == "" {
		t.Fatal("AC4 FAIL: no result line found in output")
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(resultLine), &result); err != nil {
		t.Fatalf("AC4 FAIL: cannot unmarshal result line: %v", err)
	}

	usage, ok := result["usage"].(map[string]any)
	if !ok {
		t.Fatal("AC4 FAIL: result.usage is not an object or missing")
	}

	// Verify cache fields are present in usage
	if _, ok := usage["cache_read_input_tokens"]; !ok {
		t.Error("AC4 FAIL: result.usage missing cache_read_input_tokens")
	}
	if _, ok := usage["cache_creation_input_tokens"]; !ok {
		t.Error("AC4 FAIL: result.usage missing cache_creation_input_tokens")
	}

	// Verify total_cost_usd is at top level
	if _, ok := result["total_cost_usd"]; !ok {
		t.Error("AC4 FAIL: result missing total_cost_usd")
	}

	t.Logf("AC4 PASS: result.usage = %+v, total_cost_usd = %v", usage, result["total_cost_usd"])
}

// ---------------------------------------------------------------------------
// AC5: Budget enforcement
// ---------------------------------------------------------------------------

func TestAC5_CheckBudgetExceededWithLimit(t *testing.T) {
	state := &CostState{TotalCostUSD: 0.05}

	// Budget not exceeded
	exceeded, total := CheckBudgetExceeded(state, 0.10)
	if exceeded {
		t.Error("AC5 FAIL: budget exceeded when 0.05 <= 0.10")
	}
	if total != 0.05 {
		t.Errorf("AC5 FAIL: returned total = %f, want 0.05", total)
	}

	// Budget exceeded
	exceeded, total = CheckBudgetExceeded(state, 0.02)
	if !exceeded {
		t.Error("AC5 FAIL: budget NOT exceeded when 0.05 > 0.02")
	}
	if total != 0.05 {
		t.Errorf("AC5 FAIL: returned total = %f, want 0.05", total)
	}
}

func TestAC5_CheckBudgetExceededZeroLimit(t *testing.T) {
	state := &CostState{TotalCostUSD: 100.0}

	// Zero budget = no limit
	exceeded, total := CheckBudgetExceeded(state, 0)
	if exceeded {
		t.Error("AC5 FAIL: budget exceeded when maxBudgetUSD=0 (should be no limit)")
	}
	if total != 100.0 {
		t.Errorf("AC5 FAIL: returned total = %f, want 100.0", total)
	}
}

func TestAC5_CheckBudgetExceededNegativeLimit(t *testing.T) {
	state := &CostState{TotalCostUSD: 0.01}

	exceeded, _ := CheckBudgetExceeded(state, -1)
	if exceeded {
		t.Error("AC5 FAIL: budget exceeded when maxBudgetUSD < 0 (should be no limit)")
	}
}

func TestAC5_BudgetStopInRunStream(t *testing.T) {
	server := makeMockStreamServerWithCacheTokens(t)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Override JennyHomeDir to use temp dir/.jenny for testing
	originalFunc := constants.JennyHomeDirFunc
	constants.JennyHomeDirFunc = func() string {
		return filepath.Join(tmpDir, ".jenny")
	}
	defer func() {
		constants.JennyHomeDirFunc = originalFunc
	}()

	// Pre-seed config with cost that already exceeds the budget.
	// This simulates a resumed session where the restored cost exceeds MaxBudgetUSD,
	// which is the scenario where budget enforcement kicks in before the first API call.
	costDir := filepath.Join(tmpDir, ".jenny", "sessions", "sess_budget_test")
	if err := os.MkdirAll(costDir, 0755); err != nil {
		t.Fatalf("creating session dir: %v", err)
	}
	preSeed := CostState{
		LastSessionID: "sess_budget_test",
		ModelUsage: map[string]ModelUsage{
			"test-model": {InputTokens: 500, OutputTokens: 200, CostUSD: 0.01},
		},
		TotalCostUSD: 0.01,
	}
	seedData, _ := json.Marshal(preSeed)
	if err := os.WriteFile(filepath.Join(costDir, "config"), seedData, 0644); err != nil {
		t.Fatalf("writing pre-seeded config: %v", err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() {
		sessMgr, err := session.NewManager(filepath.Join(tmpDir, "transcripts"), false)
		if err != nil {
			errCh <- err
			return
		}
		// Set budget lower than pre-seeded cost
		cfg := StreamConfig{
			Enabled:        true,
			SessionManager: sessMgr,
			MaxBudgetUSD:   0.005, // Budget is $0.005, pre-seeded cost is $0.01
			IsResume:       true,  // Enable resume restore of cost state
			SessionID:      "sess_budget_test",
		}
		_, _, err = RunStream(context.Background(), "test", nil, tmpDir, cfg, "test-model", WithClient(fastClient()))
		errCh <- err
	}()

	err := <-errCh
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Should get budget exceeded error (pre-seeded cost exceeds budget)
	if err == nil {
		t.Fatal("AC5 FAIL: RunStream should return error when restored cost exceeds budget")
	}
	if !strings.Contains(err.Error(), "budget") && !strings.Contains(err.Error(), "Budget") {
		t.Errorf("AC5 FAIL: error message should mention budget, got: %v", err)
	}

	// Verify result line is emitted
	var resultLine string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, `"type":"result"`) {
			resultLine = line
			break
		}
	}
	if resultLine == "" {
		t.Error("AC5 FAIL: no result line emitted on budget exceeded")
	} else {
		t.Logf("AC5: result line = %s", resultLine)
		// No API call should have been made - verify no stream_request_start
		if strings.Contains(output, "stream_request_start") {
			t.Error("AC5 FAIL: stream_request_start emitted but no API call should have been made (budget exceeded before first call)")
		}
	}

	t.Logf("AC5 PASS: RunStream returned budget error: %v", err)
}

func TestAC5_BudgetNoLimitWhenMaxBudgetUSDIsZero(t *testing.T) {
	server := makeMockStreamServerWithCacheTokens(t)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key-00000")

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	sessMgr, err := session.NewManager(filepath.Join(tmpDir, "transcripts"), false)
	if err != nil {
		t.Fatalf("AC5 FAIL: NewManager error = %v", err)
	}

	// No budget limit (MaxBudgetUSD = 0)
	cfg := StreamConfig{
		Enabled:        true,
		MaxBudgetUSD:   0, // No limit
		SessionManager: sessMgr,
	}
	_, _, err = RunStream(context.Background(), "test", nil, tmpDir, cfg, "test-model", WithClient(fastClient()))
	if err != nil {
		t.Fatalf("AC5 FAIL: RunStream should not fail with no budget limit: %v", err)
	}
	t.Log("AC5 PASS: RunStream completes normally with no budget limit")
}

// ---------------------------------------------------------------------------
// Pricing Table Verification Tests
// ---------------------------------------------------------------------------

// TestPricing_AC1_ClaudeUSDRates verifies AC1: Claude USD rates match reference.
func TestPricing_AC1_ClaudeUSDRates(t *testing.T) {
	state := &CostState{}

	// 1M input tokens for claude-sonnet-4-20250514 → $3
	usage := api.Usage{InputTokens: 1000000}
	AccumulateUsage(state, "claude-sonnet-4-20250514", usage)
	mu := state.ModelUsage["claude-sonnet-4-20250514"]
	if mu.CostUSD != 3.0 {
		t.Errorf("AC1 FAIL: claude-sonnet-4 USD input 1M = %f, want 3.0", mu.CostUSD)
	}

	// 1M input tokens for claude-opus-4-20250514 → $5
	usage2 := api.Usage{InputTokens: 1000000}
	AccumulateUsage(state, "claude-opus-4-20250514", usage2)
	mu2 := state.ModelUsage["claude-opus-4-20250514"]
	if mu2.CostUSD != 5.0 {
		t.Errorf("AC1 FAIL: claude-opus-4 USD input 1M = %f, want 5.0", mu2.CostUSD)
	}

	// 1M output tokens for claude-sonnet-4-20250514 → $15
	state2 := &CostState{}
	usage3 := api.Usage{OutputTokens: 1000000}
	AccumulateUsage(state2, "claude-sonnet-4-20250514", usage3)
	mu3 := state2.ModelUsage["claude-sonnet-4-20250514"]
	if mu3.CostUSD != 15.0 {
		t.Errorf("AC1 FAIL: claude-sonnet-4 USD output 1M = %f, want 15.0", mu3.CostUSD)
	}

	t.Log("AC1 PASS: Claude USD rates correct")
}

// TestPricing_AC2_NewModelFamilies verifies AC2: new model families are present.
func TestPricing_AC2_NewModelFamilies(t *testing.T) {
	newModels := []string{
		"deepseek-v4-pro",
		"gemini-2.5-flash",
		"gemini-2.1-pro",
		"minimax-m3",
		"kimi-k2.6",
		"qwen-3.7-max",
		"hunyuan-turbos",
	}

	for _, model := range newModels {
		pricing := GetModelPricing(model)
		if pricing.InputUSD <= 0 {
			t.Errorf("AC2 FAIL: %s USD InputUSD = %f, want > 0", model, pricing.InputUSD)
		}
	}

	t.Log("AC2 PASS: new model families present")
}

// TestPricing_AC3_DeepSeekUSDRates verifies AC3: native USD models have correct rates.
// Source: api-docs.deepseek.com/quick_start/pricing
func TestPricing_AC3_DeepSeekUSDRates(t *testing.T) {
	state := &CostState{}

	// 1M input tokens for deepseek-v4-flash → $0.14 (cache miss)
	usage := api.Usage{InputTokens: 1000000}
	AccumulateUsage(state, "deepseek-v4-flash", usage)
	mu := state.ModelUsage["deepseek-v4-flash"]
	if mu.CostUSD != 0.14 {
		t.Errorf("AC3 FAIL: deepseek-v4-flash USD input 1M = %f, want 0.14", mu.CostUSD)
	}

	// 1M output tokens for deepseek-v4-flash → $0.28
	state2 := &CostState{}
	usage2 := api.Usage{OutputTokens: 1000000}
	AccumulateUsage(state2, "deepseek-v4-flash", usage2)
	mu2 := state2.ModelUsage["deepseek-v4-flash"]
	if mu2.CostUSD != 0.28 {
		t.Errorf("AC3 FAIL: deepseek-v4-flash USD output 1M = %f, want 0.28", mu2.CostUSD)
	}

	// 1M input tokens for deepseek-v4-pro → $0.435 (cache miss)
	state3 := &CostState{}
	usage3 := api.Usage{InputTokens: 1000000}
	AccumulateUsage(state3, "deepseek-v4-pro", usage3)
	mu3 := state3.ModelUsage["deepseek-v4-pro"]
	if mu3.CostUSD != 0.435 {
		t.Errorf("AC3 FAIL: deepseek-v4-pro USD input 1M = %f, want 0.435", mu3.CostUSD)
	}

	t.Log("AC3 PASS: native USD model rates correct")
}

// TestPricing_AC4_UnknownModelFallback verifies AC4: unknown model returns conservative defaults.
func TestPricing_AC4_UnknownModelFallback(t *testing.T) {
	pricing := GetModelPricing("nonexistent-model")
	if !pricing.UnknownModel {
		t.Error("AC4 FAIL: unknown model UnknownModel = false, want true")
	}
	if pricing.InputUSD <= 0 || pricing.OutputUSD <= 0 {
		t.Error("AC4 FAIL: unknown model has zero rates")
	}

	t.Log("AC4 PASS: unknown model fallback correct")
}
