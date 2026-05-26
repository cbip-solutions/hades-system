// SPDX-License-Identifier: MIT

// Package integration_test — Plan 15 Phase D-8 (operator) brew Formula
// correctness.
//
// Three verification surfaces:
//
//  1. Static config gate: .goreleaser.yml brews[0] block declares the
//     canonical fields (name, repository, license, dependencies, caveats
//     sentinels). Runs in this file always; no external tooling needed.
//
//  2. Live mirror: Formula/hades.rb (the local mirror referenced by
//     scripts/verify_brew_formula.sh baseline) contains the load-bearing
//     keywords + class shape. Mirrors the shell-script lint via Go-level
//     parsing so a typo in Formula/hades.rb fails this test alongside the
//     shell lint.
//
//  3. (Optional) Snapshot-time integration: when goreleaser is available
//     locally, run a snapshot build and inspect the would-be Formula at
//     dist/homebrew/Formula/hades.rb. Asserts ldflag-injected version +
//     url + sha256 + dependencies + caveats sentinels are all rendered.
//
// All tests skip cleanly when goreleaser is absent (CI dev hosts) so the
// gates that DON'T need it stay enforced.
//
//go:build integration

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type brewsConfig struct {
	Brews []struct {
		Name       string `yaml:"name"`
		Repository struct {
			Owner  string `yaml:"owner"`
			Name   string `yaml:"name"`
			Branch string `yaml:"branch"`
		} `yaml:"repository"`
		Directory    string `yaml:"directory"`
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

func repoRootForBrew(t *testing.T) string {
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

func TestBrewsConfig_DesiredFields(t *testing.T) {
	root := repoRootForBrew(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf(".goreleaser.yml read: %v", err)
	}
	var cfg brewsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(".goreleaser.yml parse: %v", err)
	}
	if len(cfg.Brews) == 0 {
		t.Fatal("D-8: .goreleaser.yml has no brews: block")
	}
	b := cfg.Brews[0]
	if b.Name != "hades" {
		t.Errorf("D-8: brews[0].name=%q, want 'hades' (Formula filename + class)", b.Name)
	}
	if b.Repository.Owner != "hades-system" {
		t.Errorf("D-8: brews[0].repository.owner=%q, want 'hades-system'", b.Repository.Owner)
	}
	if b.Repository.Name != "homebrew-tap" {
		t.Errorf("D-8: brews[0].repository.name=%q, want 'homebrew-tap'", b.Repository.Name)
	}
	if b.License != "MIT" {
		t.Errorf("D-8: brews[0].license=%q, want 'MIT' (decisión 15)", b.License)
	}
	if b.Homepage != "https://github.com/cbip-solutions/hades-system" {
		t.Errorf("D-8: brews[0].homepage=%q, want canonical hades-system URL", b.Homepage)
	}
	if b.Description == "" {
		t.Error("D-8: brews[0].description empty")
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
		t.Error("D-8: brews[0].dependencies missing hermes-agent required")
	}
	if !tmuxSeen {
		t.Error("D-8: brews[0].dependencies missing tmux recommended")
	}

	if !strings.Contains(b.Test, "#{bin}/zen") || !strings.Contains(b.Test, "#{bin}/zen-swarm-ctld") {
		t.Errorf("D-8: brews[0].test missing #{bin}/zen + #{bin}/zen-swarm-ctld --version invocations:\n%s", b.Test)
	}

	caveatsRequired := []string{
		"hermes-agent",
		"zen daemon install",
		"zen doctor",
		"zen providers add",
		"hermes plugin list",
		"MIT",
		"Caronte",
	}
	for _, want := range caveatsRequired {
		if !strings.Contains(b.Caveats, want) {
			t.Errorf("D-8: brews[0].caveats missing required guidance: %q", want)
		}
	}

	forbidden := []string{
		"Co-Authored-By: prohibited assistant",
		"Generated with prohibited assistant",
	}
	for _, bad := range forbidden {
		if strings.Contains(b.Caveats, bad) {
			t.Errorf("D-8: brews[0].caveats contains forbidden Claude attribution: %q", bad)
		}
	}
}

func TestLocalFormulaMirror_LoadBearingKeywords(t *testing.T) {
	root := repoRootForBrew(t)
	path := filepath.Join(root, "Formula", "hades.rb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("D-8: read local Formula mirror %s: %v", path, err)
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
			t.Errorf("D-8: Formula/hades.rb missing required content: %q", want)
		}
	}

	forbidden := []string{
		"gitnexus",
		"depends_on \"gitnexus\"",
		"GITNEXUS",
	}
	for _, bad := range forbidden {
		if strings.Contains(text, bad) {
			t.Errorf("D-8: Formula/hades.rb contains retired reference: %q (decisión 6: Caronte sovereign)", bad)
		}
	}
}

func TestBrewFormulaSnapshotTemplateRender(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short mode")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skipf("goreleaser not in PATH: %v", err)
	}
	root := repoRootForBrew(t)

	cmd := exec.Command("goreleaser", "release",
		"--snapshot", "--clean",
		"--skip=publish", "--skip=docker", "--skip=sign",
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GORELEASER_CURRENT_TAG=v9.9.9-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("goreleaser snapshot: %v\noutput:\n%s", err, out)
	}

	matches, _ := filepath.Glob(filepath.Join(root, "dist", "homebrew", "Formula", "hades.rb"))
	if len(matches) == 0 {
		t.Fatalf("no dist/homebrew/Formula/hades.rb produced by snapshot")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read snapshot Formula: %v", err)
	}
	text := string(data)
	required := []*regexp.Regexp{
		regexp.MustCompile(`class Hades < Formula`),
		regexp.MustCompile(`license "MIT"`),
		regexp.MustCompile(`url "https://github\.com/cbip-solutions/hades-system/releases/download/`),
		regexp.MustCompile(`sha256 "[0-9a-f]{64}"`),
		regexp.MustCompile(`depends_on "hermes-agent"`),
		regexp.MustCompile(`depends_on "tmux" => :recommended`),
		regexp.MustCompile(`version "9\.9\.9-test"`),
	}
	for _, re := range required {
		if !re.MatchString(text) {
			t.Errorf("D-8: snapshot Formula missing pattern %q", re.String())
		}
	}

	caveatsRequired := []string{
		"hermes-agent",
		"zen daemon install",
		"zen doctor",
		"zen providers add",
		"MIT",
	}
	for _, want := range caveatsRequired {
		if !strings.Contains(text, want) {
			t.Errorf("D-8: snapshot Formula caveats missing: %q", want)
		}
	}
}

func TestBrewFormulaAuditStrict(t *testing.T) {
	if _, err := exec.LookPath("brew"); err != nil {
		t.Skipf("brew not in PATH: %v", err)
	}
	root := repoRootForBrew(t)
	path := filepath.Join(root, "Formula", "hades.rb")
	cmd := exec.Command("brew", "style", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("brew style on Formula/hades.rb failed:\n%s", out)
	}
}
