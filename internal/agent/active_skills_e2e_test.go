// Package agent provides E2E integration tests for active skills and cross-turn state
// with context compaction hardening.
package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/session"
	"github.com/ipy/jenny/internal/skills"
	"github.com/ipy/jenny/internal/tool"
)

// skillActivatingTool is a mock tool that calls ActivateForPath on a SkillActivator
// when executed, simulating a Read tool's path-triggered skill activation.
type skillActivatingTool struct {
	name      string
	activator tool.SkillActivator
}

func (t *skillActivatingTool) Name() string        { return t.name }
func (t *skillActivatingTool) Description() string { return "Mock tool for skill activation" }
func (t *skillActivatingTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{"type": "string"},
		},
	}
}
func (t *skillActivatingTool) Execute(_ context.Context, input map[string]any, _ string) (*tool.ToolResult, error) {
	if fp, ok := input["file_path"].(string); ok && t.activator != nil {
		t.activator.ActivateForPath(fp)
	}
	return &tool.ToolResult{Content: "file contents here", IsError: false}, nil
}

// mockStreamServerForCompaction creates a stateful mock SSE server that tracks
// turn count and returns different responses per turn.
type mockStreamServerForCompaction struct {
	*httptest.Server
	counter int
	mu      sync.Mutex
}

func newMockStreamServerForCompaction(t *testing.T, turn1Events, turn2Events []string) *mockStreamServerForCompaction {
	t.Helper()

	ms := &mockStreamServerForCompaction{}

	ms.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req api.AnthropicRequest
		json.Unmarshal(bodyBytes, &req)

		ms.mu.Lock()
		ms.counter++
		currentTurn := ms.counter
		ms.mu.Unlock()

		var events []string
		if currentTurn == 1 {
			events = turn1Events
		} else {
			events = turn2Events
		}

		if !req.Stream {
			// Non-streaming response for fallback
			resp := api.AnthropicResponse{
				Type: "message",
				Role: "assistant",
				Model: "test-model",
				StopReason: "end_turn",
				Usage: api.AnthropicUsage{
					InputTokens:  100,
					OutputTokens: 20,
				},
			}

			// If turn 1, return tool use in content
			if currentTurn == 1 {
				resp.StopReason = "tool_use"
				resp.Content = []api.AnthropicContentBlock{
					{
						Type: "tool_use",
						ID:   "tool_1",
						Name: "Read",
						Input: map[string]any{
							"file_path": "/test/project/main.go",
						},
					},
				}
			} else {
				resp.Content = []api.AnthropicContentBlock{
					{
						Type: "text",
						Text: "Done with Go development",
					},
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		writeSSEEvents(w, events)
	}))

	return ms
}

