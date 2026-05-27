// SPDX-License-Identifier: MIT
// Package config wraps reading/writing the various toml/json config
// files that live under configs/ + ~/.config/hades-system/.
package config

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type ProjectConfig struct {
	Path               string
	Execution          string
	AuthoritativeGit   string
	VPSEndpoint        string
	VPSAllowedCommands []string
	Doctrine           string
	BudgetMonthlyUSD   float64
	PriorityWeight     int
}

func LoadProjects(path string) (map[string]ProjectConfig, error) {
	return nil, zerrors.ErrNotImplementedPlan7
}

func SaveProjects(path string, projects map[string]ProjectConfig) error {
	return zerrors.ErrNotImplementedPlan7
}
