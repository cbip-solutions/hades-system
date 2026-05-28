// SPDX-License-Identifier: MIT
// internal/config/providers.go
//
// LoadProviders reads the [[providers]] array from providers.toml into
// a []providers.ProviderConfig. It is the HADES design loader that
// fills the HADES design gap: before HADES design the ProviderConfig schema
// existed (internal/providers/registry.go) but nothing parsed the array.
//
// The companion [[rate_cards]] array in the same file is read by the
// existing providers.RateCardRegistry.LoadFromConfig — this loader does
// not duplicate it.
//
// invariant: every entry is Validate()-checked before the slice is
// returned. A single malformed entry fails the whole load — a partial
// slice would leak an operator typo into the registry build.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

type providersConfigFile struct {
	Providers []providers.ProviderConfig `toml:"providers"`
}

func LoadProviders(path string) ([]providers.ProviderConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {

		return nil, fmt.Errorf("config.LoadProviders(%s): %w", path, err)
	}
	var doc providersConfigFile
	if err := toml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("config.LoadProviders(%s): toml: %w", path, err)
	}
	seen := make(map[string]struct{}, len(doc.Providers))
	for i := range doc.Providers {
		cfg := doc.Providers[i]
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("config.LoadProviders(%s): %w", path, err)
		}
		if _, dup := seen[cfg.Name]; dup {
			return nil, fmt.Errorf("config.LoadProviders(%s): duplicate provider name %q", path, cfg.Name)
		}
		seen[cfg.Name] = struct{}{}
	}
	return doc.Providers, nil
}
