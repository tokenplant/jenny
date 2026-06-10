package harness

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// CompareResult holds the result of comparing actual vs expected behavior.
type CompareResult struct {
	Pass bool
	Diff []DiffDetail
}

// Compare compares actual captured output against expected behavior.
func Compare(tc *TestCase, actual *CapturedOutput) CompareResult {
	var diffs []DiffDetail

	// Exit code check
	if actual.ExitCode != tc.Expected.ExitCode {
		diffs = append(diffs, DiffDetail{
			Path:     "exitCode",
			Expected: tc.Expected.ExitCode,
			Actual:   actual.ExitCode,
			Message:  "exit code mismatch",
		})
	}

	// Stdout checks
	if tc.Expected.Stdout != nil {
		diffs = append(diffs, compareOutput("stdout", tc.Expected.Stdout, actual.Stdout)...)
	}

	// Stderr checks
	if tc.Expected.Stderr != nil {
		diffs = append(diffs, compareOutput("stderr", tc.Expected.Stderr, actual.Stderr)...)
	}

	// API request checks
	if len(tc.Expected.APIRequests) > 0 {
		diffs = append(diffs, compareAPIRequests(tc.Expected.APIRequests, actual.Requests)...)
	}

	// Stream-JSON checks
	if tc.Expected.StreamJSON != nil {
		diffs = append(diffs, compareStreamJSON(tc.Expected.StreamJSON, actual.Stdout)...)
	}

	return CompareResult{
		Pass: len(diffs) == 0,
		Diff: diffs,
	}
}

func compareOutput(prefix string, exp *StdoutExpectation, actual string) []DiffDetail {
	var diffs []DiffDetail

	if exp.Equals != "" && actual != exp.Equals {
		diffs = append(diffs, DiffDetail{
			Path:     prefix,
			Expected: exp.Equals,
			Actual:   actual,
			Message:  prefix + " does not match exactly",
		})
	}

	if exp.IsEmpty && actual != "" {
		diffs = append(diffs, DiffDetail{
			Path:     prefix,
			Expected: "",
			Actual:   actual,
			Message:  "expected empty " + prefix,
		})
	}

	if len(exp.Contains) > 0 {
		found := false
		for _, substr := range exp.Contains {
			if strings.Contains(actual, substr) {
				found = true
				break
			}
		}
		if !found {
			diffs = append(diffs, DiffDetail{
				Path:     prefix,
				Expected: "contains one of: " + strings.Join(exp.Contains, ", "),
				Actual:   truncate(actual, 200),
				Message:  prefix + " does not contain any expected substring",
			})
		}
	}

	if len(exp.NotContains) > 0 {
		for _, substr := range exp.NotContains {
			if strings.Contains(actual, substr) {
				diffs = append(diffs, DiffDetail{
					Path:     prefix,
					Expected: "not contains: " + substr,
					Actual:   truncate(actual, 200),
					Message:  prefix + " contains unexpected substring",
				})
			}
		}
	}

	if len(exp.Matches) > 0 {
		for _, pattern := range exp.Matches {
			re, err := regexp.Compile(pattern)
			if err != nil {
				diffs = append(diffs, DiffDetail{
					Path:    prefix,
					Message: "invalid regex pattern: " + pattern,
				})
				continue
			}
			found := false
			for _, line := range strings.Split(actual, "\n") {
				if re.MatchString(line) {
					found = true
					break
				}
			}
			if !found {
				diffs = append(diffs, DiffDetail{
					Path:     prefix,
					Expected: "matches regex: " + pattern,
					Actual:   truncate(actual, 200),
					Message:  prefix + " does not match expected regex",
				})
			}
		}
	}

	if exp.Length != nil {
		actualLen := len(actual)
		if exp.Length.Min > 0 && actualLen < exp.Length.Min {
			diffs = append(diffs, DiffDetail{
				Path:     prefix,
				Expected: fmt.Sprintf("length >= %d", exp.Length.Min),
				Actual:   fmt.Sprintf("%d", actualLen),
				Message:  prefix + " too short",
			})
		}
		if exp.Length.Max > 0 && actualLen > exp.Length.Max {
			diffs = append(diffs, DiffDetail{
				Path:     prefix,
				Expected: fmt.Sprintf("length <= %d", exp.Length.Max),
				Actual:   fmt.Sprintf("%d", actualLen),
				Message:  prefix + " too long",
			})
		}
		if exp.Length.Exact > 0 && actualLen != exp.Length.Exact {
			diffs = append(diffs, DiffDetail{
				Path:     prefix,
				Expected: fmt.Sprintf("length == %d", exp.Length.Exact),
				Actual:   fmt.Sprintf("%d", actualLen),
				Message:  prefix + " length mismatch",
			})
		}
	}

	return diffs
}