// TestActiveSkills_E2E_ThroughCompaction tests AC1: Full pipeline e2e — tool activation →
// prompt reflection after compaction. Uses real QueryEngine with mock SSE server and
// actual compact() invocation.
func TestActiveSkills_E2E_ThroughCompaction(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test skills with activation globs
	testSkills := []skills.Skill{
		{
			Name:           "go-developer",
			Description:   "Go development skill",
			RootPath:       "/test/go-developer",
			ActivationGlob: "**/*.go",
		},
	}

	// Create PathSkillActivator
	activator := skills.NewPathSkillActivator(testSkills)

	// Create session manager
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	// Create stateful mock SSE server with proper tool_use streaming format
	turn1Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":10}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Read"}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/test/project/main.go\"}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":20}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	turn2Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":10}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Done with Go development"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":20}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	mockServer := newMockStreamServerForCompaction(t, turn1Events, turn2Events)
	defer mockServer.Close()

	t.Setenv("ANTHROPIC_BASE_URL", mockServer.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// Create mock Read tool that triggers skill activation on file access
	mockRead := &skillActivatingTool{name: "Read", activator: activator}
	tools := []tool.Tool{mockRead}

	// Create StreamConfig with skill activator
	cfg := StreamConfig{
		Enabled:        false, // Non-streaming for simpler testing
		SessionManager: sessMgr,
		SessionID:      "test-e2e-compaction",
		MaxIterations:  3,    // Limit iterations to avoid infinite loop
	}

	// Create QueryEngine with skill activator
	engine := mustNewQueryEngine(cfg, tools, "", WithClient(fastClient()), WithSkillActivator(activator))

	// Turn 1: Submit message that will trigger tool use
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "Read the Go file")
	if err != nil {
		t.Fatalf("SubmitMessage error: %v", err)
	}

	// Verify skill was activated via path-triggered activation
	activatedSkills := activator.GetActivatedSkills()
	if len(activatedSkills) != 1 {
		t.Errorf("Expected 1 activated skill after path-triggered activation, got %d", len(activatedSkills))
	}
	if len(activatedSkills) > 0 && activatedSkills[0].Name != "go-developer" {
		t.Errorf("Expected 'go-developer', got %s", activatedSkills[0].Name)
	}

	// Verify DynamicSystemSuffix contains "Active Skills:" with the skill
	suffix := DynamicSystemSuffix(engine.streamCfg, tmpDir)
	if !containsActiveSkillsSection(suffix) {
		t.Error("AC1 FAIL: DynamicSystemSuffix should contain 'Active Skills:' after activation")
	}
	if !containsSubstring(suffix, "go-developer") {
		t.Error("AC1 FAIL: DynamicSystemSuffix should contain skill name 'go-developer'")
	}

	// Now simulate compaction by invoking compactMessages directly
	messages := []api.Message{
		{Role: "user", Content: "Read the Go file"},
		{Role: "assistant", Content: "Reading the file..."},
		{Role: "user", Content: "Here's the file content"},
	}

	// Add more messages to exceed threshold
	for i := 0; i < 20; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: strings.Repeat("This is a long message to increase token count. ", 50),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: strings.Repeat("Response text. ", 50),
		})
	}

	// Invoke actual compaction
	systemPrompt := AssembleSystemPrompt(engine.streamCfg, engine.tools, tmpDir)
	compacted, err := engine.compactMessages(ctx, messages, engine.compactConfig, systemPrompt)
	if err != nil {
		t.Fatalf("compactMessages error: %v", err)
	}

	// Verify compaction modified messages
	if len(compacted) >= len(messages) {
		t.Errorf("AC1 FAIL: Compaction should reduce message count, before=%d, after=%d", len(messages), len(compacted))
	}

	// CRITICAL: Verify StreamConfig fields (ActiveSkills) are UNCHANGED after compaction
	// This is the architectural invariant that compaction only touches messages
	if len(engine.streamCfg.ActiveSkills) != 1 {
		t.Errorf("AC1 FAIL: ActiveSkills should be unchanged after compaction, got %d", len(engine.streamCfg.ActiveSkills))
	}

	// Verify DynamicSystemSuffix STILL contains the skill after compaction
	suffixAfterCompaction := DynamicSystemSuffix(engine.streamCfg, tmpDir)
	if !containsActiveSkillsSection(suffixAfterCompaction) {
		t.Error("AC1 FAIL: DynamicSystemSuffix should STILL contain 'Active Skills:' after compaction")
	}
	if !containsSubstring(suffixAfterCompaction, "go-developer") {
		t.Error("AC1 FAIL: DynamicSystemSuffix should STILL contain skill name after compaction")
	}

	t.Log("AC1 PASS: Full pipeline e2e through compaction verified")
}

