// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGoreleaserSBOMsBlockPresent(t *testing.T) {
	root := findRepoRootPhaseE(t)
	path := filepath.Join(root, ".goreleaser.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var cfg struct {
		Version int `yaml:"version"`
		SBOMs   []struct {
			ID        string   `yaml:"id"`
			Artifacts string   `yaml:"artifacts"`
			Cmd       string   `yaml:"cmd"`
			Args      []string `yaml:"args"`
		} `yaml:"sboms"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}

	if cfg.Version != 2 {
		t.Fatalf("expected GoReleaser version=2, got %d", cfg.Version)
	}

	wantIDs := map[string]bool{
		"cyclonedx":      false,
		"spdx-dual-emit": false,
	}
	for _, s := range cfg.SBOMs {
		if _, ok := wantIDs[s.ID]; ok {
			wantIDs[s.ID] = true
		}

		switch s.Artifacts {
		case "archive", "all", "binary", "package":

		default:
			t.Errorf("sbom %s: unsupported artifacts=%q", s.ID, s.Artifacts)
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing required sbom emitter id=%q", id)
		}
	}
}

func TestGoreleaserSBOMMergeHookConfigured(t *testing.T) {
	root := findRepoRootPhaseE(t)
	path := filepath.Join(root, ".goreleaser.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var cfg struct {
		After struct {
			Hooks []struct {
				Cmd    string   `yaml:"cmd"`
				Env    []string `yaml:"env"`
				Output bool     `yaml:"output"`
			} `yaml:"hooks"`
		} `yaml:"after"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}

	found := false
	for _, h := range cfg.After.Hooks {
		if h.Cmd != "" && (containsPhaseE(h.Cmd, "sbom-merge-supplement") || containsPhaseE(h.Cmd, "verify-cgo-supplement")) {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing supplement-merge post-hook in .goreleaser.yml after.hooks (expected cmd referencing sbom-merge-supplement.sh)")
	}
}
