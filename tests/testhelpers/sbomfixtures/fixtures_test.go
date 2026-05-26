// SPDX-License-Identifier: MIT

package sbomfixtures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureSupplement_Writes3Entries(t *testing.T) {
	dir := t.TempDir()
	path := FixtureSupplement(t, dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	for _, name := range []string{"sqlite-vec", "Foundation framework", "smacker/go-tree-sitter"} {
		if !strings.Contains(body, name) {
			t.Errorf("fixture supplement missing %q", name)
		}
	}
}

func TestFixtureGoMod_Writes(t *testing.T) {
	dir := t.TempDir()
	path := FixtureGoMod(t, dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "sqlite-vec-go-bindings") {
		t.Error("fixture go.mod missing sqlite-vec-go-bindings require")
	}
}

func TestFixtureVendorDir_CreatesEmpty(t *testing.T) {
	dir := t.TempDir()
	vendor := FixtureVendorDir(t, dir)
	st, err := os.Stat(vendor)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir() {
		t.Errorf("expected vendor dir, got non-dir %s", vendor)
	}
}

func TestFixtureDistArtifact_AllCompanionFiles(t *testing.T) {
	dir := t.TempDir()
	tarPath := FixtureDistArtifact(t, dir, "darwin-arm64")
	for _, suffix := range []string{".sha256", ".cdx.json", ".spdx.json", ".sig", ".pem", ".intoto.jsonl"} {
		p := tarPath + suffix
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s: %v", p, err)
		}
	}

	cdx, _ := os.ReadFile(tarPath + ".cdx.json")
	for _, name := range []string{"sqlite-vec", "Foundation framework", "smacker/go-tree-sitter"} {
		if !strings.Contains(string(cdx), name) {
			t.Errorf("fixture .cdx.json missing supplement component %q", name)
		}
	}
}

func TestFixtureDistArtifact_PerPlatformFilename(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"darwin-arm64", "linux-amd64", "linux-arm64"} {
		tar := FixtureDistArtifact(t, dir, p)
		if !strings.Contains(filepath.Base(tar), p) {
			t.Errorf("fixture tarball filename missing platform %q: %s", p, tar)
		}
	}
}
