// SPDX-License-Identifier: MIT

// Package integration_test — nfpm package correctness.
//
// The plan's D-7 ships two surfaces:
//
// 1. The `.goreleaser.yml` `nfpms:` block authoring (kept verbatim from
// D-1 since the schema fields were complete) — verified statically
// by TestNFPMConfig_DesiredFields below.
//
// 2. Goreleaser-snapshot-build integration that asserts the 4 produced
// packages (linux-amd64.deb + linux-amd64.rpm + linux-arm64.deb +
// linux-arm64.rpm) carry the expected metadata. Snapshot
// invocation requires the `goreleaser` CLI; on hosts lacking it
// (e.g. CI runners that only carry the test harness) we skip the
// snapshot test cleanly and rely on the static config gate.
//
// invariant (3-platform matrix) covers nfpm package presence
// implicitly via the `nfpms.builds` reference to both zen + zen-swarm-ctld
// build IDs; D-7 narrows the surface to the deb + rpm metadata schema
// itself.
//
// Build tag `integration` keeps the test out of `make test` baseline;
// invoked via `go test -tags=integration./tests/integration/`.
//
// go:build integration
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

type goreleaserConfig struct {
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
		Contents    []struct {
			Src string `yaml:"src"`
			Dst string `yaml:"dst"`
		} `yaml:"contents"`
		FileNameTemplate string `yaml:"file_name_template"`
	} `yaml:"nfpms"`
	Builds []struct {
		ID     string   `yaml:"id"`
		GOOS   []string `yaml:"goos"`
		GOArch []string `yaml:"goarch"`
		Ignore []struct {
			GOOS   string `yaml:"goos"`
			GOArch string `yaml:"goarch"`
		} `yaml:"ignore"`
	} `yaml:"builds"`
}

func repoRootForNFPM(t *testing.T) string {
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

func loadGoreleaserConfig(t *testing.T) goreleaserConfig {
	t.Helper()
	root := repoRootForNFPM(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf(".goreleaser.yml read: %v", err)
	}
	var cfg goreleaserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf(".goreleaser.yml parse: %v", err)
	}
	return cfg
}

func TestNFPMConfig_DesiredFields(t *testing.T) {
	cfg := loadGoreleaserConfig(t)
	if len(cfg.NFPMs) == 0 {
		t.Fatal("D-7: .goreleaser.yml has no nfpms: block")
	}
	pkg := cfg.NFPMs[0]
	if pkg.ID != "linux-packages" {
		t.Errorf("D-7: nfpms[0].id=%q, want 'linux-packages'", pkg.ID)
	}
	if pkg.PackageName != "zen-swarm" {
		t.Errorf("D-7: nfpms[0].package_name=%q, want 'zen-swarm'", pkg.PackageName)
	}
	wantBuilds := map[string]bool{"zen": false, "zen-swarm-ctld": false}
	for _, b := range pkg.Builds {
		if _, ok := wantBuilds[b]; ok {
			wantBuilds[b] = true
		}
	}
	for b, seen := range wantBuilds {
		if !seen {
			t.Errorf("D-7: nfpms[0].builds missing %q (only %v)", b, pkg.Builds)
		}
	}
	if pkg.License != "MIT" {
		t.Errorf("D-7: nfpms[0].license=%q, want 'MIT' (decisión 15)", pkg.License)
	}
	if pkg.Vendor != "hades-system" {
		t.Errorf("D-7: nfpms[0].vendor=%q, want 'cbip-solutions'", pkg.Vendor)
	}
	if !regexp.MustCompile(`hades-(dev|system)`).MatchString(pkg.Maintainer) {
		t.Errorf("D-7: nfpms[0].maintainer=%q, want hades-system canonical contact", pkg.Maintainer)
	}
	if pkg.BinDir != "/usr/local/bin" {
		t.Errorf("D-7: nfpms[0].bindir=%q, want '/usr/local/bin'", pkg.BinDir)
	}
	if pkg.Description == "" {
		t.Error("D-7: nfpms[0].description empty")
	}
	if pkg.Homepage != "https://github.com/cbip-solutions/hades-system" {
		t.Errorf("D-7: nfpms[0].homepage=%q, want canonical hades-system url", pkg.Homepage)
	}

	wantFormats := map[string]bool{"deb": false, "rpm": false}
	for _, f := range pkg.Formats {
		if _, ok := wantFormats[f]; ok {
			wantFormats[f] = true
		}
	}
	for f, seen := range wantFormats {
		if !seen {
			t.Errorf("D-7: nfpms[0].formats missing %q (only %v)", f, pkg.Formats)
		}
	}
	// file_name_template MUST embed both.Os and.Arch so the 4
	// produced filenames are distinct.
	if !strings.Contains(pkg.FileNameTemplate, "{{ .Os }}") || !strings.Contains(pkg.FileNameTemplate, "{{ .Arch }}") {
		t.Errorf("D-7: nfpms[0].file_name_template=%q must reference .Os + .Arch", pkg.FileNameTemplate)
	}
}

func TestNFPMConfig_LinuxArchCoverage(t *testing.T) {
	cfg := loadGoreleaserConfig(t)
	for _, b := range cfg.Builds {

		if b.ID != "zen" && b.ID != "zen-swarm-ctld" {
			continue
		}
		linuxAMD := false
		linuxARM := false
		for _, gos := range b.GOOS {
			if gos != "linux" {
				continue
			}
			for _, ga := range b.GOArch {

				ignored := false
				for _, ig := range b.Ignore {
					if ig.GOOS == "linux" && ig.GOArch == ga {
						ignored = true
						break
					}
				}
				if ignored {
					continue
				}
				switch ga {
				case "amd64":
					linuxAMD = true
				case "arm64":
					linuxARM = true
				}
			}
		}
		if !linuxAMD {
			t.Errorf("D-7: build %q does not produce linux-amd64 (nfpm needs both arches)", b.ID)
		}
		if !linuxARM {
			t.Errorf("D-7: build %q does not produce linux-arm64 (nfpm needs both arches)", b.ID)
		}
	}
}

