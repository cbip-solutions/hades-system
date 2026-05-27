// SPDX-License-Identifier: MIT
//
// internal/buildinfo package tests.
//
// Verifies the build-metadata embedding surface used by --version on
// both binaries (cmd/zen + cmd/zen-swarm-ctld) and consumed downstream
// by cmd/verify-release-checksums and audit
// chain provenance.
//
// invariant — reproducibility metadata recorded in build:
//
// Version() + Commit() + Date() + GoVersion() + Platform() must be
// non-empty and Summary() must contain the five canonical fields.
// The.goreleaser.yml builds: block injects values via -X ldflags;
// without injection the sentinels "dev" / "unknown" are returned.
package buildinfo

import (
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestVersionDefault(t *testing.T) {
	if version == "" {
		t.Fatal("buildinfo.version is empty string; expected 'dev' sentinel at zero value")
	}

}

func TestVersionFormat(t *testing.T) {
	v := Version()
	if v == "dev" {
		return
	}
	re := regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9\-.+]+)?$`)
	if !re.MatchString(v) {
		t.Errorf("Version()=%q does not match semver-ish pattern", v)
	}
}

func TestCommitDefault(t *testing.T) {
	if commit == "" {
		t.Fatal("buildinfo.commit is empty string; expected 'unknown' sentinel at zero value")
	}
}

func TestDateDefault(t *testing.T) {
	if date == "" {
		t.Fatal("buildinfo.date is empty string; expected 'unknown' sentinel at zero value")
	}
}

func TestGoVersion(t *testing.T) {
	got := GoVersion()
	want := strings.TrimPrefix(runtime.Version(), "go")
	if got != want {
		t.Errorf("GoVersion()=%q, want %q", got, want)
	}
	if strings.HasPrefix(got, "go") {
		t.Errorf("GoVersion()=%q still carries the 'go' prefix", got)
	}
}

func TestPlatform(t *testing.T) {
	got := Platform()
	want := runtime.GOOS + "/" + runtime.GOARCH
	if got != want {
		t.Errorf("Platform()=%q, want %q", got, want)
	}
}

func TestSummary(t *testing.T) {
	s := Summary()
	must := []string{
		"zen-swarm",
		"commit:",
		"date:",
		"go:",
		"platform:",
	}
	for _, want := range must {
		if !strings.Contains(s, want) {
			t.Errorf("Summary()=%q missing field %q", s, want)
		}
	}
}

func TestSummaryIncludesGoMinor(t *testing.T) {
	s := Summary()
	if !regexp.MustCompile(`go:1\.\d+`).MatchString(s) {
		t.Errorf("Summary()=%q does not include go:1.<minor>", s)
	}
}

func TestSummaryPlatformSegment(t *testing.T) {
	s := Summary()
	want := "platform:" + runtime.GOOS + "/" + runtime.GOARCH
	if !strings.Contains(s, want) {
		t.Errorf("Summary()=%q missing %q segment", s, want)
	}
}

func TestProvenanceKeys(t *testing.T) {
	p := Provenance()
	wantKeys := []string{
		"buildinfo.version",
		"buildinfo.commit",
		"buildinfo.date",
		"buildinfo.go_version",
		"buildinfo.platform",
	}
	if len(p) != len(wantKeys) {
		t.Errorf("Provenance() map size=%d, want %d", len(p), len(wantKeys))
	}
	for _, k := range wantKeys {
		v, ok := p[k]
		if !ok {
			t.Errorf("Provenance() missing key %q", k)
		}
		if v == "" {
			t.Errorf("Provenance()[%q] is empty (sentinel expected)", k)
		}
	}
}

func TestVersionWithLDFlags(t *testing.T) {
	v := Version()
	if v == "" {
		t.Fatal("Version() returned empty string")
	}
	if v != "dev" {

		if !strings.ContainsAny(v, "0123456789") && v != "snapshot" {
			t.Errorf("Version()=%q is neither 'dev' nor a numeric/'snapshot' label", v)
		}
	}
}