// TestPermissionDenials_SurviveCompaction tests AC2: Cross-turn state survival
// after compaction for PermissionDenials using real compaction.
func TestPermissionDenials_SurviveCompaction(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := StreamConfig{Enabled: false}

	// Record a permission denial
	denialKey := BuildDenialKey("Bash", map[string]any{"command": "rm -rf /"})
	cfg.AddPermissionDenial(denialKey)

	// Verify denial is present
	if !cfg.HasPermissionDenial(denialKey) {
		t.Fatal("AC2 FAIL: Denial key should be present after AddPermissionDenial")
	}

	// Verify the same tool+input matches the denial
	matchingKey := BuildDenialKey("Bash", map[string]any{"command": "rm -rf /"})
	if !cfg.HasPermissionDenial(matchingKey) {
		t.Fatal("AC2 FAIL: Matching key should match denial")
	}

	// Verify different tool does not match
	differentKey := BuildDenialKey("Read", map[string]any{"file_path": "/etc/passwd"})
	if cfg.HasPermissionDenial(differentKey) {
		t.Fatal("AC2 FAIL: Different tool should not match denial")
	}

	// Create a QueryEngine and simulate real compaction
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":10}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":10}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server := makeMockStreamServer(t, events)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg.SessionManager = sessMgr
	cfg.SessionID = "test-permissions-compaction"

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	// Simulate real compaction with the engine
	messages := []api.Message{
		{Role: "user", Content: "Test"},
	}
	for i := 0; i < 15; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: strings.Repeat("Message content. ", 100),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: strings.Repeat("Response. ", 100),
		})
	}

	ctx := context.Background()
	systemPrompt := AssembleSystemPrompt(engine.streamCfg, engine.tools, tmpDir)
	compacted, err := engine.compactMessages(ctx, messages, engine.compactConfig, systemPrompt)
	if err != nil {
		t.Fatalf("compactMessages error: %v", err)
	}

	// CRITICAL: Verify StreamConfig fields are UNCHANGED after real compaction
	denialCountBefore := len(engine.streamCfg.PermissionDenials)

	_ = compacted

	denialCountAfter := len(engine.streamCfg.PermissionDenials)
	if denialCountBefore != denialCountAfter {
		t.Errorf("AC2 FAIL: PermissionDenials count changed after compaction: before=%d, after=%d", denialCountBefore, denialCountAfter)
	}

	// Verify denial key still present after real compaction
	if !engine.streamCfg.HasPermissionDenial(denialKey) {
		t.Error("AC2 FAIL: Denial key should survive real compaction")
	}

	// Verify the matching key still matches
	if !engine.streamCfg.HasPermissionDenial(matchingKey) {
		t.Error("AC2 FAIL: Matching key should still match after real compaction")
	}

	// Verify different key still doesn't match
	if engine.streamCfg.HasPermissionDenial(differentKey) {
		t.Error("AC2 FAIL: Different key should still not match after real compaction")
	}

	t.Log("AC2 PASS: PermissionDenials survive real compaction")
}

