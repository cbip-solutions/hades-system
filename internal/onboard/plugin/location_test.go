package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLocationSpikePassReturnsProjectScope(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", tmp)
	loc, err := ResolveLocation(true)
	if err != nil {
		t.Fatalf("ResolveLocation(true): %v", err)
	}
	if loc.Kind != LocationKindProjectScope {
		t.Errorf("Kind = %v, want LocationKindProjectScope; loc=%+v", loc.Kind, loc)
	}
	wantSuffix := filepath.Join(".hermes", "plugins", "zen-swarm")
	if !strings.HasSuffix(loc.Path, wantSuffix) {
		t.Errorf("Path = %q, want suffix %q", loc.Path, wantSuffix)
	}
	if !filepath.IsAbs(loc.Path) {
		t.Errorf("Path = %q, not absolute", loc.Path)
	}
}

func TestResolveLocationSpikeFailReturnsUserScope(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", "/tmp/myproj-test123")
	loc, err := ResolveLocation(false)
	if err != nil {
		t.Fatalf("ResolveLocation(false): %v", err)
	}
	if loc.Kind != LocationKindUserScope {
		t.Errorf("Kind = %v, want LocationKindUserScope; loc=%+v", loc.Kind, loc)
	}
	if !strings.Contains(loc.Path, "zen-swarm-") {
		t.Errorf("Path = %q, want zen-swarm-<slug> pattern", loc.Path)
	}
	if !strings.HasPrefix(loc.Path, tmp) {
		t.Errorf("Path = %q, want under HOME=%q", loc.Path, tmp)
	}
}

func TestLocationKindStringer(t *testing.T) {
	cases := []struct {
		k    LocationKind
		want string
	}{
		{LocationKindProjectScope, "project-scope"},
		{LocationKindUserScope, "user-scope"},
		{LocationKind(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("LocationKind(%d).String() = %q, want %q", tc.k, got, tc.want)
		}
	}
}

func TestInstallCreatesDirAndManifest(t *testing.T) {
	tmp := t.TempDir()
	loc := Location{
		Path: filepath.Join(tmp, ".hermes", "plugins", "zen-swarm"),
		Kind: LocationKindProjectScope,
	}
	manifest := []byte(`name = "zen-swarm"
version = "0.13.0"
description = "zen-swarm Hermes plugin"
`)
	canonical, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: manifest, Scope: "project"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if canonical != loc.Path {
		t.Errorf("canonical = %q, want %q", canonical, loc.Path)
	}
	st, err := os.Stat(loc.Path)
	if err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("Path is not a directory: %v", st.Mode())
	}
	mfPath := filepath.Join(loc.Path, "plugin.toml")
	st, err = os.Stat(mfPath)
	if err != nil {
		t.Fatalf("plugin.toml not written: %v", err)
	}
	got, err := os.ReadFile(mfPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(manifest) {
		t.Errorf("manifest mismatch:\n got=%q\nwant=%q", got, manifest)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("plugin.toml mode = %v, want 0o600", st.Mode().Perm())
	}
}

func TestInstallIdempotent(t *testing.T) {
	tmp := t.TempDir()
	loc := Location{
		Path: filepath.Join(tmp, ".hermes", "plugins", "zen-swarm"),
		Kind: LocationKindProjectScope,
	}
	manifest := []byte(`name = "zen-swarm"
version = "0.13.0"
description = "zen-swarm Hermes plugin"
`)
	if _, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: manifest, Scope: "project"}); err != nil {
		t.Fatalf("Install first: %v", err)
	}
	if _, err := Install(context.Background(), InstallOptions{Location: loc, Manifest: manifest, Scope: "project"}); err != nil {
		t.Fatalf("Install second (idempotent): %v", err)
	}
	got, err := os.ReadFile(filepath.Join(loc.Path, "plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(manifest) {
		t.Errorf("manifest content drift after second install:\n got=%q\nwant=%q", got, manifest)
	}
}

func TestResolveLocationCwdFallback(t *testing.T) {
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	loc, err := ResolveLocation(true)
	if err != nil {
		t.Fatalf("ResolveLocation(true) cwd fallback: %v", err)
	}
	if loc.Kind != LocationKindProjectScope {
		t.Errorf("Kind = %v, want LocationKindProjectScope", loc.Kind)
	}
	if !strings.HasPrefix(loc.Path, cwd) {
		t.Errorf("Path = %q, want prefix cwd=%q", loc.Path, cwd)
	}
}

func TestResolveLocationUserHomeDirFallback(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("ZEN_REPO_ROOT_OVERRIDE", "/tmp/somerepo")

	loc, err := ResolveLocation(false)
	if err != nil {

		return
	}
	if loc.Kind != LocationKindUserScope {
		t.Errorf("Kind = %v, want LocationKindUserScope", loc.Kind)
	}
}
