// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildVerifyReleaseBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "verify-release-artifacts")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func fixtureDistDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	platforms := []string{"darwin-arm64", "linux-amd64", "linux-arm64"}
	for _, p := range platforms {
		tar := filepath.Join(dir, "zen-swarm-1.0.0-"+p+".tar.gz")
		if err := os.WriteFile(tar, []byte("dummy "+p), 0o644); err != nil {
			t.Fatal(err)
		}

		cdx := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[{"name":"sqlite-vec"},{"name":"Foundation framework"},{"name":"smacker/go-tree-sitter"}]}`
		if err := os.WriteFile(tar+".cdx.json", []byte(cdx), 0o644); err != nil {
			t.Fatal(err)
		}

		spdx := `{"spdxVersion":"SPDX-3.0.1","SPDXID":"SPDXRef-DOCUMENT","name":"zen-swarm-1.0.0-` + p + `"}`
		if err := os.WriteFile(tar+".spdx.json", []byte(spdx), 0o644); err != nil {
			t.Fatal(err)
		}

		h := sha256.Sum256([]byte("dummy " + p))
		if err := os.WriteFile(tar+".sha256", []byte(hex.EncodeToString(h[:])+"  "+filepath.Base(tar)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBinary_FastMode_EndToEnd(t *testing.T) {
	dist := fixtureDistDir(t)
	bin := buildVerifyReleaseBinary(t)
	out, err := exec.Command(bin,
		"--dir", dist,
		"--mode", "fast",
		"--check-sbom",
		"--check-cgo-supplement",
		"--check-attestation=false",
		"--check-cosign=false",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 fast-mode, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "verified") {
		t.Errorf("expected 'verified' in output, got:\n%s", out)
	}
}

func TestBinary_FailsOnMissingDir(t *testing.T) {
	bin := buildVerifyReleaseBinary(t)
	_, err := exec.Command(bin, "--dir", "/no/such/dir/zen-system-test").CombinedOutput()
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}

func TestBinary_RejectsInvalidMode(t *testing.T) {
	bin := buildVerifyReleaseBinary(t)
	dist := t.TempDir()
	out, err := exec.Command(bin, "--dir", dist, "--mode", "garbage").CombinedOutput()
	if err == nil {
		t.Fatalf("expected error for invalid --mode, got nil; output:\n%s", out)
	}
	if !strings.Contains(string(out), "invalid --mode") {
		t.Errorf("expected diagnostic 'invalid --mode' in output, got:\n%s", out)
	}
}

func TestBinary_FailsOnMissingSBOM(t *testing.T) {
	dist := t.TempDir()

	tar := filepath.Join(dist, "zen-swarm-1.0.0-darwin-arm64.tar.gz")
	if err := os.WriteFile(tar, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{"linux-amd64", "linux-arm64"} {
		if err := os.WriteFile(filepath.Join(dist, "zen-swarm-1.0.0-"+p+".tar.gz"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	bin := buildVerifyReleaseBinary(t)
	out, err := exec.Command(bin,
		"--dir", dist,
		"--mode", "fast",
		"--check-sbom",
		"--check-cgo-supplement=false",
		"--check-attestation=false",
		"--check-cosign=false",
	).CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure for missing SBOMs, got success\n%s", out)
	}
	if !strings.Contains(string(out), "SBOM") {
		t.Errorf("expected 'SBOM' in error output, got:\n%s", out)
	}
}

func TestBinary_FailsOnMultiArchMissing(t *testing.T) {
	dist := t.TempDir()
	tar := filepath.Join(dist, "zen-swarm-1.0.0-darwin-arm64.tar.gz")
	if err := os.WriteFile(tar, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := buildVerifyReleaseBinary(t)
	_, err := exec.Command(bin, "--dir", dist, "--mode", "fast").CombinedOutput()
	if err == nil {
		t.Fatal("expected matrix-incomplete failure, got nil")
	}
}
