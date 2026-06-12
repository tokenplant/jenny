package redact

import (
	"regexp"
	"strings"
)

// Finding is a single secret match produced by the Detector. The Redactor
// converts each Finding into a placeholder and stores the Secret for later
// Recover(). Mirrors the shape of gitleaks/v8/report.Finding (subset).
type Finding struct {
	// RuleID identifies which rule matched (e.g. "aws-access-token",
	// "generic-api-key"). Useful for allowlist configuration and reporting.
	RuleID string

	// Secret is the actual secret value that should be redacted. This is
	// either the full regex match (SecretGroup == 0) or a named capture
	// group (SecretGroup > 0).
	Secret string

	// Match is the full regex match (or the surrounding context for rules
	// with a SecretGroup). The Redactor uses Match to locate the position
	// in the content for placeholder substitution.
	Match string

	// Start is the byte index of Match in the original content.
	Start int

	// End is the byte index just past the last byte of Match.
	End int

	// Entropy is the Shannon entropy of Secret, in bits/char. Recorded for
	// debugging and for tests; the Redactor doesn't act on it directly.
	Entropy float64
}

// Allowlist filters out matches that look like secrets but are known-safe.
// Mirrors gitleaks/v8/config.Allowlist. For tool-result redaction we only
// need regex and stopwords; path/commit filters are N/A outside of git
// source scanning.
type Allowlist struct {
	// Regexes are matched against the secret. Any match exempts the finding.
	Regexes []*regexp.Regexp

	// StopWords are common placeholder/test values that look like secrets
	// but are not (e.g. "example", "test", "xxxxxx"). Any case-insensitive
	// substring match exempts the finding.
	StopWords []string
}

// allowed reports whether the given secret is exempt from redaction by this
// allowlist. Returns true if any regex matches OR any stop word is a
// case-insensitive substring of the secret.
func (a *Allowlist) allowed(secret string) bool {
	if a == nil {
		return false
	}
	lower := strings.ToLower(secret)
	for _, re := range a.Regexes {
		if re.MatchString(secret) {
			return true
		}
	}
	for _, sw := range a.StopWords {
		if sw == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(sw)) {
			return true
		}
	}
	return false
}

// Rule is a single detection rule. Mirrors gitleaks/v8/config.Rule. Each rule
// scans content via its Regex, optionally gated by a Shannon-entropy
// threshold on the captured secret, optionally gated by keyword prefiltering,
// and optionally exempted by an Allowlist.
type Rule struct {
	// ID is the unique rule identifier (e.g. "aws-access-token").
	ID string

	// Description is a human-readable summary of what the rule catches.
	Description string

	// Regex is the pattern that defines a candidate secret. Required.
	Regex *regexp.Regexp

	// Entropy is the minimum Shannon entropy (bits/char) of the captured
	// secret. 0 means no entropy check (use only when the regex itself
	// is precise enough). gitleaks' typical default for entropy-gated
	// rules is 3.5-4.0.
	Entropy float64

	// Keywords, if non-empty, are lowercased and matched (substring,
	// case-insensitive) against the content. The rule is only run when
	// at least one keyword is present. This is the "generic-api-key"
	// prefilter that cuts down false positives on arbitrary high-entropy
	// strings.
	Keywords []string

	// SecretGroup selects which capture group is the secret value.
	// 0 = full match (the default). 1 = first capture group. Etc.
	// Mirrors gitleaks' Rule.SecretGroup.
	SecretGroup int

	// Allowlist is a per-rule exemption list. See Allowlist.
	Allowlist *Allowlist
}

// Detector runs a set of Rules against content and returns Findings. The
// Redactor owns a single Detector populated with defaultRules(); the
// Detector itself is stateless after construction and safe to call
// concurrently from multiple goroutines (the Redactor serializes calls
// under its own mutex).
type Detector struct {
	rules []Rule
}

