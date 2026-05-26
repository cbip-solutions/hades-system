// SPDX-License-Identifier: MIT

package compliance

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/sbom/cgosupplement"
	"github.com/cbip-solutions/hades-system/tests/testhelpers/sbomfixtures"
)

func TestInvZenE1_DualEmitSBOMPresent(t *testing.T) {
	dir := t.TempDir()
	tarPath := sbomfixtures.FixtureDistArtifact(t, dir, "darwin-arm64")

	for _, suffix := range []string{".cdx.json", ".spdx.json"} {
		p := tarPath + suffix
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if len(data) == 0 {
			t.Errorf("empty SBOM: %s", p)
		}
		var probe map[string]interface{}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Errorf("invalid JSON in %s: %v", p, err)
		}
	}
}

func TestInvZenE2_CGOSupplementValid(t *testing.T) {
	dir := t.TempDir()
	suppPath := sbomfixtures.FixtureSupplement(t, dir)
	gomod := sbomfixtures.FixtureGoMod(t, dir)
	sbomfixtures.FixtureVendorDir(t, dir)

	s, err := cgosupplement.Load(suppPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 3 {
		t.Errorf("want 3 entries (post Stage-0 reality-check), got %d", len(s.Entries))
	}
	wantNames := map[string]bool{
		"sqlite-vec":             false,
		"Foundation framework":   false,
		"smacker/go-tree-sitter": false,
	}
	for _, e := range s.Entries {
		wantNames[e.Name] = true
	}
	for n, ok := range wantNames {
		if !ok {
			t.Errorf("missing entry: %s", n)
		}
	}
	if err := s.ValidateAgainstGoMod(gomod); err != nil {
		t.Errorf("ValidateAgainstGoMod: %v", err)
	}
	if err := s.ValidateAgainstVendorDir(filepath.Join(dir, "vendor")); err != nil {
		t.Errorf("ValidateAgainstVendorDir: %v", err)
	}
}

func TestInvZenE3_SBOMAttestationValid_LibraryWiring(t *testing.T) {
	root := findRepoRootPhaseE(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "actions/attest-build-provenance@v2") {
		t.Error("inv-zen-301 prerequisite: release.yml missing attest-build-provenance@v2 step")
	}

	if !strings.Contains(body, ".cdx.json") || !strings.Contains(body, ".spdx.json") {
		t.Error("inv-zen-301: SBOM glob not covered in attestation subject-path")
	}
}

func TestInvZenE4_SLSAL2ProvenanceWorkflow(t *testing.T) {
	root := findRepoRootPhaseE(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "id-token: write") {
		t.Error("inv-zen-302: id-token: write permission missing")
	}
	if !strings.Contains(body, "attestations: write") {
		t.Error("inv-zen-302: attestations: write permission missing")
	}
	count := strings.Count(body, "uses: actions/attest-build-provenance@v2")
	if count < 2 {
		t.Errorf("inv-zen-302: expected >=2 attest-build-provenance@v2 occurrences (D-9 OCI + E-3 binaries+SBOMs), got %d", count)
	}
}

func TestInvZenE5_OCIImageSignedDelegation(t *testing.T) {
	root := findRepoRootPhaseE(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "cosign sign") || !strings.Contains(body, "ghcr.io") {
		t.Error("inv-zen-303 prerequisite: release.yml missing Phase D-9 cosign sign for ghcr.io image")
	}

	if !methodExistsInVerifier(t, "VerifyOCIImageSignature") {
		t.Error("inv-zen-303: VerifyOCIImageSignature method missing in internal/release/verifier")
	}
}

func methodExistsInVerifier(t *testing.T, name string) bool {
	t.Helper()
	root := findRepoRootPhaseE(t)
	verifierDir := filepath.Join(root, "internal", "release", "verifier")
	entries, err := os.ReadDir(verifierDir)
	if err != nil {
		return false
	}
	needle := "func (v *Verifier) " + name + "("
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(verifierDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			return true
		}
	}
	return false
}

func TestInvZenE6_DriftDetection(t *testing.T) {
	dir := t.TempDir()
	sbomfixtures.FixtureSupplement(t, dir)

	gomod := `module example
go 1.25
require (
	github.com/asg017/sqlite-vec-go-bindings v0.2.0
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	sbomfixtures.FixtureVendorDir(t, dir)

	bin := findOrBuildBinary(t, "verify-cgo-supplement")

	cmd := exec.Command(bin, "--root", dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("inv-zen-304: expected drift detection exit 1; got 0 with output:\n%s", out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("inv-zen-304: expected exit code 1, got %d", exitErr.ExitCode())
		}
	}
	if !strings.Contains(string(out), "sqlite-vec") {
		t.Errorf("inv-zen-304: expected drift error mentioning sqlite-vec, got:\n%s", out)
	}
}

func findOrBuildBinary(t *testing.T, name string) string {
	t.Helper()
	root := findRepoRootPhaseE(t)
	bin := filepath.Join(root, "bin", name)
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/"+name)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build cmd/%s: %v\n%s", name, err, out)
	}
	return bin
}