func TestNFPMConfig_ContentsLicenseDocs(t *testing.T) {
	cfg := loadGoreleaserConfig(t)
	pkg := cfg.NFPMs[0]
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
			t.Errorf("D-7: nfpms[0].contents missing dst %q", dst)
		}
	}
}

func TestNFPMSnapshotProduces4Packages(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short mode")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		t.Skipf("goreleaser not in PATH: %v", err)
	}
	root := repoRootForNFPM(t)
	tmp := t.TempDir()

	cmd := exec.Command("goreleaser", "release",
		"--snapshot", "--clean",
		"--skip=publish", "--skip=docker", "--skip=sign",
	)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GORELEASER_CURRENT_TAG=v9.9.9-test", "DIST_OVERRIDE="+tmp)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser snapshot: %v\noutput:\n%s", err, out)
	}

	distDir := filepath.Join(root, "dist")
	if _, err := os.Stat(distDir); err != nil {
		t.Fatalf("dist/ not produced: %v", err)
	}
	debs, _ := filepath.Glob(filepath.Join(distDir, "zen-swarm-*-linux-*.deb"))
	rpms, _ := filepath.Glob(filepath.Join(distDir, "zen-swarm-*-linux-*.rpm"))
	if len(debs) != 2 {
		t.Errorf("D-7: expected 2 .deb packages, got %d: %v", len(debs), debs)
	}
	if len(rpms) != 2 {
		t.Errorf("D-7: expected 2 .rpm packages, got %d: %v", len(rpms), rpms)
	}
	// Each filename MUST carry its arch token (avoids both linux-amd64.debs
	// being produced and the linux-arm64 ones silently dropped).
	wantArch := map[string]bool{"linux-amd64": false, "linux-arm64": false}
	for _, p := range append(debs, rpms...) {
		base := filepath.Base(p)
		for arch := range wantArch {
			if strings.Contains(base, arch) {
				wantArch[arch] = true
			}
		}
	}
	for arch, seen := range wantArch {
		if !seen {
			t.Errorf("D-7: no packages carry %q in filename (deb+rpm union: %v)", arch, append(debs, rpms...))
		}
	}
}

func TestDEBMetadata(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skipf("dpkg-deb not in PATH: %v", err)
	}
	root := repoRootForNFPM(t)
	matches, _ := filepath.Glob(filepath.Join(root, "dist", "zen-swarm-*-linux-amd64.deb"))
	if len(matches) == 0 {
		t.Skip("no .deb in dist/; run snapshot first")
	}
	deb := matches[0]
	cmd := exec.Command("dpkg-deb", "--info", deb)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dpkg-deb --info: %v", err)
	}
	text := string(out)
	requiredFields := map[string]*regexp.Regexp{
		"Package":     regexp.MustCompile(`Package:\s*zen-swarm`),
		"License":     regexp.MustCompile(`MIT`),
		"Maintainer":  regexp.MustCompile(`hades-(dev|system)`),
		"Homepage":    regexp.MustCompile(`cbip-solutions/hades-system`),
		"Description": regexp.MustCompile(`Multi-project agentic development orchestrator`),
	}
	for field, re := range requiredFields {
		if !re.MatchString(text) {
			t.Errorf("D-7: .deb missing field %q in:\n%s", field, text)
		}
	}
}

func TestDEBContents(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skipf("dpkg-deb not in PATH: %v", err)
	}
	root := repoRootForNFPM(t)
	matches, _ := filepath.Glob(filepath.Join(root, "dist", "zen-swarm-*-linux-amd64.deb"))
	if len(matches) == 0 {
		t.Skip("no .deb in dist/")
	}
	deb := matches[0]
	cmd := exec.Command("dpkg-deb", "--contents", deb)
	out, _ := cmd.CombinedOutput()
	text := string(out)
	requiredPaths := []string{
		"/usr/local/bin/zen",
		"/usr/local/bin/zen-swarm-ctld",
		"/usr/share/doc/zen-swarm/LICENSE",
		"/usr/share/doc/zen-swarm/README.md",
	}
	for _, p := range requiredPaths {
		if !strings.Contains(text, p) {
			t.Errorf("D-7: .deb contents missing %q in:\n%s", p, text)
		}
	}
}

func TestRPMMetadata(t *testing.T) {
	if _, err := exec.LookPath("rpm"); err != nil {
		t.Skipf("rpm not in PATH: %v", err)
	}
	root := repoRootForNFPM(t)
	matches, _ := filepath.Glob(filepath.Join(root, "dist", "zen-swarm-*-linux-amd64.rpm"))
	if len(matches) == 0 {
		t.Skip("no .rpm in dist/")
	}
	rpm := matches[0]
	cmd := exec.Command("rpm", "-qpi", rpm)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rpm -qpi: %v", err)
	}
	text := string(out)
	requiredFields := map[string]*regexp.Regexp{
		"Name":    regexp.MustCompile(`Name\s*:\s*zen-swarm`),
		"License": regexp.MustCompile(`License\s*:\s*MIT`),
		"URL":     regexp.MustCompile(`URL\s*:\s*https://github\.com/cbip-solutions/hades-system`),
		"Summary": regexp.MustCompile(`Summary\s*:\s*Multi-project agentic development orchestrator`),
	}
	for field, re := range requiredFields {
		if !re.MatchString(text) {
			t.Errorf("D-7: .rpm missing field %q in:\n%s", field, text)
		}
	}
}
