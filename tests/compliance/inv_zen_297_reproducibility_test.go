// SPDX-License-Identifier: MIT

package compliance_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/buildinfo"
	"gopkg.in/yaml.v3"
)

func repoRootInvZen297(t *testing.T) string {
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

func TestInvZen297_BuildinfoPackagePresent(t *testing.T) {
	root := repoRootInvZen297(t)
	pkgFile := filepath.Join(root, "internal", "buildinfo", "buildinfo.go")
	data, err := os.ReadFile(pkgFile)
	if err != nil {
		t.Fatalf("inv-zen-297 VIOLATED: internal/buildinfo/buildinfo.go missing: %v", err)
	}
	src := string(data)
	requiredSymbols := []string{
		"func Version() string",
		"func Commit() string",
		"func Date() string",
		"func GoVersion() string",
		"func Platform() string",
		"func Summary() string",
		"func Provenance() map[string]string",
	}
	for _, sym := range requiredSymbols {
		if !strings.Contains(src, sym) {
			t.Errorf("inv-zen-297 VIOLATED: internal/buildinfo/buildinfo.go missing symbol %q", sym)
		}
	}
}

func TestInvZen297_BuildinfoProvenanceKeys(t *testing.T) {
	wantKeys := []string{
		"buildinfo.version",
		"buildinfo.commit",
		"buildinfo.date",
		"buildinfo.go_version",
		"buildinfo.platform",
	}
	p := buildinfo.Provenance()
	if len(p) != len(wantKeys) {
		t.Errorf("inv-zen-297 VIOLATED: Provenance() map size=%d, want %d", len(p), len(wantKeys))
	}
	for _, k := range wantKeys {
		if _, ok := p[k]; !ok {
			t.Errorf("inv-zen-297 VIOLATED: Provenance() missing key %q", k)
		}
	}
}

func TestInvZen297_BuildinfoLDFlagsPresent(t *testing.T) {
	root := repoRootInvZen297(t)
	data, err := os.ReadFile(filepath.Join(root, ".goreleaser.yml"))
	if err != nil {
		t.Fatalf("inv-zen-297: read .goreleaser.yml: %v", err)
	}
	var cfg struct {
		Builds []struct {
			ID      string   `yaml:"id"`
			LDFlags []string `yaml:"ldflags"`
		} `yaml:"builds"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("inv-zen-297: parse: %v", err)
	}
	require := []string{
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.version=",
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.commit=",
		"-X github.com/cbip-solutions/hades-system/internal/buildinfo.date=",
	}
	for _, build := range cfg.Builds {
		joined := strings.Join(build.LDFlags, " ")
		for _, want := range require {
			if !strings.Contains(joined, want) {
				t.Errorf("inv-zen-297 VIOLATED: build %q ldflags missing %q in %q",
					build.ID, want, joined)
			}
		}
	}
}

func TestInvZen297_VerifyReleaseChecksumsPresent(t *testing.T) {
	root := repoRootInvZen297(t)
	paths := []string{
		"cmd/verify-release-checksums/main.go",
		"cmd/verify-release-checksums/main_test.go",
		"scripts/release-gates/verify_release_checksums.sh",
		"scripts/release-gates/release-checksums.golden.json",
	}
	for _, p := range paths {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("inv-zen-297 VIOLATED: required file missing: %s (%v)", p, err)
		}
	}

	scriptInfo, err := os.Stat(filepath.Join(root, "scripts/release-gates/verify_release_checksums.sh"))
	if err == nil && scriptInfo.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-297 VIOLATED: verify_release_checksums.sh not executable (mode %v)", scriptInfo.Mode())
	}
}

func TestInvZen297_GoldenManifestShape(t *testing.T) {
	root := repoRootInvZen297(t)
	path := filepath.Join(root, "scripts", "release-gates", "release-checksums.golden.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-297: read golden manifest: %v", err)
	}
	type platform struct {
		Platform     string `json:"platform"`
		NameTemplate string `json:"name_template"`
	}
	var m struct {
		SchemaVersion       string     `json:"schema_version"`
		Platforms           []platform `json:"platforms"`
		VersionSummaryRegex string     `json:"version_summary_regex"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("inv-zen-297: parse golden manifest: %v", err)
	}
	if m.SchemaVersion == "" {
		t.Error("inv-zen-297 VIOLATED: golden manifest schema_version empty")
	}
	wantPlatforms := map[string]bool{
		"darwin-arm64": false,
		"linux-amd64":  false,
		"linux-arm64":  false,
	}
	for _, p := range m.Platforms {
		if _, ok := wantPlatforms[p.Platform]; ok {
			wantPlatforms[p.Platform] = true
		}
		if !strings.Contains(p.NameTemplate, "{{VERSION}}") {
			t.Errorf("inv-zen-297 VIOLATED: platform %q name_template missing {{VERSION}} placeholder: %q", p.Platform, p.NameTemplate)
		}
		if !strings.HasSuffix(p.NameTemplate, ".tar.gz") {
			t.Errorf("inv-zen-297 VIOLATED: platform %q name_template does not end in .tar.gz: %q", p.Platform, p.NameTemplate)
		}
	}
	for plat, ok := range wantPlatforms {
		if !ok {
			t.Errorf("inv-zen-297 VIOLATED: golden manifest missing platform %q", plat)
		}
	}
	if m.VersionSummaryRegex == "" {
		t.Error("inv-zen-297 VIOLATED: golden manifest version_summary_regex empty")
	}

	if _, err := regexp.Compile(m.VersionSummaryRegex); err != nil {
		t.Errorf("inv-zen-297 VIOLATED: golden version_summary_regex does not compile: %v", err)
	}

	// Sister-test: the canonical buildinfo Summary() shape MUST match the
	// regex (or every future build's --version output is "invalid"). We
	// build a sample using fake values that exercise every named capture.
	sample := "zen-swarm v1.0.0 commit:abc1234 date:2026-05-25T10:00:00Z go:1.25.6 platform:darwin/arm64"
	re, err := regexp.Compile(m.VersionSummaryRegex)
	if err == nil {
		if !re.MatchString(sample) {
			t.Errorf("inv-zen-297 VIOLATED: golden version_summary_regex does not match canonical Summary() sample:\n  regex=%s\n  sample=%s", m.VersionSummaryRegex, sample)
		}
	}
}

func TestInvZen297_BuildinfoSummaryFieldSet(t *testing.T) {
	s := buildinfo.Summary()
	must := []string{"zen-swarm", "commit:", "date:", "go:", "platform:"}
	for _, want := range must {
		if !strings.Contains(s, want) {
			t.Errorf("inv-zen-297 VIOLATED: buildinfo.Summary()=%q missing canonical field %q", s, want)
		}
	}
}

var errSentinelUnused = errors.New("inv-zen-297 sentinel — unused at v0")

func init() { _ = errSentinelUnused }
