// SPDX-License-Identifier: MIT
// Package monorepo implements walk-UP workspace detection per spec §2.4 Q4=B
// + SOTA-3 #4. Walks from a starting absolute path toward filesystem root,
// stopping at either the .git boundary or root. Returns the first matching
// workspace marker per the priority order:
//
//  1. pnpm-workspace.yaml
//  2. turbo.json
//  3. nx.json
//  4. rush.json
//  5. lerna.json
//  6. Cargo.toml with [workspace] table
//  7. go.work
//  8. BUILD.bazel + MODULE.bazel (both required)
//  9. pants.toml
//
// Rationale (spec §2.4 + SOTA-3 #4): when operator runs zen recognize inside
// e.g. apps/web/ of a monorepo, recognize would mis-classify the inner package
// as the whole project. Walk-UP first identifies the workspace root so the
// orchestrator can position its FS scope correctly.
package monorepo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Workspace struct {
	Root        string
	Tool        string
	ConfigPath  string
	GitBoundary string
}

var ErrRelativePath = errors.New("monorepo: WalkUp requires an absolute path")

type detector struct {
	Tool   string
	Detect func(dir string) (Workspace, bool)
}

var detectors = []detector{
	{Tool: "pnpm", Detect: simpleFileDetector("pnpm", "pnpm-workspace.yaml")},
	{Tool: "turbo", Detect: simpleFileDetector("turbo", "turbo.json")},
	{Tool: "nx", Detect: simpleFileDetector("nx", "nx.json")},
	{Tool: "rush", Detect: simpleFileDetector("rush", "rush.json")},
	{Tool: "lerna", Detect: simpleFileDetector("lerna", "lerna.json")},
	{Tool: "cargo", Detect: cargoWorkspaceDetector},
	{Tool: "go-work", Detect: simpleFileDetector("go-work", "go.work")},
	{Tool: "bazel", Detect: bazelDetector},
	{Tool: "pants", Detect: simpleFileDetector("pants", "pants.toml")},
}

func simpleFileDetector(tool, filename string) func(dir string) (Workspace, bool) {
	return func(dir string) (Workspace, bool) {
		candidate := filepath.Join(dir, filename)
		if fileExists(candidate) {
			return Workspace{Root: dir, Tool: tool, ConfigPath: candidate}, true
		}
		return Workspace{}, false
	}
}

func cargoWorkspaceDetector(dir string) (Workspace, bool) {
	candidate := filepath.Join(dir, "Cargo.toml")
	if !fileExists(candidate) {
		return Workspace{}, false
	}
	buf, err := os.ReadFile(candidate)
	if err != nil {
		return Workspace{}, false
	}

	for _, line := range strings.Split(string(buf), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[workspace]") || strings.HasPrefix(line, "[workspace.") {
			return Workspace{Root: dir, Tool: "cargo", ConfigPath: candidate}, true
		}
	}
	return Workspace{}, false
}

func bazelDetector(dir string) (Workspace, bool) {
	build := filepath.Join(dir, "BUILD.bazel")
	module := filepath.Join(dir, "MODULE.bazel")
	if fileExists(build) && fileExists(module) {
		return Workspace{Root: dir, Tool: "bazel", ConfigPath: module}, true
	}
	return Workspace{}, false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dirHasGit(dir string) bool {
	p := filepath.Join(dir, ".git")
	_, err := os.Stat(p)
	return err == nil
}

// WalkUp walks from absPath up to filesystem root or .git boundary,
// returning the first workspace match. Zero-value Workspace{} when none found.
//
// Per spec §2.4 + SOTA-3 #4. PRECONDITION: absPath MUST be absolute.
func WalkUp(absPath string) (Workspace, error) {
	if !filepath.IsAbs(absPath) {
		return Workspace{}, ErrRelativePath
	}
	info, err := os.Stat(absPath)
	var current string
	if err == nil && info.IsDir() {
		current = absPath
	} else {
		current = filepath.Dir(absPath)
	}

	gitBoundary := ""
	for {

		if dirHasGit(current) {
			gitBoundary = current

			for _, d := range detectors {
				if ws, ok := d.Detect(current); ok {
					ws.GitBoundary = gitBoundary
					return ws, nil
				}
			}

			return Workspace{GitBoundary: gitBoundary}, nil
		}

		for _, d := range detectors {
			if ws, ok := d.Detect(current); ok {
				return ws, nil
			}
		}

		parent := filepath.Dir(current)
		if parent == current {

			return Workspace{GitBoundary: gitBoundary}, nil
		}
		current = parent
	}
}
