package harness

import (
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
		for _, d := range compareStdout(tc.Expected.Stdout, actual.Stdout) {
			diffs = append(diffs, d)
		}
	}

	// Stderr checks
	if tc.Expected.Stderr != nil {
		for _, d := range compareStderr(tc.Expected.Stderr, actual.Stderr) {
			diffs = append(diffs, d)
		}
	}

	return CompareResult{
		Pass: len(diffs) == 0,
		Diff: diffs,
	}
}

func compareStdout(exp *StdoutExpectation, actual string) []DiffDetail {
	var diffs []DiffDetail

	if exp.Equals != "" && actual != exp.Equals {
		diffs = append(diffs, DiffDetail{
			Path:     "stdout",
			Expected: exp.Equals,
			Actual:   actual,
			Message:  "stdout does not match exactly",
		})
	}

	if exp.IsEmpty && actual != "" {
		diffs = append(diffs, DiffDetail{
			Path:     "stdout",
			Expected: "",
			Actual:   actual,
			Message:  "expected empty stdout",
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
				Path:     "stdout",
				Expected: "contains one of: " + strings.Join(exp.Contains, ", "),
				Actual:   actual,
				Message:  "stdout does not contain any expected substring",
			})
		}
	}

	if len(exp.NotContains) > 0 {
		for _, substr := range exp.NotContains {
			if strings.Contains(actual, substr) {
				diffs = append(diffs, DiffDetail{
					Path:     "stdout",
					Expected: "not contains: " + substr,
					Actual:   actual,
					Message:  "stdout contains unexpected substring",
				})
			}
		}
	}

	if len(exp.Matches) > 0 {
		for i, pattern := range exp.Matches {
			re, err := regexp.Compile(pattern)
			if err != nil {
				diffs = append(diffs, DiffDetail{
					Path:     "stdout",
					Expected: "regex: " + pattern,
					Actual:   "invalid regex",
					Message:  "invalid regex pattern",
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
					Path:     "stdout",
					Expected: "matches regex: " + pattern,
					Actual:   "no line matched",
					Message:  "stdout does not match expected regex at line " + string(rune('0'+i)),
				})
			}
		}
	}

	if exp.Length != nil {
		actualLen := len(actual)
		if exp.Length.Min > 0 && actualLen < exp.Length.Min {
			diffs = append(diffs, DiffDetail{
				Path:     "stdout",
				Expected: "length >= " + itoa(exp.Length.Min),
				Actual:   itoa(actualLen),
				Message:  "stdout too short",
			})
		}
		if exp.Length.Max > 0 && actualLen > exp.Length.Max {
			diffs = append(diffs, DiffDetail{
				Path:     "stdout",
				Expected: "length <= " + itoa(exp.Length.Max),
				Actual:   itoa(actualLen),
				Message:  "stdout too long",
			})
		}
		if exp.Length.Exact > 0 && actualLen != exp.Length.Exact {
			diffs = append(diffs, DiffDetail{
				Path:     "stdout",
				Expected: "length == " + itoa(exp.Length.Exact),
				Actual:   itoa(actualLen),
				Message:  "stdout length mismatch",
			})
		}
	}

	return diffs
}

func compareStderr(exp *StderrExpectation, actual string) []DiffDetail {
	// Same implementation as stdout for now
	return compareStdout((*StdoutExpectation)(exp), actual)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b strings.Builder
	for n > 0 {
		b.WriteByte(byte('0' + n%10))
		n /= 10
	}
	// Reverse the string
	s := b.String()
	r := []byte(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
