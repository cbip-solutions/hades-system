// SPDX-License-Identifier: MIT
// internal/config/routing.go
//
// RoutingConfig + LoadRouting read routing.toml — the optional
// task-kind→profile rule layer. LoadRouting fills the
// ErrNotImplementedPlan3 stub. The rules are consumed by the
// orchestrator when it maps an incoming task to a profile name before
// calling the ProfileResolver (resolver.go).
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type RoutingConfig struct {
	Default string        `toml:"default"`
	Rules   []RoutingRule `toml:"rules"`
}

type RoutingRule struct {
	When    string `toml:"when"`
	Profile string `toml:"profile"`
}

func LoadRouting(path string) (*RoutingConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config.LoadRouting(%s): %w", path, err)
	}
	var cfg RoutingConfig
	if err := toml.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("config.LoadRouting(%s): toml: %w", path, err)
	}
	return &cfg, nil
}
