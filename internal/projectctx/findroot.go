// SPDX-License-Identifier: MIT
// findroot.go — release Task A-8 helper.
//
// FindProjectRoot walks up the filesystem from `start` looking for an
// ancestor directory that contains EITHER `hadessystem.toml` OR a `.git`
// entry (file or directory). Returns the canonical path of the first
// ancestor that matches, or an error if neither marker is found before
// reaching the filesystem root.
//
// Used by the CLI doctor subcommand (Task A-8) when the operator runs
// `hades project doctor` (no alias arg) from a subdirectory of a project:
// the daemon needs the project root to compute the canonical sha256
// project_id consistently regardless of which subdirectory the operator
// is sitting in.
//
// Boundary discipline (inv-hades-031): pure stdlib (os, path/filepath,
// errors, fmt). No internal/store imports; no projectctxadapter
// dependency.
package projectctx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrNoProjectRoot = errors.New("projectctx: no project root found (no hadessystem.toml or .git ancestor)")

func FindProjectRoot(start string) (string, error) {
	if start == "" {
		return "", ErrEmptyPath
	}
	canonical, err := CanonicalPath(start)
	if err != nil {
		return "", fmt.Errorf("projectctx.FindProjectRoot: %w", err)
	}
	current := canonical
	for {
		if hasMarker(current) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {

			return "", fmt.Errorf("%w: starting from %q", ErrNoProjectRoot, canonical)
		}
		current = parent
	}
}

func hasMarker(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "hadessystem.toml")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	return false
}