// NewDetector builds a Detector from a rule set. The rules slice is
// iterated in order; for performance, put cheaper / higher-signal rules
// (those with tight regex and few keywords) before generic ones.
func NewDetector(rules []Rule) *Detector {
	return &Detector{rules: rules}
}

// DefaultDetector returns a Detector preloaded with defaultRules(), the
// gitleaks-aligned default rule set. The rule set is built once at package
// init and shared across all Redactors.
func DefaultDetector() *Detector {
	return NewDetector(defaultRules())
}

// Detect scans content and returns a slice of Findings, one per non-exempt
// regex match that also passes entropy and keyword gates. Findings are
// returned in rule-firing order (not in content position order); the
// Redactor re-sorts them by Start before substituting placeholders.
//
// Algorithm (mirrors gitleaks/v8/detect.Detector.Detect):
//
//  1. Build a set of keywords actually present in content (case-insensitive
//     substring search). Empty if no rule has keywords.
//  2. For each rule:
//     a. If the rule has keywords, skip unless at least one is present.
//     b. Run rule.Regex.FindAllStringSubmatchIndex on content.
//     c. For each match, extract the secret (full match or capture group),
//     compute entropy, apply gates (entropy, allowlist).
//     d. Append Finding to results.
func (d *Detector) Detect(content string) []Finding {
	if len(d.rules) == 0 {
		return nil
	}
	present := keywordPresence(content)
	var findings []Finding
	for _, rule := range d.rules {
		if len(rule.Keywords) > 0 && !anyKeywordPresent(rule.Keywords, present) {
			continue
		}
		matches := rule.Regex.FindAllStringSubmatchIndex(content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			secret, matchText, ok := extractSecret(content, m, rule.SecretGroup)
			if !ok {
				continue
			}
			if rule.Allowlist != nil && rule.Allowlist.allowed(secret) {
				continue
			}
			if rule.Entropy > 0 {
				ent := shannonEntropy(secret)
				if ent <= rule.Entropy {
					continue
				}
				findings = append(findings, Finding{
					RuleID:  rule.ID,
					Secret:  secret,
					Match:   matchText,
					Start:   m[0],
					End:     m[1],
					Entropy: ent,
				})
				continue
			}
			findings = append(findings, Finding{
				RuleID: rule.ID,
				Secret: secret,
				Match:  matchText,
				Start:  m[0],
				End:    m[1],
			})
		}
	}
	return findings
}

// keywordPresence returns a set of all lowercased keywords from defaultRules
// that appear in content. The set is a map[string]bool for O(1) lookup
// during rule iteration. Mirrors gitleaks' prefilter trie in shape, but
// uses simple substring search since our keyword list is small (~50).
func keywordPresence(content string) map[string]bool {
	lower := strings.ToLower(content)
	present := make(map[string]bool)
	for _, kw := range allKeywords() {
		if strings.Contains(lower, kw) {
			present[kw] = true
		}
	}
	return present
}

// anyKeywordPresent reports whether at least one of the rule's keywords is
// in the present set. Mirrors gitleaks' fragment-keyword check.
func anyKeywordPresent(keywords []string, present map[string]bool) bool {
	for _, kw := range keywords {
		if present[kw] {
			return true
		}
	}
	return false
}

// extractSecret pulls the secret value out of a regex match. Returns the
// secret, the full match text, and ok=false if the requested SecretGroup
// is out of range.
func extractSecret(content string, match []int, secretGroup int) (secret, matchText string, ok bool) {
	if secretGroup == 0 {
		return content[match[0]:match[1]], content[match[0]:match[1]], true
	}
	// Capture group: FindStringSubmatchIndex returns pairs of indices, with
	// match[0]/match[1] = full match and match[2i]/match[2i+1] = group i.
	g0 := 2 * secretGroup
	g1 := g0 + 1
	if g1 >= len(match) || match[g0] < 0 || match[g1] < 0 {
		return "", "", false
	}
	return content[match[g0]:match[g1]], content[match[0]:match[1]], true
}