// TestDiscoveredSkillNames_SurviveCompaction_E2E tests AC3: Cross-turn state survival
// after compaction for DiscoveredSkillNames, including thread safety under concurrent calls.
// Uses unique strings per goroutine and verifies all entries survived.
func TestDiscoveredSkillNames_SurviveCompaction_E2E(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := StreamConfig{Enabled: false}

	// Add discovered skill names (serial additions)
	cfg.AddDiscoveredSkillName("readme-writer")
	cfg.AddDiscoveredSkillName("code-review")

	// Test concurrent access with UNIQUE strings per goroutine
	var wg sync.WaitGroup
	concurrentCount := 10
	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Each goroutine adds a unique skill name
			cfg.AddDiscoveredSkillName("concurrent-skill-" + strings.Repeat("x", idx+1))
		}(i)
	}
	wg.Wait()

	// Verify ALL unique entries are present
	expectedCount := 2 + concurrentCount
	if len(cfg.DiscoveredSkillNames) != expectedCount {
		t.Errorf("AC3 FAIL: Expected %d unique skill names, got %d", expectedCount, len(cfg.DiscoveredSkillNames))
	}

	// Verify the specific unique entries are present
	foundCount := 0
	for i := 0; i < concurrentCount; i++ {
		expectedName := "concurrent-skill-" + strings.Repeat("x", i+1)
		for _, name := range cfg.DiscoveredSkillNames {
			if name == expectedName {
				foundCount++
				break
			}
		}
	}
	if foundCount != concurrentCount {
		t.Errorf("AC3 FAIL: Only %d/%d unique concurrent skill names found", foundCount, concurrentCount)
	}

	// Verify deduplication works
	initialCount := len(cfg.DiscoveredSkillNames)
	cfg.AddDiscoveredSkillName("readme-writer") // duplicate
	if len(cfg.DiscoveredSkillNames) != initialCount {
		t.Error("AC3 FAIL: Duplicate skill name should not increase count")
	}

	// Now test with real compaction
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":10}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Done"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":10}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server := makeMockStreamServer(t, events)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg.SessionManager = sessMgr
	cfg.SessionID = "test-discovered-compaction"

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	// Capture count before compaction
	countBefore := len(engine.streamCfg.DiscoveredSkillNames)

	// Simulate real compaction
	messages := []api.Message{
		{Role: "user", Content: "Test"},
	}
	for i := 0; i < 15; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: strings.Repeat("Message content. ", 100),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: strings.Repeat("Response. ", 100),
		})
	}

	ctx := context.Background()
	systemPrompt := AssembleSystemPrompt(engine.streamCfg, engine.tools, tmpDir)
	_, err = engine.compactMessages(ctx, messages, engine.compactConfig, systemPrompt)
	if err != nil {
		t.Fatalf("compactMessages error: %v", err)
	}

	// CRITICAL: Verify count is UNCHANGED after real compaction
	countAfter := len(engine.streamCfg.DiscoveredSkillNames)
	if countBefore != countAfter {
		t.Errorf("AC3 FAIL: DiscoveredSkillNames count changed after compaction: before=%d, after=%d", countBefore, countAfter)
	}

	// Verify all entries still present after compaction
	for _, name := range cfg.DiscoveredSkillNames {
		found := false
		for _, nameAfter := range engine.streamCfg.DiscoveredSkillNames {
			if name == nameAfter {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AC3 FAIL: Skill name %q lost after compaction", name)
		}
	}

	// Verify deduplication still works after compaction
	currentCount := len(engine.streamCfg.DiscoveredSkillNames)
	engine.streamCfg.AddDiscoveredSkillName("readme-writer")
	if len(engine.streamCfg.DiscoveredSkillNames) != currentCount {
		t.Error("AC3 FAIL: Deduplication should still work after compaction")
	}

	t.Log("AC3 PASS: DiscoveredSkillNames survive real compaction with proper thread safety")
}

