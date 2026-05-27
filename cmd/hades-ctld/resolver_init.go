// SPDX-License-Identifier: MIT
package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cbip-solutions/hades-system/internal/config"
)

func buildProfileResolver(providersDir string) (*config.ProfileResolver, error) {
	profiles, err := config.LoadProfiles(filepath.Join(providersDir, "profiles.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	var defaultProfile string
	routing, rerr := config.LoadRouting(filepath.Join(providersDir, "routing.toml"))
	if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return nil, rerr
	}
	if routing != nil {
		defaultProfile = routing.Default
	}

	return config.NewProfileResolver(config.ProfileResolverLayers{
		Profiles:        profiles,
		CheckoutProfile: defaultProfile,
	}), nil
}
