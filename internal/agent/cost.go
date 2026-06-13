// Package agent provides the core agent loop.
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/log"
)

// ModelPricing defines per-token USD rates for a model.
type ModelPricing struct {
	InputUSD         float64
	OutputUSD        float64
	CacheReadUSD     float64
	CacheCreationUSD float64
	UnknownModel     bool
}

// DefaultPricing is the pricing table for known models.
// Rates are per-token in USD. All entries include source citations.
// CNY-denominated prices are converted at ~6.77 CNY/USD (June 2026 approximation).
var DefaultPricing = map[string]ModelPricing{
	// -------------------------------------------------------------------------
	// Anthropic Claude models — source: claude.com/pricing#api (June 2026)
	// -------------------------------------------------------------------------
	"claude-sonnet-4-20250514": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.0000003,  // $0.30/1M cache read
		CacheCreationUSD: 0.00000375, // $3.75/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-opus-4-20250514": {
		InputUSD:         0.000005,   // $5.00/1M input
		OutputUSD:        0.000025,   // $25.00/1M output
		CacheReadUSD:     0.0000005,  // $0.50/1M cache read
		CacheCreationUSD: 0.00000625, // $6.25/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-haiku-4-20250514": {
		InputUSD:         0.000001,  // $1.00/1M input
		OutputUSD:        0.000005,  // $5.00/1M output
		CacheReadUSD:     0.0000001, // $0.10/1M cache read
		CacheCreationUSD: 0.0000003, // $0.30/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-fable-5": {
		InputUSD:         0.00001,   // $10.00/1M input
		OutputUSD:        0.00005,   // $50.00/1M output
		CacheReadUSD:     0.000001,  // $1.00/1M cache read
		CacheCreationUSD: 0.0000125, // $12.50/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-sonnet-4.6": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.0000003,  // $0.30/1M cache read
		CacheCreationUSD: 0.00000375, // $3.75/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-opus-4.8": {
		InputUSD:         0.000005,   // $5.00/1M input
		OutputUSD:        0.000025,   // $25.00/1M output
		CacheReadUSD:     0.0000005,  // $0.50/1M cache read
		CacheCreationUSD: 0.00000625, // $6.25/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-haiku-4.5": {
		InputUSD:         0.000001,  // $1.00/1M input
		OutputUSD:        0.000005,  // $5.00/1M output
		CacheReadUSD:     0.0000001, // $0.10/1M cache read
		CacheCreationUSD: 0.0000003, // $0.30/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-3-5-sonnet-latest": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.000003,   // $3.00/1M cache read
		CacheCreationUSD: 0.00000375, // $3.75/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-3-5-sonnet-20240620": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.000003,   // $3.00/1M cache read
		CacheCreationUSD: 0.00000375, // $3.75/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-3-opus-latest": {
		InputUSD:         0.000015,   // $15.00/1M input
		OutputUSD:        0.000075,   // $75.00/1M output
		CacheReadUSD:     0.000015,   // $15.00/1M cache read
		CacheCreationUSD: 0.00000375, // $3.75/1M cache creation
		// source: claude.com/pricing#api
	},
	"claude-3-opus-20240229": {
		InputUSD:         0.000015,   // $15.00/1M input
		OutputUSD:        0.000075,   // $75.00/1M output
		CacheReadUSD:     0.000015,   // $15.00/1M cache read
		CacheCreationUSD: 0.00000375, // $3.75/1M cache creation
		// source: claude.com/pricing#api
	},
	// -------------------------------------------------------------------------
	// DeepSeek models — source: api-docs.deepseek.com/quick_start/pricing (June 2026)
	// -------------------------------------------------------------------------
	"deepseek-v4-flash": {
		InputUSD:         0.00000014,   // $0.14/1M input (cache miss)
		OutputUSD:        0.00000028,   // $0.28/1M output
		CacheReadUSD:     0.0000000028, // $0.0028/1M cache read (cache hit)
		CacheCreationUSD: 0,            // Not published
		// source: api-docs.deepseek.com/quick_start/pricing
	},
	"deepseek-v4-pro": {
		InputUSD:         0.000000435,    // $0.435/1M input (cache miss)
		OutputUSD:        0.00000087,     // $0.87/1M output
		CacheReadUSD:     0.000000003625, // $0.003625/1M cache read (cache hit)
		CacheCreationUSD: 0,              // Not published
		// source: api-docs.deepseek.com/quick_start/pricing
	},
	// -------------------------------------------------------------------------
	// Google Gemini models — source: ai.google.dev/gemini-api/docs/pricing (June 2026)
	// Standard tier, <=200K input tokens
	// -------------------------------------------------------------------------
	"gemini-3.5-flash": {
		InputUSD:         0.0000015,  // $1.50/1M input (text/image/video/audio)
		OutputUSD:        0.000009,   // $9.00/1M output
		CacheReadUSD:     0.00000015, // $0.15/1M cached input
		CacheCreationUSD: 0,          // Not published
		// source: ai.google.dev/gemini-api/docs/pricing
	},
	"gemini-3.1-pro": {
		InputUSD:         0.000002,  // $2.00/1M input
		OutputUSD:        0.000012,  // $12.00/1M output
		CacheReadUSD:     0.0000002, // $0.20/1M cached input
		CacheCreationUSD: 0,         // Not published
		// source: ai.google.dev/gemini-api/docs/pricing
	},
	"gemini-3.1-flash-lite": {
		InputUSD:         0.00000025,  // $0.25/1M input
		OutputUSD:        0.0000015,   // $1.50/1M output
		CacheReadUSD:     0.000000025, // $0.025/1M cached input
		CacheCreationUSD: 0,           // Not published
		// source: ai.google.dev/gemini-api/docs/pricing
	},
	// -------------------------------------------------------------------------
	// MiniMax models — source: platform.minimaxi.com/docs/guides/pricing-paygo (June 2026)
	// Prices published in CNY; converted at ~6.77 CNY/USD (June 2026 approximation)
	// -------------------------------------------------------------------------
	"minimax-m3": {
		InputUSD:         0.000000620, // ¥4.20/1M → $0.620/1M input (≤512K, standard tier)
		OutputUSD:        0.000002481, // ¥16.80/1M → $2.481/1M output
		CacheReadUSD:     0.000000124, // ¥0.84/1M → $0.124/1M cache read
		CacheCreationUSD: 0,           // N/A
		// source: platform.minimaxi.com/docs/guides/pricing-paygo
	},
	"minimax-m2.7": {
		InputUSD:         0.000000322, // ¥2.18/1M → $0.322/1M input
		OutputUSD:        0.000001240, // ¥8.40/1M → $1.240/1M output
		CacheReadUSD:     0.000000062, // ¥0.42/1M → $0.062/1M cache read
		CacheCreationUSD: 0.000000089, // ¥0.60/1M → $0.089/1M cache write
		// source: platform.minimaxi.com/docs/guides/pricing-paygo
	},
	"minimax-m2.7-highspeed": {
		InputUSD:         0.000000620, // ¥4.20/1M → $0.620/1M input (1.5x standard)
		OutputUSD:        0.000002481, // ¥16.80/1M → $2.481/1M output
		CacheReadUSD:     0.000000062, // ¥0.42/1M → $0.062/1M cache read
		CacheCreationUSD: 0.000000089, // ¥0.60/1M → $0.089/1M cache write
		// source: platform.minimaxi.com/docs/guides/pricing-paygo
	},
	"minimax-m2.5": {
		InputUSD:         0.000000310, // ¥2.10/1M → $0.310/1M input
		OutputUSD:        0.000001240, // ¥8.40/1M → $1.240/1M output
		CacheReadUSD:     0.000000031, // ¥0.21/1M → $0.031/1M cache read
		CacheCreationUSD: 0.000000074, // ¥0.50/1M → $0.074/1M cache write
		// source: platform.minimaxi.com/docs/guides/pricing-paygo
	},
	// -------------------------------------------------------------------------
	// Kimi/Moonshot models — source: platform.kimi.com/docs/pricing/chat-k26 (June 2026)
	// Prices published in CNY; converted at ~6.77 CNY/USD (June 2026 approximation)
	// -------------------------------------------------------------------------
	"kimi-k2.7-code": {
		InputUSD:         0.000000886, // ¥6.00/1M → $0.886/1M input
		OutputUSD:        0.000005023, // ¥34.00/1M → $5.023/1M output
		CacheReadUSD:     0.000000886, // ¥6.00/1M → $0.886/1M cache read (same as input)
		CacheCreationUSD: 0,           // Not published
		// source: platform.kimi.com/docs/pricing/chat-k26
	},
	"kimi-k2.6": {
		InputUSD:         0.000000664, // ¥4.50/1M → $0.664/1M input
		OutputUSD:        0.000003840, // ¥26.00/1M → $3.840/1M output
		CacheReadUSD:     0.000000664, // ¥4.50/1M → $0.664/1M cache read (same as input)
		CacheCreationUSD: 0,           // Not published
		// source: platform.kimi.com/docs/pricing/chat-k26
	},
	"kimi-k2.5": {
		InputUSD:         0.000000517, // ¥3.50/1M → $0.517/1M input
		OutputUSD:        0.000003101, // ¥21.00/1M → $3.101/1M output
		CacheReadUSD:     0.000000517, // ¥3.50/1M → $0.517/1M cache read (same as input)
		CacheCreationUSD: 0,           // Not published
		// source: platform.kimi.com/docs/pricing/chat-k25
	},
	"moonshot-v1-8k": {
		InputUSD:         0.000000074, // ¥0.50/1M → $0.074/1M input
		OutputUSD:        0.000000443, // ¥3.00/1M → $0.443/1M output
		CacheReadUSD:     0.000000074, // ¥0.50/1M → $0.074/1M cache read (same as input)
		CacheCreationUSD: 0,           // Not published
		// source: platform.kimi.com/docs/pricing/chat-v1
	},
	"moonshot-v1-32k": {
		InputUSD:         0.000000148, // ¥1.00/1M → $0.148/1M input
		OutputUSD:        0.000000443, // ¥3.00/1M → $0.443/1M output
		CacheReadUSD:     0.000000148, // ¥1.00/1M → $0.148/1M cache read (same as input)
		CacheCreationUSD: 0,           // Not published
		// source: platform.kimi.com/docs/pricing/chat-v1
	},
	"moonshot-v1-128k": {
		InputUSD:         0.000000296, // ¥2.00/1M → $0.296/1M input
		OutputUSD:        0.000000443, // ¥3.00/1M → $0.443/1M output
		CacheReadUSD:     0.000000296, // ¥2.00/1M → $0.296/1M cache read (same as input)
		CacheCreationUSD: 0,           // Not published
		// source: platform.kimi.com/docs/pricing/chat-v1
	},
	// -------------------------------------------------------------------------
	// Qwen/Alibaba models — source: docs.qwencloud.com/developer-guides/getting-started/pricing (June 2026)
	// -------------------------------------------------------------------------
	"qwen-3.7-max": {
		InputUSD:         0.0000025,   // $2.50/1M input (standard tier)
		OutputUSD:        0.0000075,   // $7.50/1M output
		CacheReadUSD:     0.00000025,  // $0.25/1M cache read (implicit cache)
		CacheCreationUSD: 0.000003125, // $3.125/1M cache creation (explicit)
		// source: docs.qwencloud.com/developer-guides/getting-started/pricing
	},
	"qwen-3.7-plus": {
		InputUSD:         0.0000004,  // $0.40/1M input (standard tier)
		OutputUSD:        0.0000016,  // $1.60/1M output
		CacheReadUSD:     0.00000004, // $0.04/1M cache read (implicit cache)
		CacheCreationUSD: 0.0000005,  // $0.50/1M cache creation (explicit)
		// source: docs.qwencloud.com/developer-guides/getting-started/pricing
	},
	"qwen-3.6-flash": {
		InputUSD:         0.00000025, // $0.25/1M input
		OutputUSD:        0.0000015,  // $1.50/1M output
		CacheReadUSD:     0,          // Not published
		CacheCreationUSD: 0,          // Not published
		// source: docs.qwencloud.com/developer-guides/getting-started/pricing
	},
	// -------------------------------------------------------------------------
	// Tencent Hunyuan models — source: cloud.tencent.com/document/product/1729/97731 (June 2026)
	// Prices published in CNY; converted at ~6.77 CNY/USD (June 2026 approximation)
	// -------------------------------------------------------------------------
	"hunyuan-turbos": {
		InputUSD:         0.000000118, // ¥0.80/1M → $0.118/1M input
		OutputUSD:        0.000000295, // ¥2.00/1M → $0.295/1M output
		CacheReadUSD:     0,           // Not published
		CacheCreationUSD: 0,           // Not published
		// source: cloud.tencent.com/document/product/1729/97731
	},
	"hunyuan-t1": {
		InputUSD:         0.000000148, // ¥1.00/1M → $0.148/1M input
		OutputUSD:        0.000000591, // ¥4.00/1M → $0.591/1M output
		CacheReadUSD:     0,           // Not published
		CacheCreationUSD: 0,           // Not published
		// source: cloud.tencent.com/document/product/1729/97731
	},
	"hunyuan-hy-2.0-instruct": {
		InputUSD:         0.000000470, // ¥3.18/1M → $0.470/1M input (≤32K tokens)
		OutputUSD:        0.000001174, // ¥7.95/1M → $1.174/1M output
		CacheReadUSD:     0,           // Not published
		CacheCreationUSD: 0,           // Not published
		// source: cloud.tencent.com/document/product/1729/97731
	},
	"hunyuan-hy-2.0-think": {
		InputUSD:         0.000000587, // ¥3.975/1M → $0.587/1M input (≤32K tokens)
		OutputUSD:        0.000002349, // ¥15.90/1M → $2.349/1M output
		CacheReadUSD:     0,           // Not published
		CacheCreationUSD: 0,           // Not published
		// source: cloud.tencent.com/document/product/1729/97731
	},
	"hunyuan-a13b": {
		InputUSD:         0.000000074, // ¥0.50/1M → $0.074/1M input
		OutputUSD:        0.000000295, // ¥2.00/1M → $0.295/1M output
		CacheReadUSD:     0,           // Not published
		CacheCreationUSD: 0,           // Not published
		// source: cloud.tencent.com/document/product/1729/97731
	},
}

