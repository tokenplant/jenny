package redact

import (
	"regexp"
	"strings"
)

// defaultRules returns the gitleaks-aligned default rule set. Each rule
// mirrors the corresponding gitleaks/v8 default rule in shape (regex,
// entropy, keywords, allowlist) — the bodies are reimplemented here rather
// than imported, since the user requirement is to match gitleaks'
// capability level without taking a runtime dependency on the gitleaks
// package.
//
// The set covers the most common vendor tokens encountered in tool results
// (OpenAI, GitHub, AWS, Stripe, Slack, Discord, GitLab, NPM, PyPI,
// Heroku, Twilio, SendGrid, Mailgun, JWT, SSH PEM) plus a generic
// high-entropy API-key rule gated by keyword prefilter.
//
// Rules are ordered: high-signal / narrow-regex rules first (so they
// match before the generic one has a chance to swallow surrounding
// text), then the generic rule last.
func defaultRules() []Rule {
	commonStopWords := []string{
		"example", "example.com", "test", "sample", "placeholder",
		"dummy", "fake", "mock", "your-", "my-", "changeme",
		"xxxxxx", "000000", "111111", "123456", "1234567890",
		"abcdef", "qwerty", "asdf",
	}
	_ = commonStopWords // referenced by individual rule allowlists below

	return []Rule{
		// ----- AWS -----
		{
			ID:          "aws-access-token",
			Description: "AWS Access Key ID",
			Regex:       regexp.MustCompile(`\b((?:AKIA|ASIA)[A-Z0-9]{16})\b`),
			Keywords:    []string{"akia", "asia", "aws"},
		},
		{
			ID:          "aws-secret-key",
			Description: "AWS Secret Access Key (context-anchored)",
			Regex:       regexp.MustCompile(`(?i)\b((?:aws|amazon)?_?(?:secret)?_?(?:access)?_?key)\b\s*[:=]\s*["']?([A-Za-z0-9/+=]{40})["']?`),
			Entropy:     3.5,
			SecretGroup: 2,
			Keywords:    []string{"aws", "secret", "key"},
			Allowlist: &Allowlist{
				StopWords: commonStopWords,
			},
		},

		// ----- Stripe -----
		{
			ID:          "stripe-access-token",
			Description: "Stripe API key (live or test)",
			Regex:       regexp.MustCompile(`\b((?:sk|rk)_(?:live|test)_[A-Za-z0-9]{24,99})\b`),
			Keywords:    []string{"stripe", "sk_live", "sk_test", "rk_live"},
		},
		{
			ID:          "stripe-restricted-key",
			Description: "Stripe restricted API key",
			Regex:       regexp.MustCompile(`\b(rk_live_[A-Za-z0-9]{24,99})\b`),
			Keywords:    []string{"stripe", "rk_live"},
		},

		// ----- GitHub -----
		{
			ID:          "github-pat",
			Description: "GitHub Personal Access Token (classic)",
			Regex:       regexp.MustCompile(`\b(ghp_[A-Za-z0-9]{36,255})\b`),
			Keywords:    []string{"ghp_", "github", "token"},
		},
		{
			ID:          "github-oauth",
			Description: "GitHub OAuth Access Token",
			Regex:       regexp.MustCompile(`\b(gho_[A-Za-z0-9]{36,255})\b`),
			Keywords:    []string{"gho_", "github", "token"},
		},
		{
			ID:          "github-app-token",
			Description: "GitHub App installation token",
			Regex:       regexp.MustCompile(`\b((?:ghu|ghs)_[A-Za-z0-9]{36,255})\b`),
			Keywords:    []string{"ghu_", "ghs_", "github", "token"},
		},
		{
			ID:          "github-fine-grained-pat",
			Description: "GitHub fine-grained Personal Access Token",
			Regex:       regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{82})\b`),
			Keywords:    []string{"github_pat_", "github", "token"},
		},
		{
			ID:          "github-refresh-token",
			Description: "GitHub refresh token",
			Regex:       regexp.MustCompile(`\b(ghr_[A-Za-z0-9]{36,255})\b`),
			Keywords:    []string{"ghr_", "github", "token"},
		},

		// ----- GitLab -----
		{
			ID:          "gitlab-pat",
			Description: "GitLab Personal Access Token",
			Regex:       regexp.MustCompile(`\b(glpat-[A-Za-z0-9_\-]{20,})\b`),
			Keywords:    []string{"glpat-", "gitlab", "token"},
		},
		{
			ID:          "gitlab-runner-token",
			Description: "GitLab Runner Registration Token",
			Regex:       regexp.MustCompile(`\b(GR1348941[A-Za-z0-9_\-]{20,})\b`),
			Keywords:    []string{"gr1348941", "gitlab"},
		},

		// ----- Slack -----
		{
			ID:          "slack-token",
			Description: "Slack API token (xox[baprs]-)",
			Regex:       regexp.MustCompile(`\b(xox[baprs]-[A-Za-z0-9-]{10,48})\b`),
			Keywords:    []string{"xoxb-", "xoxa-", "xoxp-", "xoxr-", "xoxs-", "slack"},
		},
		{
			ID:          "slack-webhook",
			Description: "Slack incoming webhook URL",
			Regex:       regexp.MustCompile(`(https?://hooks\.slack\.com/services/T[A-Z0-9]{8,}/B[A-Z0-9]{8,}/[A-Za-z0-9]{24})`),
			Keywords:    []string{"hooks.slack.com", "slack"},
		},

		// ----- Discord -----
		{
			ID:          "discord-token",
			Description: "Discord bot/user token",
			Regex:       regexp.MustCompile(`\b([MN][A-Za-z0-9]{23,25}\.[A-Za-z0-9_\-]{6,7}\.[A-Za-z0-9_\-]{27,})\b`),
			Entropy:     4.0,
			Keywords:    []string{"discord", "token", "bot"},
		},
		{
			ID:          "discord-webhook",
			Description: "Discord webhook URL",
			Regex:       regexp.MustCompile(`(https?://(?:canary\.|ptb\.)?discord(?:app)?\.com/api/webhooks/[0-9]{17,20}/[A-Za-z0-9_\-]{60,80})`),
			Keywords:    []string{"discord", "webhook"},
		},

		// ----- OpenAI / Anthropic -----
		{
			ID:          "openai-api-key",
			Description: "OpenAI API key",
			Regex:       regexp.MustCompile(`\b(sk-(?:proj-)?[A-Za-z0-9_\-]{20,})\b`),
			Keywords:    []string{"sk-", "openai", "api_key", "apikey"},
		},
		{
			ID:          "anthropic-api-key",
			Description: "Anthropic API key",
			Regex:       regexp.MustCompile(`\b(sk-ant-(?:api03-)?[A-Za-z0-9_\-]{20,})\b`),
			Keywords:    []string{"sk-ant-", "anthropic", "api_key", "apikey"},
		},

		// ----- NPM / PyPI -----
		{
			ID:          "npm-token",
			Description: "NPM access token",
			Regex:       regexp.MustCompile(`\b(npm_[A-Za-z0-9]{36})\b`),
			Keywords:    []string{"npm_", "npm", "token"},
		},
		{
			ID:          "pypi-token",
			Description: "PyPI API token",
			Regex:       regexp.MustCompile(`\b(pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_\-]{50,})\b`),
			Keywords:    []string{"pypi-", "pypi", "token"},
		},

		// ----- Heroku -----
		{
			ID:          "heroku-api-key",
			Description: "Heroku API key (UUID-shaped)",
			Regex:       regexp.MustCompile(`(?i)\bheroku(?:api)?[_\-]?key\b\s*[:=]\s*["']?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})["']?`),
			SecretGroup: 1,
			Keywords:    []string{"heroku"},
		},

		// ----- Twilio -----
		{
			ID:          "twilio-api-key",
			Description: "Twilio API Key SID",
			Regex:       regexp.MustCompile(`\b(SK[0-9a-fA-F]{32})\b`),
			Keywords:    []string{"twilio", "sk"},
		},

		// ----- SendGrid -----
		{
			ID:          "sendgrid-api-key",
			Description: "SendGrid API key",
			Regex:       regexp.MustCompile(`\b(SG\.[A-Za-z0-9_\-]{22}\.[A-Za-z0-9_\-]{43})\b`),
			Keywords:    []string{"sendgrid", "sg."},
		},

		// ----- Mailgun -----
		{
			ID:          "mailgun-api-key",
			Description: "Mailgun API key",
			Regex:       regexp.MustCompile(`\b(key-[0-9a-zA-Z]{32})\b`),
			Keywords:    []string{"mailgun", "key-"},
		},

		// ----- JWT -----
		{
			ID:          "jwt",
			Description: "JSON Web Token",
			Regex:       regexp.MustCompile(`\b(eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,})\b`),
			Entropy:     3.5,
			Keywords:    []string{"eyj", "jwt", "token", "bearer"},
		},

		// ----- SSH private keys (PEM) -----
		{
			ID:          "ssh-private-key",
			Description: "PEM-encoded private key",
			Regex:       regexp.MustCompile(`-----BEGIN[ A-Z]*PRIVATE KEY-----[\s\S]*?-----END[ A-Z]*PRIVATE KEY-----`),
			Keywords:    []string{"private key", "begin"},
		},

		// ----- Generic high-entropy API key (keyword-gated) -----
		//
		// This is the rule that catches tokens the vendor-specific rules
		// miss — custom API keys, random passwords, machine-issued secrets
		// that don't follow a known prefix. The keyword prefilter is what
		// makes it stable: without it, we'd redact arbitrary long identifiers
		// in code or paths. With it, we only fire on text that *looks* like
		// a secret assignment.
		//
		// Mirrors gitleaks' "generic-api-key" rule:
		//   regex:   `(?:^|[^0-9A-Za-z])(?P<key>[A-Za-z0-9_-]{32,45})(?:\b|$)`
		//   entropy: 3.5
		//   keywords: [key, secret, token, password, auth, credential, ...]
		//
		// Max length 160 to cover long-format tokens (e.g. "fe_oa_..." 54+ chars)
		// and edge cases that pass entropy but exceed gitleaks' default of 45.
		// JWT is caught by the dedicated "jwt" rule (no max), so generic-api-key
		// only needs to cover bearer-style API keys. keyword prefilter + entropy
		// gate keep false positives low despite the generous upper bound.
		{
			ID:          "generic-api-key",
			Description: "Generic high-entropy API key (keyword-gated)",
			Regex:       regexp.MustCompile(`(?:^|[^0-9A-Za-z_])([A-Za-z0-9_\-]{32,160})(?:[^0-9A-Za-z_\-]|$)`),
			SecretGroup: 1,
			Entropy:     3.5,
			Keywords: []string{
				"key", "keys", "secret", "secrets", "token", "tokens",
				"password", "passwd", "passphrase", "credential", "credentials",
				"auth", "authorization", "bearer", "api", "apikey", "api_key",
				"access_key", "private", "private_key", "client_secret",
			},
			Allowlist: &Allowlist{
				StopWords: commonStopWords,
			},
		},
	}
}

// allKeywords returns the deduplicated, lowercased union of every rule's
// keyword set. Used by keywordPresence to build the prefilter set.
func allKeywords() []string {
	seen := make(map[string]struct{})
	for _, r := range defaultRules() {
		for _, kw := range r.Keywords {
			seen[strings.ToLower(kw)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for kw := range seen {
		out = append(out, kw)
	}
	return out
}
