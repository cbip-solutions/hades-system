// SPDX-License-Identifier: MIT

package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skipIfMissingTool(t *testing.T, bin string) {
	t.Helper()
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("integration smoke skipped: %s not on PATH", bin)
	}
}

func climbToRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find repo root (no go.mod ancestor)")
	return ""
}

func TestReleasePipelineSmoke_DriftNegative(t *testing.T) {
	root := climbToRepoRoot(t)

	tmpRoot := t.TempDir()

	src, err := os.ReadFile(filepath.Join(root, "docs", "sbom", "cgo-supplement.cdx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpRoot, "docs", "sbom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpRoot, "docs", "sbom", "cgo-supplement.cdx.json"), src, 0o644); err != nil {
		t.Fatal(err)
	}

	gomod := `module example
go 1.25
require (
	github.com/asg017/sqlite-vec-go-bindings v9.9.9
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)
`
	if err := os.WriteFile(filepath.Join(tmpRoot, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpRoot, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(root, "bin", "verify-cgo-supplement")
	if _, err := os.Stat(bin); err != nil {
		buildCmd := exec.Command("go", "build", "-o", bin, "./cmd/verify-cgo-supplement")
		buildCmd.Dir = root
		if out, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("build verify-cgo-supplement: %v\n%s", err, out)
		}
	}

	cmd := exec.Command(bin, "--root", tmpRoot)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected drift exit 1, got 0 with output:\n%s", out)
	}
	if !strings.Contains(string(out), "sqlite-vec") {
		t.Errorf("expected drift output mentioning sqlite-vec, got:\n%s", out)
	}
}

func TestReleasePipelineSmoke_SnapshotEndToEnd(t *testing.T) {
	skipIfMissingTool(t, "goreleaser")
	skipIfMissingTool(t, "syft")

	root := climbToRepoRoot(t)

	distDir := filepath.Join(root, "dist")
	_ = os.RemoveAll(distDir)
	t.Cleanup(func() { _ = os.RemoveAll(distDir) })

	for _, bin := range []string{"verify-release-artifacts", "verify-cgo-supplement"} {
		buildCmd := exec.Command("go", "build", "-o", filepath.Join(root, "bin", bin), "./cmd/"+bin)
		buildCmd.Dir = root
		if out, err := buildCmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", bin, err, out)
		}
	}

	snap := exec.Command("goreleaser", "release",
		"--snapshot",
		"--clean",
		"--skip=sign,publish,docker",
	)
	snap.Dir = root
	snap.Env = append(os.Environ(),
		"VERIFY_CGO_SUPPLEMENT_BIN="+filepath.Join(root, "bin", "verify-cgo-supplement"),
	)
	out, err := snap.CombinedOutput()
	if err != nil {
		t.Fatalf("goreleaser snapshot: %v\n%s", err, out)
	}

	wantPlatforms := []string{"darwin-arm64", "linux-amd64", "linux-arm64"}
	for _, platform := range wantPlatforms {
		matches, _ := filepath.Glob(filepath.Join(distDir, "*"+platform+"*.tar.gz"))
		if len(matches) == 0 {
			t.Errorf("platform %s: no tarball found in dist/", platform)
			continue
		}
		tarPath := matches[0]
		for _, suffix := range []string{".cdx.json", ".spdx.json"} {
			p := tarPath + suffix
			st, err := os.Stat(p)
			if err != nil {
				t.Errorf("platform %s: missing %s: %v", platform, p, err)
				continue
			}
			if st.Size() == 0 {
				t.Errorf("platform %s: empty %s", platform, p)
			}
		}
	}

	v := exec.Command(filepath.Join(root, "bin", "verify-release-artifacts"),
		"--dir", distDir,
		"--mode", "fast",
		"--check-sbom",
		"--check-cgo-supplement",
		"--check-attestation=false",
		"--check-cosign=false",
	)
	v.Dir = root
	if out, err := v.CombinedOutput(); err != nil {
		t.Errorf("verify-release-artifacts: %v\n%s", err, out)
	}

	vc := exec.Command(filepath.Join(root, "bin", "verify-cgo-supplement"),
		"--root", root,
		"--allow-missing-vendor",
	)
	if out, err := vc.CombinedOutput(); err != nil {
		t.Errorf("verify-cgo-supplement: %v\n%s", err, out)
	}
}