// UnknownModelPricing is the conservative default for unknown models.
var UnknownModelPricing = ModelPricing{
	InputUSD:         0.000003,
	OutputUSD:        0.000015,
	CacheReadUSD:     0.000003,
	CacheCreationUSD: 0.00000375,
	UnknownModel:     true,
}

// Custom pricing override loaded from .jenny/pricing.json.
// Supports reload: if the file changes, LoadCustomPricing re-reads it.
var customPricing map[string]ModelPricing
var customPricingMu sync.RWMutex
var customPricingPath string   // path of last loaded file
var customPricingModTime int64 // mtime of last loaded file

// LoadCustomPricing reads .jenny/pricing.json from the project directory and
// merges entries into the global custom pricing map. File entries take precedence
// over DefaultPricing. Malformed JSON produces a logged warning (not fatal).
// Safe to call multiple times; re-reads the file if it has changed since last load.
func LoadCustomPricing(projectDir string) {
	configPath := filepath.Join(projectDir, ".jenny", "pricing.json")

	// Quick path: check if file has changed without locking
	customPricingMu.RLock()
	samePath := customPricingPath == configPath
	modTime := customPricingModTime
	customPricingMu.RUnlock()

	if samePath && modTime != 0 {
		if stat, err := os.Stat(configPath); err == nil && stat.ModTime().Unix() == modTime {
			return // no change, skip re-read
		}
	}

	customPricingMu.Lock()
	defer customPricingMu.Unlock()

	// Double-check after acquiring write lock
	if customPricingPath == configPath && customPricingModTime != 0 {
		if stat, err := os.Stat(configPath); err == nil && stat.ModTime().Unix() == customPricingModTime {
			return
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("failed to read pricing override", "path", configPath, "error", err)
		}
		customPricingPath = configPath
		customPricingModTime = 0
		customPricing = nil
		return
	}

	var overrides map[string]ModelPricing
	if err := json.Unmarshal(data, &overrides); err != nil {
		log.Warn("failed to parse pricing override", "path", configPath, "error", err)
		customPricingPath = configPath
		customPricingModTime = 0
		customPricing = nil
		return
	}

	customPricing = overrides
	if stat, err := os.Stat(configPath); err == nil {
		customPricingModTime = stat.ModTime().Unix()
	}
	customPricingPath = configPath
	log.Debug("loaded custom pricing override", "models", len(overrides))
}

