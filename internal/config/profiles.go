// SPDX-License-Identifier: MIT
// internal/config/profiles.go
//
// ProfileConfig + LoadProfiles read profiles.toml — the role→cascade
// layer of HADES design's three-file config model. A profile is a role
// (orchestrator, worker-code, tactical, …); its Cascade is the ordered
// list of providers.toml [[providers]].name entries the dispatcher
// iterates. The [profiles.<name>] table key supplies the
// profile name — TOML map-table semantics — and is copied into
// ProfileConfig.Name by the loader.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type ProfileConfig struct {
	Name        string
	Cascade     []string `toml:"cascade"`
	Description string   `toml:"description"`
}

type profilesConfigFile struct {
	Profiles map[string]ProfileConfig `toml:"profiles"`
}

func LoadProfiles(path string) (map[string]ProfileConfig, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config.LoadProfiles(%s): %w", path, err)
	}
	var doc profilesConfigFile
	if err := toml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("config.LoadProfiles(%s): toml: %w", path, err)
	}
	out := make(map[string]ProfileConfig, len(doc.Profiles))
	for name, p := range doc.Profiles {
		if len(p.Cascade) == 0 {
			return nil, fmt.Errorf("config.LoadProfiles(%s): profile %q has an empty cascade", path, name)
		}
		p.Name = name
		out[name] = p
	}
	return out, nil
}
