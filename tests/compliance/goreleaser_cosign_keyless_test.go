// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGoreleaserCosignKeylessCoverage(t *testing.T) {
	root := findRepoRootPhaseE(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		Signs []struct {
			ID        string   `yaml:"id"`
			Cmd       string   `yaml:"cmd"`
			Args      []string `yaml:"args"`
			Artifacts string   `yaml:"artifacts"`
			Env       []string `yaml:"env"`
		} `yaml:"signs"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	wantIDs := map[string]string{
		"cosign-keyless":          "archive",
		"cosign-keyless-checksum": "checksum",
		"cosign-keyless-sbom":     "sbom",
	}
	got := map[string]bool{}

	for _, s := range cfg.Signs {
		want, ok := wantIDs[s.ID]
		if !ok {
			continue
		}
		got[s.ID] = true

		if s.Cmd != "cosign" {
			t.Errorf("%s: cmd want 'cosign', got %q", s.ID, s.Cmd)
		}
		if s.Artifacts != want {
			t.Errorf("%s: artifacts want %q, got %q", s.ID, want, s.Artifacts)
		}

		hasSignBlob := false
		hasYes := false
		hasOutputSig := false
		hasOutputCert := false
		hasArtifactPlaceholder := false
		for _, a := range s.Args {
			switch {
			case a == "sign-blob":
				hasSignBlob = true
			case a == "--yes":
				hasYes = true
			case strings.Contains(a, "${signature}"):
				hasOutputSig = true
			case strings.Contains(a, "${certificate}"):
				hasOutputCert = true
			case strings.Contains(a, "${artifact}"):
				hasArtifactPlaceholder = true
			}
		}
		if !hasSignBlob || !hasYes || !hasOutputSig || !hasOutputCert || !hasArtifactPlaceholder {
			t.Errorf("%s args incomplete: sign-blob=%v --yes=%v ${signature}=%v ${certificate}=%v ${artifact}=%v",
				s.ID, hasSignBlob, hasYes, hasOutputSig, hasOutputCert, hasArtifactPlaceholder)
		}

		hasExperimental := false
		for _, e := range s.Env {
			if e == "COSIGN_EXPERIMENTAL=1" {
				hasExperimental = true
				break
			}
		}
		if !hasExperimental {
			t.Errorf("%s env missing COSIGN_EXPERIMENTAL=1", s.ID)
		}
	}

	for id := range wantIDs {
		if !got[id] {
			t.Errorf("signs: missing required cosign-keyless entry: %s", id)
		}
	}
}

func TestGoreleaserCosignKeylessNotSigningPackages(t *testing.T) {
	root := findRepoRootPhaseE(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		Signs []struct {
			ID        string `yaml:"id"`
			Artifacts string `yaml:"artifacts"`
		} `yaml:"signs"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	for _, s := range cfg.Signs {
		if !strings.HasPrefix(s.ID, "cosign-keyless") {
			continue
		}
		if s.Artifacts == "all" || s.Artifacts == "package" {
			t.Errorf("%s: artifacts=%q would also sign nfpm packages (have own GPG standard); use the 3-way split instead",
				s.ID, s.Artifacts)
		}
	}
}
