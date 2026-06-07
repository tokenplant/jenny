// Package agent provides the core agent loop.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/api"
	"github.com/ipy/jenny/internal/constants"
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
// Rates are per-token in USD.
var DefaultPricing = map[string]ModelPricing{
	"deepseek-v4-flash": {
		InputUSD:         0.0000015,  // $1.50/1M input
		OutputUSD:        0.000008,   // $8.00/1M output
		CacheReadUSD:     0.00000015, // $0.15/1M cache read
		CacheCreationUSD: 0.000004,   // $4.00/1M cache creation
	},
	"claude-sonnet-4-20250514": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.000003,   // Same as input (cache hit)
		CacheCreationUSD: 0.00001875, // $18.75/1M cache creation
	},
	"claude-opus-4-20250514": {
		InputUSD:         0.000015,   // $15.00/1M input
		OutputUSD:        0.000075,   // $75.00/1M output
		CacheReadUSD:     0.000015,   // Same as input
		CacheCreationUSD: 0.00001875, // $18.75/1M cache creation
	},
	"claude-3-5-sonnet-latest": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.000003,   // Same as input
		CacheCreationUSD: 0.00001875, // $18.75/1M cache creation
	},
	"claude-3-5-sonnet-20240620": {
		InputUSD:         0.000003,   // $3.00/1M input
		OutputUSD:        0.000015,   // $15.00/1M output
		CacheReadUSD:     0.000003,   // Same as input
		CacheCreationUSD: 0.00001875, // $18.75/1M cache creation
	},
	"claude-3-opus-latest": {
		InputUSD:         0.000015,   // $15.00/1M input
		OutputUSD:        0.000075,   // $75.00/1M output
		CacheReadUSD:     0.000015,   // Same as input
		CacheCreationUSD: 0.00001875, // $18.75/1M cache creation
	},
	"claude-3-opus-20240229": {
		InputUSD:         0.000015,   // $15.00/1M input
		OutputUSD:        0.000075,   // $75.00/1M output
		CacheReadUSD:     0.000015,   // Same as input
		CacheCreationUSD: 0.00001875, // $18.75/1M cache creation
	},
}

// DefaultPricingCNY is the CNY pricing table for known models.
// Rates are per-token in CNY (approximately USD × 7.0).
var DefaultPricingCNY = map[string]ModelPricing{
	"deepseek-v4-flash": {
		InputUSD:         0.0000105,  // ¥10.50/1M input
		OutputUSD:        0.000056,   // ¥56.00/1M output
		CacheReadUSD:     0.00000105, // ¥1.05/1M cache read
		CacheCreationUSD: 0.000028,   // ¥28.00/1M cache creation
	},
	"claude-sonnet-4-20250514": {
		InputUSD:         0.000021,   // ¥21.00/1M input
		OutputUSD:        0.000105,   // ¥105.00/1M output
		CacheReadUSD:     0.000021,   // Same as input (cache hit)
		CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	},
	"claude-opus-4-20250514": {
		InputUSD:         0.000105,   // ¥105.00/1M input
		OutputUSD:        0.000525,   // ¥525.00/1M output
		CacheReadUSD:     0.000105,   // Same as input
		CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	},
	"claude-3-5-sonnet-latest": {
		InputUSD:         0.000021,   // ¥21.00/1M input
		OutputUSD:        0.000105,   // ¥105.00/1M output
		CacheReadUSD:     0.000021,   // Same as input
		CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	},
	"claude-3-5-sonnet-20240620": {
		InputUSD:         0.000021,   // ¥21.00/1M input
		OutputUSD:        0.000105,   // ¥105.00/1M output
		CacheReadUSD:     0.000021,   // Same as input
		CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	},
	"claude-3-opus-latest": {
		InputUSD:         0.000105,   // ¥105.00/1M input
		OutputUSD:        0.000525,   // ¥525.00/1M output
		CacheReadUSD:     0.000105,   // Same as input
		CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	},
	"claude-3-opus-20240229": {
		InputUSD:         0.000105,   // ¥105.00/1M input
		OutputUSD:        0.000525,   // ¥525.00/1M output
		CacheReadUSD:     0.000105,   // Same as input
		CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	},
}

// UnknownModelPricing is the conservative default for unknown models.
var UnknownModelPricing = ModelPricing{
	InputUSD:         0.000003,
	OutputUSD:        0.000015,
	CacheReadUSD:     0.000003,
	CacheCreationUSD: 0.00001875,
	UnknownModel:     true,
}

