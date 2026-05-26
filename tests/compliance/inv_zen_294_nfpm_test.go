// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRootInvZen294NFPM(t *testing.T) string {
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

type nfpmConfig struct {
	NFPMs []struct {
		ID          string   `yaml:"id"`
		PackageName string   `yaml:"package_name"`
		Builds      []string `yaml:"builds"`
		Vendor      string   `yaml:"vendor"`
		Homepage    string   `yaml:"homepage"`
		Maintainer  string   `yaml:"maintainer"`
		Description string   `yaml:"description"`
		License     string   `yaml:"license"`
		Formats     []string `yaml:"formats"`
		BinDir      string   `yaml:"bindir"`
		Section     string   `yaml:"section"`
		Priority    string   `yaml:"priority"`
		Contents    []struct {
			Src string `yaml:"src"`
			Dst string `yaml:"dst"`
		} `yaml:"contents"`
		FileNameTemplate string `yaml:"file_name_template"`
	} `yaml:"nfpms"`
}

func loadNFPMConfig(t *testing.T) nfpmConfig {
	t.Helper()
	root := repoRootInvZen294NFPM(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-294 (nfpm): read: %v", err)
	}
	var cfg nfpmConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-294 (nfpm): parse: %v", err)
	}
	return cfg
}

func TestInvZen294_NFPMConfigShape(t *testing.T) {
	cfg := loadNFPMConfig(t)
	if len(cfg.NFPMs) == 0 {
		t.Fatal("inv-zen-294 (nfpm) VIOLATED: .goreleaser.yml has no nfpms: block")
	}
	pkg := cfg.NFPMs[0]
	if pkg.PackageName != "zen-swarm" {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: package_name=%q, want 'zen-swarm'", pkg.PackageName)
	}
	if pkg.License != "MIT" {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: license=%q, want 'MIT' (decisión 15)", pkg.License)
	}
	if pkg.Vendor != "hades-system" {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: vendor=%q, want 'hades-system'", pkg.Vendor)
	}
	if !regexp.MustCompile(`hades-(dev|system)`).MatchString(pkg.Maintainer) {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: maintainer=%q, want hades-system canonical contact", pkg.Maintainer)
	}
	if pkg.BinDir != "/usr/local/bin" {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: bindir=%q, want '/usr/local/bin'", pkg.BinDir)
	}
	if pkg.Homepage != "https://github.com/cbip-solutions/hades-system" {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: homepage=%q, want canonical hades-system URL", pkg.Homepage)
	}
	if pkg.Description == "" {
		t.Error("inv-zen-294 (nfpm) VIOLATED: description empty")
	}
	if !strings.Contains(pkg.FileNameTemplate, "{{ .Os }}") || !strings.Contains(pkg.FileNameTemplate, "{{ .Arch }}") {
		t.Errorf("inv-zen-294 (nfpm) VIOLATED: file_name_template=%q must reference .Os + .Arch", pkg.FileNameTemplate)
	}

	wantBuilds := map[string]bool{"zen": false, "zen-swarm-ctld": false}
	for _, b := range pkg.Builds {
		if _, ok := wantBuilds[b]; ok {
			wantBuilds[b] = true
		}
	}
	for b, seen := range wantBuilds {
		if !seen {
			t.Errorf("inv-zen-294 (nfpm) VIOLATED: builds missing %q", b)
		}
	}

	wantFormats := map[string]bool{"deb": false, "rpm": false}
	for _, f := range pkg.Formats {
		if _, ok := wantFormats[f]; ok {
			wantFormats[f] = true
		}
	}
	for f, seen := range wantFormats {
		if !seen {
			t.Errorf("inv-zen-294 (nfpm) VIOLATED: formats missing %q", f)
		}
	}

	wantContents := map[string]bool{
		"/usr/share/doc/zen-swarm/LICENSE":   false,
		"/usr/share/doc/zen-swarm/README.md": false,
	}
	for _, c := range pkg.Contents {
		if _, ok := wantContents[c.Dst]; ok {
			wantContents[c.Dst] = true
		}
	}
	for dst, seen := range wantContents {
		if !seen {
			t.Errorf("inv-zen-294 (nfpm) VIOLATED: contents missing dst %q", dst)
		}
	}
}
