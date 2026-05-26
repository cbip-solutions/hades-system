// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRootInvZen294Brew(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for d := wd; d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatalf("repo root not found from %s", wd)
	return ""
}

type brewsConfigCompliance struct {
	Brews []struct {
		Name       string `yaml:"name"`
		Repository struct {
			Owner string `yaml:"owner"`
			Name  string `yaml:"name"`
		} `yaml:"repository"`
		Homepage     string `yaml:"homepage"`
		Description  string `yaml:"description"`
		License      string `yaml:"license"`
		Dependencies []struct {
			Name string `yaml:"name"`
			Type string `yaml:"type"`
		} `yaml:"dependencies"`
		Test    string `yaml:"test"`
		Caveats string `yaml:"caveats"`
	} `yaml:"brews"`
}

func TestInvZen294_BrewsConfigShape(t *testing.T) {
	root := repoRootInvZen294Brew(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-294 (brew): read .goreleaser.yml: %v", err)
	}
	var cfg brewsConfigCompliance
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-294 (brew): parse: %v", err)
	}
	if len(cfg.Brews) == 0 {
		t.Fatal("inv-zen-294 (brew) VIOLATED: .goreleaser.yml has no brews: block")
	}
	b := cfg.Brews[0]
	if b.Name != "hades" {
		t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].name=%q, want 'hades' (matches Formula/hades.rb + class Hades)", b.Name)
	}
	if b.Repository.Owner != "hades-system" {
		t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].repository.owner=%q, want 'hades-system'", b.Repository.Owner)
	}
	if b.Repository.Name != "homebrew-tap" {
		t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].repository.name=%q, want 'homebrew-tap'", b.Repository.Name)
	}
	if b.License != "MIT" {
		t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].license=%q, want 'MIT' (decisión 15)", b.License)
	}
	if b.Homepage != "https://github.com/cbip-solutions/hades-system" {
		t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].homepage=%q, want canonical hades-system URL", b.Homepage)
	}
	if b.Description == "" {
		t.Error("inv-zen-294 (brew) VIOLATED: brews[0].description empty")
	}

	hermesSeen := false
	tmuxSeen := false
	for _, d := range b.Dependencies {
		if d.Name == "hermes-agent" && d.Type == "required" {
			hermesSeen = true
		}
		if d.Name == "tmux" && d.Type == "recommended" {
			tmuxSeen = true
		}
	}
	if !hermesSeen {
		t.Error("inv-zen-294 (brew) VIOLATED: brews[0].dependencies missing hermes-agent required")
	}
	if !tmuxSeen {
		t.Error("inv-zen-294 (brew) VIOLATED: brews[0].dependencies missing tmux recommended")
	}
	if !strings.Contains(b.Test, "#{bin}/zen") || !strings.Contains(b.Test, "#{bin}/zen-swarm-ctld") {
		t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].test must exec both binaries; got:\n%s", b.Test)
	}

	caveatsRequired := []string{
		"hermes-agent",
		"zen daemon install",
		"zen doctor",
		"zen providers add",
		"MIT",
		"Caronte",
	}
	for _, want := range caveatsRequired {
		if !strings.Contains(b.Caveats, want) {
			t.Errorf("inv-zen-294 (brew) VIOLATED: brews[0].caveats missing %q", want)
		}
	}
}

func TestInvZen294_FormulaMirrorShape(t *testing.T) {
	root := repoRootInvZen294Brew(t)
	path := filepath.Join(root, "Formula", "hades.rb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-294 (brew) VIOLATED: read Formula/hades.rb: %v", err)
	}
	text := string(data)
	required := []string{
		`class Hades < Formula`,
		`license "MIT"`,
		`depends_on "hermes-agent"`,
		`Caronte`,
		`Hermes Agent`,
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Errorf("inv-zen-294 (brew) VIOLATED: Formula/hades.rb missing %q", want)
		}
	}

	forbidden := []string{
		`depends_on "gitnexus"`,
		"GITNEXUS",
	}
	for _, bad := range forbidden {
		if strings.Contains(text, bad) {
			t.Errorf("inv-zen-294 (brew) VIOLATED: Formula/hades.rb contains retired reference: %q (decisión 6: Caronte sovereign)", bad)
		}
	}
}

func TestInvZen294_VerifyBrewFormulaScriptShape(t *testing.T) {
	root := repoRootInvZen294Brew(t)
	path := filepath.Join(root, "scripts", "verify_brew_formula.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-294 (brew) VIOLATED: read %s: %v", path, err)
	}
	text := string(data)

	for _, want := range []string{"baseline", "post-release", "TAP_URL", "RELEASES_BASE"} {
		if !strings.Contains(text, want) {
			t.Errorf("inv-zen-294 (brew) VIOLATED: verify_brew_formula.sh missing token %q (post-release extension not wired)", want)
		}
	}

	for _, want := range []string{
		"FORMULA_VERSION",
		"FORMULA_URL",
		"FORMULA_SHA",
		"ACTUAL_SHA",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("inv-zen-294 (brew) VIOLATED: verify_brew_formula.sh missing assertion variable %q", want)
		}
	}

	info, err := os.Stat(path)
	if err == nil && info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-294 (brew) VIOLATED: verify_brew_formula.sh not executable (mode %v)", info.Mode())
	}
}