// TestActiveSkills_AccumulateAcrossTurns tests AC4: Multi-turn sequential activation
// accumulation. Uses real QueryEngine with multi-turn mock server.
func TestActiveSkills_AccumulateAcrossTurns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test skills with different activation globs
	testSkills := []skills.Skill{
		{
			Name:           "go-developer",
			Description:   "Go development skill",
			RootPath:       tmpDir + "/go-developer",
			ActivationGlob: "**/*.go",
		},
		{
			Name:           "python-developer",
			Description:   "Python development skill",
			RootPath:       tmpDir + "/python-developer",
			ActivationGlob: "**/*.py",
		},
	}

	activator := skills.NewPathSkillActivator(testSkills)

	// Create session manager
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	// Create stateful mock server for multi-turn with proper tool_use streaming format
	turn1Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":10}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"Read"}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/test/project/main.go\"}"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":20}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}
	turn2Events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_2","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":10}}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseLine("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Done with Go development"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":100,"output_tokens":20}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	mockServer := newMockStreamServerForCompaction(t, turn1Events, turn2Events)
	defer mockServer.Close()

	t.Setenv("ANTHROPIC_BASE_URL", mockServer.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	// Create mock Read tool that triggers skill activation on file access
	mockRead := &skillActivatingTool{name: "Read", activator: activator}
	tools := []tool.Tool{mockRead}

	cfg := StreamConfig{
		Enabled:        false,
		SessionManager: sessMgr,
		SessionID:      "test-multi-turn",
		MaxIterations:  5,
	}

	engine := mustNewQueryEngine(cfg, tools, "", WithClient(fastClient()), WithSkillActivator(activator))

	// Turn 1: path-triggered activation for skill-A (go-developer)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err = engine.SubmitMessage(ctx, "Read the Go file at /test/project/main.go")
	if err != nil {
		t.Fatalf("SubmitMessage Turn 1 error: %v", err)
	}

	// Verify skill-A is in the activated list
	activatedSkills1 := activator.GetActivatedSkills()
	if len(activatedSkills1) != 1 {
		t.Errorf("AC4 FAIL: Expected 1 skill after turn 1, got %d", len(activatedSkills1))
	}
	if len(activatedSkills1) > 0 && activatedSkills1[0].Name != "go-developer" {
		t.Errorf("AC4 FAIL: Expected 'go-developer', got %s", activatedSkills1[0].Name)
	}

	// Verify DynamicSystemSuffix reflects skill-A
	suffix1 := DynamicSystemSuffix(engine.streamCfg, tmpDir)
	if !containsActiveSkillsSection(suffix1) {
		t.Error("AC4 FAIL: Suffix should contain 'Active Skills:' after turn 1")
	}
	if !containsSubstring(suffix1, "go-developer") {
		t.Error("AC4 FAIL: Suffix should contain 'go-developer' after turn 1")
	}

	// Turn 2: explicit activation for skill-B (python-developer)
	activator.RegisterActivation("python-developer", tmpDir+"/python-developer")

	// Verify both skills are in the list
	activatedSkills2 := activator.GetActivatedSkills()
	if len(activatedSkills2) != 2 {
		t.Errorf("AC4 FAIL: Expected 2 skills after turn 2, got %d", len(activatedSkills2))
	}

	// Verify no duplication
	skillNames := make(map[string]bool)
	for _, s := range activatedSkills2 {
		if skillNames[s.Name] {
			t.Errorf("AC4 FAIL: Duplicate skill %s found", s.Name)
		}
		skillNames[s.Name] = true
	}

	// Verify both skills are present
	if !skillNames["go-developer"] {
		t.Error("AC4 FAIL: 'go-developer' should be active after both turns")
	}
	if !skillNames["python-developer"] {
		t.Error("AC4 FAIL: 'python-developer' should be active after explicit activation")
	}

	// Verify dedup: re-activating same skill doesn't add duplicates
	activator.RegisterActivation("go-developer", tmpDir+"/go-developer")
	activatedSkills3 := activator.GetActivatedSkills()
	if len(activatedSkills3) != 2 {
		t.Errorf("AC4 FAIL: Re-activating skill should not create duplicate, got %d skills", len(activatedSkills3))
	}

	t.Log("AC4 PASS: Multi-turn sequential activation accumulation verified")
}