func compareAPIRequests(expected []APIRequestExpectation, actual []RecordedRequest) []DiffDetail {
	var diffs []DiffDetail

	for _, exp := range expected {
		idx := exp.Index
		if idx < 0 {
			idx = 0
		}
		if idx >= len(actual) {
			diffs = append(diffs, DiffDetail{
				Path:     fmt.Sprintf("apiRequests[%d]", idx),
				Expected: "request exists",
				Actual:   fmt.Sprintf("only %d requests recorded", len(actual)),
				Message:  "API request not found",
			})
			continue
		}

		body := actual[idx].Body
		prefix := fmt.Sprintf("apiRequests[%d]", idx)

		if exp.Model != "" {
			model, _ := body["model"].(string)
			if model != exp.Model {
				re, err := regexp.Compile(exp.Model)
				if err != nil || !re.MatchString(model) {
					diffs = append(diffs, DiffDetail{
						Path:     prefix + ".model",
						Expected: exp.Model,
						Actual:   model,
						Message:  "model mismatch",
					})
				}
			}
		}

		if exp.MaxTokens > 0 {
			mt, _ := body["max_tokens"].(float64)
			if int(mt) != exp.MaxTokens {
				diffs = append(diffs, DiffDetail{
					Path:     prefix + ".max_tokens",
					Expected: exp.MaxTokens,
					Actual:   int(mt),
					Message:  "max_tokens mismatch",
				})
			}
		}

		if exp.HasSystemPrompt {
			sys := body["system"]
			if sys == nil {
				diffs = append(diffs, DiffDetail{
					Path:     prefix + ".system",
					Expected: "non-empty system prompt",
					Actual:   nil,
					Message:  "system prompt missing",
				})
			}
		}

		if exp.System != nil {
			sysText := extractSystemText(body)
			for _, substr := range exp.System.Contains {
				if !strings.Contains(sysText, substr) {
					diffs = append(diffs, DiffDetail{
						Path:     prefix + ".system",
						Expected: "contains: " + substr,
						Actual:   truncate(sysText, 200),
						Message:  "system prompt does not contain expected substring",
					})
				}
			}
			for _, substr := range exp.System.NotContains {
				if strings.Contains(sysText, substr) {
					diffs = append(diffs, DiffDetail{
						Path:     prefix + ".system",
						Expected: "not contains: " + substr,
						Actual:   truncate(sysText, 200),
						Message:  "system prompt contains unexpected substring",
					})
				}
			}
		}

		if exp.Tools != nil {
			diffs = append(diffs, compareTools(prefix, exp.Tools, body)...)
		}

		for _, key := range exp.HasField {
			if _, ok := body[key]; !ok {
				diffs = append(diffs, DiffDetail{
					Path:     prefix + "." + key,
					Expected: "field exists",
					Actual:   "missing",
					Message:  "expected field not found in request body",
				})
			}
		}

		for key, val := range exp.FieldEquals {
			actual := body[key]
			if !jsonEqual(actual, val) {
				diffs = append(diffs, DiffDetail{
					Path:     prefix + "." + key,
					Expected: val,
					Actual:   actual,
					Message:  "field value mismatch",
				})
			}
		}
	}

	return diffs
}

