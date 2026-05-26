// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildVerifyBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "verify-cgo-supplement")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func writeMinimalSupplement(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "docs", "sbom"), 0o755); err != nil {
		t.Fatal(err)
	}
	supp := `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"serialNumber":"urn:uuid:00000000-0000-0000-0000-000000000001","components":[{"type":"library","name":"sqlite-vec","version":"0.1.6","licenses":[{"license":{"id":"Apache-2.0"}}],"properties":[{"name":"hades-system:cgo-classification","value":"cgo-static"},{"name":"hades-system:go-binding","value":"github.com/asg017/sqlite-vec-go-bindings v0.1.6"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "docs", "sbom", "cgo-supplement.cdx.json"), []byte(supp), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCGOSupplement_Golden(t *testing.T) {
	root := t.TempDir()
	writeMinimalSupplement(t, root)
	gomod := `module example
go 1.25
require github.com/asg017/sqlite-vec-go-bindings v0.1.6
`
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}

	bin := buildVerifyBinary(t)
	out, err := exec.Command(bin, "--root", root).CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "no drift") {
		t.Errorf("expected 'no drift' in output, got:\n%s", out)
	}
}

func TestVerifyCGOSupplement_DriftDetection(t *testing.T) {
	root := t.TempDir()
	writeMinimalSupplement(t, root)

	gomod := `module example
go 1.25
require github.com/asg017/sqlite-vec-go-bindings v0.2.0
`
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}

	bin := buildVerifyBinary(t)
	out, err := exec.Command(bin, "--root", root).CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit 1 (drift), got 0\n%s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
		}
	}
	if !strings.Contains(string(out), "sqlite-vec") {
		t.Errorf("expected drift output mentioning sqlite-vec, got:\n%s", out)
	}
}

func TestVerifyCGOSupplement_AllowMissingVendor(t *testing.T) {
	root := t.TempDir()
	writeMinimalSupplement(t, root)
	gomod := `module example
go 1.25
require github.com/asg017/sqlite-vec-go-bindings v0.1.6
`
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	// NOTE(plan-15): deliberately do NOT create vendor/

	bin := buildVerifyBinary(t)
	out, err := exec.Command(bin, "--root", root, "--allow-missing-vendor").CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit 0 with --allow-missing-vendor, got %v\n%s", err, out)
	}
}

func TestVerifyCGOSupplement_MergeMode(t *testing.T) {
	root := t.TempDir()
	writeMinimalSupplement(t, root)

	autoSBOM := filepath.Join(root, "auto.cdx.json")
	if err := os.WriteFile(autoSBOM, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := buildVerifyBinary(t)
	out, err := exec.Command(bin,
		"--root", root,
		"--merge",
		"--sbom", autoSBOM,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("merge mode expected exit 0, got %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "merged 1 supplement entries") {
		t.Errorf("expected 'merged 1 supplement entries' message, got:\n%s", out)
	}

	data, err := os.ReadFile(autoSBOM)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sqlite-vec") {
		t.Errorf("merged SBOM missing sqlite-vec, got:\n%s", data)
	}
}

func TestVerifyCGOSupplement_MergeRequiresSBOMFlag(t *testing.T) {
	root := t.TempDir()
	writeMinimalSupplement(t, root)

	bin := buildVerifyBinary(t)
	_, err := exec.Command(bin,
		"--root", root,
		"--merge",
	).CombinedOutput()
	if err == nil {
		t.Fatal("expected error when --merge invoked without --sbom, got nil")
	}
}