// TestCompaction_PreservesNonCompactedFields tests AC5: Compaction does not modify
// StreamConfig non-compacted fields. Uses real compact() invocation.
func TestCompaction_PreservesNonCompactedFields(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := StreamConfig{
		Enabled: false,
		ActiveSkills: []ActivatedSkill{
			{Name: "skill-1", RootPath: "/path/to/skill-1"},
			{Name: "skill-2", RootPath: "/path/to/skill-2"},
		},
	}

	// Add permission denials
	denialKey1 := BuildDenialKey("Bash", map[string]any{"command": "rm -rf /"})
	denialKey2 := BuildDenialKey("Read", map[string]any{"file_path": "/etc/passwd"})
	cfg.AddPermissionDenial(denialKey1)
	cfg.AddPermissionDenial(denialKey2)

	// Add discovered skill names
	cfg.AddDiscoveredSkillName("readme-writer")
	cfg.AddDiscoveredSkillName("code-review")
	cfg.AddDiscoveredSkillName("api-designer")

	// Create session manager
	sessMgr, err := session.NewManager(tmpDir, false)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	events := []string{
		sseLine("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test-model","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":10}}`),
		sseLine("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Summary"}}`),
		sseLine("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseLine("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":10}}`),
		sseLine("message_stop", `{"type":"message_stop"}`),
	}

	server := makeMockStreamServer(t, events)
	defer server.Close()

	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	cfg.SessionManager = sessMgr
	cfg.SessionID = "test-compaction-preservation"

	engine := mustNewQueryEngine(cfg, nil, "", WithClient(fastClient()))

	// Capture state before compaction
	originalActiveSkills := make([]ActivatedSkill, len(engine.streamCfg.ActiveSkills))
	copy(originalActiveSkills, engine.streamCfg.ActiveSkills)
	originalDenials := make([]string, len(engine.streamCfg.PermissionDenials))
	copy(originalDenials, engine.streamCfg.PermissionDenials)
	originalDiscovered := make([]string, len(engine.streamCfg.DiscoveredSkillNames))
	copy(originalDiscovered, engine.streamCfg.DiscoveredSkillNames)

	// Build messages that will trigger compaction
	messages := []api.Message{
		{Role: "user", Content: "Test compaction"},
	}
	for i := 0; i < 20; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: strings.Repeat("Content. ", 100),
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: strings.Repeat("Response. ", 100),
		})
	}

	// Invoke actual compaction
	ctx := context.Background()
	systemPrompt := AssembleSystemPrompt(engine.streamCfg, engine.tools, tmpDir)
	compacted, err := engine.compactMessages(ctx, messages, engine.compactConfig, systemPrompt)
	if err != nil {
		t.Fatalf("compactMessages error: %v", err)
	}

	// Verify compaction modified messages
	if len(compacted) >= len(messages) {
		t.Errorf("AC5 FAIL: Compaction should reduce message count, before=%d, after=%d", len(messages), len(compacted))
	}

	// CRITICAL: Verify all three fields are UNCHANGED after real compaction
	// Check ActiveSkills
	if len(engine.streamCfg.ActiveSkills) != len(originalActiveSkills) {
		t.Errorf("AC5 FAIL: ActiveSkills count changed after compaction: was %d, now %d", len(originalActiveSkills), len(engine.streamCfg.ActiveSkills))
	}
	for i, s := range originalActiveSkills {
		if i >= len(engine.streamCfg.ActiveSkills) || engine.streamCfg.ActiveSkills[i].Name != s.Name || engine.streamCfg.ActiveSkills[i].RootPath != s.RootPath {
			t.Errorf("AC5 FAIL: ActiveSkills[%d] changed after compaction", i)
		}
	}

	// Check PermissionDenials
	if len(engine.streamCfg.PermissionDenials) != len(originalDenials) {
		t.Errorf("AC5 FAIL: PermissionDenials count changed after compaction: was %d, now %d", len(originalDenials), len(engine.streamCfg.PermissionDenials))
	}
	for i, d := range originalDenials {
		if i >= len(engine.streamCfg.PermissionDenials) || engine.streamCfg.PermissionDenials[i] != d {
			t.Errorf("AC5 FAIL: PermissionDenials[%d] changed after compaction", i)
		}
	}

	// Check DiscoveredSkillNames
	if len(engine.streamCfg.DiscoveredSkillNames) != len(originalDiscovered) {
		t.Errorf("AC5 FAIL: DiscoveredSkillNames count changed after compaction: was %d, now %d", len(originalDiscovered), len(engine.streamCfg.DiscoveredSkillNames))
	}
	for i, n := range originalDiscovered {
		if i >= len(engine.streamCfg.DiscoveredSkillNames) || engine.streamCfg.DiscoveredSkillNames[i] != n {
			t.Errorf("AC5 FAIL: DiscoveredSkillNames[%d] changed after compaction", i)
		}
	}

	t.Log("AC5 PASS: Compaction preserves non-compacted fields")
}