func compareTools(prefix string, exp *ToolsExpectation, body map[string]any) []DiffDetail {
	var diffs []DiffDetail

	toolsRaw, ok := body["tools"].([]any)
	if !ok {
		if exp.MinCount > 0 || len(exp.HasTool) > 0 {
			diffs = append(diffs, DiffDetail{
				Path:    prefix + ".tools",
				Message: "tools field missing or not an array",
			})
		}
		return diffs
	}

	if exp.MinCount > 0 && len(toolsRaw) < exp.MinCount {
		diffs = append(diffs, DiffDetail{
			Path:     prefix + ".tools.count",
			Expected: fmt.Sprintf(">= %d", exp.MinCount),
			Actual:   len(toolsRaw),
			Message:  "too few tools",
		})
	}

	toolNames := make(map[string]bool)
	for _, raw := range toolsRaw {
		if t, ok := raw.(map[string]any); ok {
			if name, ok := t["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}

	for _, name := range exp.HasTool {
		if !toolNames[name] {
			diffs = append(diffs, DiffDetail{
				Path:     prefix + ".tools",
				Expected: "has tool: " + name,
				Actual:   fmt.Sprintf("tools: %v", mapKeys(toolNames)),
				Message:  "expected tool not found",
			})
		}
	}

	for _, name := range exp.NotHasTool {
		if toolNames[name] {
			diffs = append(diffs, DiffDetail{
				Path:    prefix + ".tools",
				Message: "unexpected tool found: " + name,
			})
		}
	}

	if len(exp.EachHasFields) > 0 {
		for i, raw := range toolsRaw {
			t, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range exp.EachHasFields {
				if _, ok := t[field]; !ok {
					name, _ := t["name"].(string)
					diffs = append(diffs, DiffDetail{
						Path:     fmt.Sprintf("%s.tools[%d].%s", prefix, i, field),
						Expected: "field exists",
						Actual:   fmt.Sprintf("missing on tool %q", name),
						Message:  "tool missing required field",
					})
				}
			}
		}
	}

	return diffs
}

func compareStreamJSON(exp *StreamJSONExpectation, stdout string) []DiffDetail {
	var diffs []DiffDetail

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	var events []map[string]any
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			if exp.AllLinesValidJSON {
				diffs = append(diffs, DiffDetail{
					Path:     "streamJSON.validJSON",
					Expected: "valid JSON",
					Actual:   truncate(line, 100),
					Message:  "non-JSON line in stream output: " + err.Error(),
				})
			}
			continue
		}
		events = append(events, m)
	}

	if exp.AllLinesValidJSON {
		nonEmpty := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				nonEmpty++
			}
		}
		if len(events) != nonEmpty && len(diffs) == 0 {
			diffs = append(diffs, DiffDetail{
				Path:     "streamJSON.validJSON",
				Expected: fmt.Sprintf("%d valid JSON lines", nonEmpty),
				Actual:   fmt.Sprintf("%d parsed events", len(events)),
				Message:  "some lines failed JSON parse",
			})
		}
	}

	if exp.EventCount != nil {
		n := len(events)
		if exp.EventCount.Min > 0 && n < exp.EventCount.Min {
			diffs = append(diffs, DiffDetail{
				Path:     "streamJSON.eventCount",
				Expected: fmt.Sprintf(">= %d", exp.EventCount.Min),
				Actual:   n,
				Message:  "too few events",
			})
		}
		if exp.EventCount.Max > 0 && n > exp.EventCount.Max {
			diffs = append(diffs, DiffDetail{
				Path:     "streamJSON.eventCount",
				Expected: fmt.Sprintf("<= %d", exp.EventCount.Max),
				Actual:   n,
				Message:  "too many events",
			})
		}
	}

	if len(events) > 0 && exp.FirstEvent != nil {
		diffs = append(diffs, compareEvent("streamJSON.first", exp.FirstEvent, events[0])...)
	}

	if len(events) > 0 && exp.LastEvent != nil {
		diffs = append(diffs, compareEvent("streamJSON.last", exp.LastEvent, events[len(events)-1])...)
	}

	if exp.HasEventTypes != nil {
		typeSet := make(map[string]bool)
		for _, ev := range events {
			if t, ok := ev["type"].(string); ok {
				typeSet[t] = true
			}
		}
		for _, t := range exp.HasEventTypes {
			if !typeSet[t] {
				diffs = append(diffs, DiffDetail{
					Path:     "streamJSON.hasEventType",
					Expected: t,
					Actual:   fmt.Sprintf("types found: %v", mapKeys(typeSet)),
					Message:  "expected event type not found",
				})
			}
		}
	}

	if exp.SessionIDConsistent && len(events) > 1 {
		firstSID, _ := events[0]["session_id"].(string)
		for i, ev := range events[1:] {
			sid, _ := ev["session_id"].(string)
			if sid != firstSID && sid != "" && firstSID != "" {
				diffs = append(diffs, DiffDetail{
					Path:     fmt.Sprintf("streamJSON.events[%d].session_id", i+1),
					Expected: firstSID,
					Actual:   sid,
					Message:  "session_id inconsistent across events",
				})
				break
			}
		}
	}

	if exp.UUIDsUnique && len(events) > 0 {
		seen := make(map[string]int)
		for i, ev := range events {
			uid, _ := ev["uuid"].(string)
			if uid == "" {
				continue
			}
			if prev, ok := seen[uid]; ok {
				diffs = append(diffs, DiffDetail{
					Path:     fmt.Sprintf("streamJSON.events[%d].uuid", i),
					Expected: "unique uuid",
					Actual:   fmt.Sprintf("duplicate of event %d", prev),
					Message:  "uuid not unique",
				})
				break
			}
			seen[uid] = i
		}
	}

	for _, ia := range exp.EventAssertions {
		var target map[string]any
		var targetIdx int

		if ia.Index >= 0 {
			if ia.Index < len(events) {
				target = events[ia.Index]
				targetIdx = ia.Index
			} else {
				diffs = append(diffs, DiffDetail{
					Path:    fmt.Sprintf("streamJSON.events[%d]", ia.Index),
					Message: fmt.Sprintf("event index %d out of range (%d events)", ia.Index, len(events)),
				})
				continue
			}
		} else {
			for i, ev := range events {
				t, _ := ev["type"].(string)
				if t != ia.TypeFilter {
					continue
				}
				if ia.SubtypeFilter != "" {
					st, _ := ev["subtype"].(string)
					if st != ia.SubtypeFilter {
						continue
					}
				}
				target = ev
				targetIdx = i
				break
			}
			if target == nil {
				filter := ia.TypeFilter
				if ia.SubtypeFilter != "" {
					filter += "/" + ia.SubtypeFilter
				}
				diffs = append(diffs, DiffDetail{
					Path:    "streamJSON.events",
					Message: "no event matching type filter: " + filter,
				})
				continue
			}
		}

		diffs = append(diffs, compareEvent(
			fmt.Sprintf("streamJSON.events[%d]", targetIdx),
			&ia.Expect, target,
		)...)
	}

	return diffs
}