// ResetCustomPricing clears the custom pricing map. Intended for testing only.
func ResetCustomPricing() {
	customPricingMu.Lock()
	customPricing = nil
	customPricingPath = ""
	customPricingModTime = 0
	customPricingMu.Unlock()
}

// GetModelPricing returns the pricing for a model, checking custom overrides first,
// then DefaultPricing, then returning a conservative default for unknown models.
func GetModelPricing(model string) ModelPricing {
	// Check custom overrides first
	customPricingMu.RLock()
	p, ok := customPricing[model]
	customPricingMu.RUnlock()
	if ok {
		return p
	}

	// Check default table
	if pricing, ok := DefaultPricing[model]; ok {
		return pricing
	}

	return UnknownModelPricing
}

// CalculateCost computes the USD cost for given token counts using model pricing.
func CalculateCost(pricing ModelPricing, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int) float64 {
	cost := float64(inputTokens)*pricing.InputUSD +
		float64(outputTokens)*pricing.OutputUSD +
		float64(cacheReadTokens)*pricing.CacheReadUSD +
		float64(cacheCreationTokens)*pricing.CacheCreationUSD
	return cost
}

// ModelUsage tracks token usage for a specific model.
type ModelUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	CostUSD                  float64
}

// CostState tracks accumulated cost across all models.
type CostState struct {
	LastSessionID       string
	ModelUsage          map[string]ModelUsage
	TotalCostUSD        float64
	HasUnknownModelCost bool

	// Compaction state - persisted for session resume
	CompactFailCount int
}