// UnknownModelPricingCNY is the conservative default CNY pricing for unknown models.
var UnknownModelPricingCNY = ModelPricing{
	InputUSD:         0.000021,   // ¥21.00/1M input
	OutputUSD:        0.000105,   // ¥105.00/1M output
	CacheReadUSD:     0.000021,   // Same as input
	CacheCreationUSD: 0.00013125, // ¥131.25/1M cache creation
	UnknownModel:     true,
}

// ModelUsage tracks token usage for a specific model.
type ModelUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
	CostUSD                  float64
	CostCNY                  float64
}

// CostState tracks accumulated cost across all models.
type CostState struct {
	LastSessionID       string
	Currency            string `json:"currency,omitempty"`
	ModelUsage          map[string]ModelUsage
	TotalCostUSD        float64
	TotalCostCNY        float64
	HasUnknownModelCost bool

	// Compaction state - persisted for session resume
	CompactFailCount int
}

// costConfigPath returns the path to the cost config file.
func costConfigPath() string {
	return filepath.Join(constants.JennyHomeDir(), "config")
}

// SaveCostState saves the cost state to .jenny/config as JSON.
func SaveCostState(state *CostState) error {
	path := costConfigPath()
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling cost state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating jenny home directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing cost config: %w", err)
	}
	return nil
}

// LoadCostState loads the cost state from .jenny/config.
func LoadCostState() (*CostState, error) {
	path := costConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No config file yet
		}
		return nil, fmt.Errorf("reading cost config: %w", err)
	}
	var state CostState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshaling cost state: %w", err)
	}
	return &state, nil
}

// RestoreCostState loads cost state and restores tokens if session ID matches.
// Returns the restored CostState and a boolean indicating if restoration succeeded.
func RestoreCostState(sessionID string) (*CostState, bool, error) {
	state, err := LoadCostState()
	if err != nil {
		return nil, false, err
	}
	if state == nil {
		return nil, false, nil
	}
	// Only restore if session ID matches
	if state.LastSessionID != sessionID {
		return nil, false, nil
	}
	return state, true, nil
}

// GetModelPricing returns the pricing for a model, using conservative default for unknown models.
// The currency parameter selects the appropriate pricing table ("USD" or "CNY").
func GetModelPricing(model string, currency string) ModelPricing {
	if currency == "CNY" {
		if pricing, ok := DefaultPricingCNY[model]; ok {
			return pricing
		}
		return UnknownModelPricingCNY
	}
	if pricing, ok := DefaultPricing[model]; ok {
		return pricing
	}
	return UnknownModelPricing
}

// CalculateCost calculates the cost for given token counts using model pricing.
func CalculateCost(pricing ModelPricing, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int) float64 {
	cost := float64(inputTokens)*pricing.InputUSD +
		float64(outputTokens)*pricing.OutputUSD +
		float64(cacheReadTokens)*pricing.CacheReadUSD +
		float64(cacheCreationTokens)*pricing.CacheCreationUSD
	return cost
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

	// Always compute USD cost for backward compatibility
	pricingUSD := GetModelPricing(model, "USD")
	mu.CostUSD = CalculateCost(pricingUSD, mu.InputTokens, mu.OutputTokens, mu.CacheReadInputTokens, mu.CacheCreationInputTokens)

	// Compute CNY cost when currency is CNY
	if state.Currency == "CNY" {
		pricingCNY := GetModelPricing(model, "CNY")
		mu.CostCNY = CalculateCost(pricingCNY, mu.InputTokens, mu.OutputTokens, mu.CacheReadInputTokens, mu.CacheCreationInputTokens)
	}

	state.ModelUsage[model] = mu

	if pricingUSD.UnknownModel {
		state.HasUnknownModelCost = true
	}

	// Recalculate totals
	state.TotalCostUSD = 0
	state.TotalCostCNY = 0
	for _, m := range state.ModelUsage {
		state.TotalCostUSD += m.CostUSD
		state.TotalCostCNY += m.CostCNY
	}
}

// CheckBudgetExceeded checks if the accumulated cost exceeds the budget.
// The currency parameter is used for error message formatting.
// Returns true if budget is exceeded (should stop) and the current total cost.
func CheckBudgetExceeded(state *CostState, maxBudget float64, currency string) (bool, float64) {
	if maxBudget <= 0 {
		if currency == "CNY" {
			return false, state.TotalCostCNY
		}
		return false, state.TotalCostUSD
	}
	if currency == "CNY" {
		return state.TotalCostCNY > maxBudget, state.TotalCostCNY
	}
	return state.TotalCostUSD > maxBudget, state.TotalCostUSD
}