func compareEvent(prefix string, exp *EventExpectation, actual map[string]any) []DiffDetail {
	var diffs []DiffDetail

	if exp.Type != "" {
		t, _ := actual["type"].(string)
		if t != exp.Type {
			diffs = append(diffs, DiffDetail{
				Path:     prefix + ".type",
				Expected: exp.Type,
				Actual:   t,
				Message:  "event type mismatch",
			})
		}
	}

	if exp.Subtype != "" {
		st, _ := actual["subtype"].(string)
		if st != exp.Subtype {
			diffs = append(diffs, DiffDetail{
				Path:     prefix + ".subtype",
				Expected: exp.Subtype,
				Actual:   st,
				Message:  "event subtype mismatch",
			})
		}
	}

	for _, field := range exp.HasFields {
		if _, ok := actual[field]; !ok {
			diffs = append(diffs, DiffDetail{
				Path:    prefix + "." + field,
				Message: "expected field missing",
			})
		}
	}

	for _, field := range exp.FieldNotEmpty {
		val, ok := actual[field]
		if !ok {
			diffs = append(diffs, DiffDetail{
				Path:    prefix + "." + field,
				Message: "expected non-empty field missing",
			})
		} else if isEmpty(val) {
			diffs = append(diffs, DiffDetail{
				Path:     prefix + "." + field,
				Expected: "non-empty",
				Actual:   val,
				Message:  "expected non-empty field is empty",
			})
		}
	}

	for field, expected := range exp.FieldEquals {
		actual := actual[field]
		if !jsonEqual(actual, expected) {
			diffs = append(diffs, DiffDetail{
				Path:     prefix + "." + field,
				Expected: expected,
				Actual:   actual,
				Message:  "field value mismatch",
			})
		}
	}

	for field, substr := range exp.FieldContains {
		val, _ := actual[field].(string)
		if !strings.Contains(val, substr) {
			diffs = append(diffs, DiffDetail{
				Path:     prefix + "." + field,
				Expected: "contains: " + substr,
				Actual:   truncate(val, 200),
				Message:  "field does not contain expected substring",
			})
		}
	}

	for field, nested := range exp.Nested {
		obj, ok := actual[field].(map[string]any)
		if !ok {
			diffs = append(diffs, DiffDetail{
				Path:    prefix + "." + field,
				Message: "expected nested object, got " + fmt.Sprintf("%T", actual[field]),
			})
			continue
		}
		diffs = append(diffs, compareEvent(prefix+"."+field, nested, obj)...)
	}

	return diffs
}

// extractSystemText extracts the concatenated text from a system prompt,
// handling both string and array-of-objects formats.
func extractSystemText(body map[string]any) string {
	sys := body["system"]
	if sys == nil {
		return ""
	}
	if s, ok := sys.(string); ok {
		return s
	}
	if arr, ok := sys.([]any); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", sys)
}

func jsonEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	if f, ok := v.(float64); ok {
		return f == 0
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
