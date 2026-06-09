// Package agent provides cost calculation and persistence.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/constants"
)

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