// AccumulateUsage adds token usage from an API response to the cost state.
func AccumulateUsage(state *CostState, model string, usage api.Usage) {
	if state.ModelUsage == nil {
		state.ModelUsage = make(map[string]ModelUsage)
	}

	mu := state.ModelUsage[model]
	mu.InputTokens += usage.InputTokens
	mu.OutputTokens += usage.OutputTokens
	mu.CacheReadInputTokens += usage.CacheReadInputTokens
	mu.CacheCreationInputTokens += usage.CacheCreationInputTokens

	pricing := GetModelPricing(model)
	mu.CostUSD = CalculateCost(pricing, mu.InputTokens, mu.OutputTokens, mu.CacheReadInputTokens, mu.CacheCreationInputTokens)

	state.ModelUsage[model] = mu

	if pricing.UnknownModel {
		state.HasUnknownModelCost = true
	}

	// Recalculate total USD
	state.TotalCostUSD = 0
	for _, m := range state.ModelUsage {
		state.TotalCostUSD += m.CostUSD
	}
}

// CheckBudgetExceeded checks if the accumulated USD cost exceeds the budget.
// Returns true if budget is exceeded (should stop) and the current total USD cost.
// When maxBudget <= 0, no limit applies (returns false, current total).
func CheckBudgetExceeded(state *CostState, maxBudget float64) (bool, float64) {
	if maxBudget <= 0 {
		return false, state.TotalCostUSD
	}
	return state.TotalCostUSD > maxBudget, state.TotalCostUSD
}
