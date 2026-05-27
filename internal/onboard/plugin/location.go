// SPDX-License-Identifier: MIT
package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type LocationKind int

const (
	LocationKindUnknown LocationKind = iota

	LocationKindProjectScope

	LocationKindUserScope
)

func (k LocationKind) String() string {
	switch k {
	case LocationKindProjectScope:
		return "project-scope"
	case LocationKindUserScope:
		return "user-scope"
	default:
		return "unknown"
	}
}

type Location struct {
	Path string
	Kind LocationKind
}

var ErrRepoRootMissing = errors.New("plugin: project root not determinable")

func ResolveLocation(spikeOutcome bool) (Location, error) {
	repoRoot, err := repoRootDir()
	if err != nil {
		return Location{}, fmt.Errorf("resolve project root: %w", err)
	}

	if spikeOutcome {
		return Location{
			Path: filepath.Join(repoRoot, ".hermes", "plugins", "hades-system"),
			Kind: LocationKindProjectScope,
		}, nil
	}

	home, err := userHomeDir()
	if err != nil {
		return Location{}, fmt.Errorf("resolve home: %w", err)
	}
	slug := Slug(repoRoot)
	return Location{
		Path: filepath.Join(home, ".hermes", "plugins", "hades-system-"+slug),
		Kind: LocationKindUserScope,
	}, nil
}

func repoRootDir() (string, error) {
	if override := os.Getenv("HADES_REPO_ROOT_OVERRIDE"); override != "" {
		return override, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRepoRootMissing, err)
	}
	return cwd, nil
}

func userHomeDir() (string, error) {
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return home, nil
}