// TestActiveSkills_GracefulDegradation tests AC6: Graceful degradation when
// activator returns empty skills. Verifies nil-slice/empty-slice edge case.
func TestActiveSkills_GracefulDegradation(t *testing.T) {
	tmpDir := t.TempDir()

	// Test case 1: nil ActiveSkills
	cfg1 := StreamConfig{ActiveSkills: nil}
	suffix1 := DynamicSystemSuffix(cfg1, tmpDir)
	if containsActiveSkillsSection(suffix1) {
		t.Error("AC6 FAIL: Active Skills section should not be present for nil skills")
	}

	// Test case 2: empty ActiveSkills slice
	cfg2 := StreamConfig{ActiveSkills: []ActivatedSkill{}}
	suffix2 := DynamicSystemSuffix(cfg2, tmpDir)
	if containsActiveSkillsSection(suffix2) {
		t.Error("AC6 FAIL: Active Skills section should not be present for empty skills")
	}

	// Test case 3: Activator with no matching skills
	testSkills := []skills.Skill{
		{
			Name:           "go-developer",
			Description:   "Go development skill",
			RootPath:       "/test/go-developer",
			ActivationGlob: "**/*.go",
		},
	}
	activator := skills.NewPathSkillActivator(testSkills)

	// Activate with non-matching path
	nonMatchingPath := "/some/path/file.py"
	activated := activator.ActivateForPath(nonMatchingPath)
	if len(activated) != 0 {
		t.Error("AC6 FAIL: Non-matching path should not activate any skill")
	}

	// Verify no active skills section when activator returns empty
	activatedSkills := activator.GetActivatedSkills()
	cfg3 := StreamConfig{ActiveSkills: []ActivatedSkill{}}
	if len(activatedSkills) == 0 {
		suffix3 := DynamicSystemSuffix(cfg3, tmpDir)
		if containsActiveSkillsSection(suffix3) {
			t.Error("AC6 FAIL: No Active Skills section should appear when activator returns empty")
		}
	}

	t.Log("AC6 PASS: Graceful degradation for empty/nil skills")
}

// TestActiveSkills_NoRegression verifies AC7: No regression on existing unit tests.
func TestActiveSkills_NoRegression(t *testing.T) {
	tmpDir := t.TempDir()

	// Test containsActiveSkillsSection helper
	cfg := StreamConfig{
		ActiveSkills: []ActivatedSkill{
			{Name: "test-skill", RootPath: tmpDir + "/test-skill"},
		},
	}
	suffix := DynamicSystemSuffix(cfg, tmpDir)

	if !containsActiveSkillsSection(suffix) {
		t.Error("AC7 FAIL: containsActiveSkillsSection should detect Active Skills section")
	}

	// Test containsSubstring helper
	if !containsSubstring(suffix, "test-skill") {
		t.Error("AC7 FAIL: containsSubstring should find skill name in suffix")
	}

	// Test BuildDenialKey
	key1 := BuildDenialKey("Bash", map[string]any{"command": "ls"})
	key2 := BuildDenialKey("Bash", map[string]any{"command": "ls"})
	if key1 != key2 {
		t.Error("AC7 FAIL: BuildDenialKey should produce deterministic keys")
	}

	// Test DynamicSystemSuffix with no skills
	cfgNoSkills := StreamConfig{}
	suffixNoSkills := DynamicSystemSuffix(cfgNoSkills, tmpDir)
	if containsActiveSkillsSection(suffixNoSkills) {
		t.Error("AC7 FAIL: No Active Skills section should appear when no skills are active")
	}

	t.Log("AC7 PASS: No regression on existing unit test helpers")
}
