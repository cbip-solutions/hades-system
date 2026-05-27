// SPDX-License-Identifier: MIT
// internal/config/orchestrator.go
//
// OrchestratorConfig + LoadOrchestrator read the per-project
// [projects.<id>.orchestrator] sub-table from projects.toml — the
// per-project override layer of three-file config model.
// LoadOrchestrator fills the ErrNotImplementedPlan3 stub.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type OrchestratorConfig struct {
	Default              string           `toml:"default"`
	FallbackChain        []string         `toml:"fallback_chain"`
	AutoFallbackToPAYGO  bool             `toml:"auto_fallback_to_paygo"`
	AutoFallbackToGemini bool             `toml:"auto_fallback_to_gemini"`
	AllowProviders       []string         `toml:"allow_providers"`
	PAYGSafety           PAYGSafetyConfig `toml:"payg_safety"`
}

type PAYGSafetyConfig struct {
	APIKeySource     string  `toml:"api_key_source"`
	PerSessionCapUSD float64 `toml:"per_session_cap_usd"`
	PerDayCapUSD     float64 `toml:"per_day_cap_usd"`
	PerMonthCapUSD   float64 `toml:"per_month_cap_usd"`
	NotifyAtPercent  []int   `toml:"notify_at_percent"`
	AutoPauseAtCap   bool    `toml:"auto_pause_at_cap"`
}

type orchestratorProjectsFile struct {
	Projects map[string]struct {
		Orchestrator OrchestratorConfig `toml:"orchestrator"`
	} `toml:"projects"`
}

func LoadOrchestrator(projectsTOMLPath, project string) (*OrchestratorConfig, error) {
	body, err := os.ReadFile(projectsTOMLPath)
	if err != nil {
		return nil, fmt.Errorf("config.LoadOrchestrator(%s): %w", projectsTOMLPath, err)
	}
	var doc orchestratorProjectsFile
	if err := toml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("config.LoadOrchestrator(%s): toml: %w", projectsTOMLPath, err)
	}
	entry, ok := doc.Projects[project]
	if !ok {
		return nil, fmt.Errorf("config.LoadOrchestrator(%s): no project %q", projectsTOMLPath, project)
	}
	cfg := entry.Orchestrator
	return &cfg, nil
}
