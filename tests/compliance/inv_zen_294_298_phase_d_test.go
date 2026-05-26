// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootForPhaseD(t *testing.T) string {
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
	t.Fatalf("repo root not found from %q", wd)
	return ""
}

func TestInvZen294ArtifactsProducedAllThreeTargets(t *testing.T) {
	root := repoRootForPhaseD(t)

	artifactPath := filepath.Join(root, "internal/release/verifier/artifact.go")
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read %s: %v", artifactPath, err)
	}
	text := string(data)
	for _, plat := range []string{"darwin-arm64", "linux-amd64", "linux-arm64"} {
		if !strings.Contains(text, plat) {
			t.Errorf("inv-zen-294: %s missing platform %q", artifactPath, plat)
		}
	}

	releaserPath := filepath.Join(root, ".goreleaser.yml")
	if _, err := os.Stat(releaserPath); err != nil {
		t.Fatalf("inv-zen-294: %s missing: %v", releaserPath, err)
	}
	rdata, err := os.ReadFile(releaserPath)
	if err != nil {
		t.Fatalf("read %s: %v", releaserPath, err)
	}
	rtext := string(rdata)
	for _, gosArch := range []string{"darwin", "linux", "amd64", "arm64"} {
		if !strings.Contains(rtext, gosArch) {
			t.Errorf("inv-zen-294: .goreleaser.yml missing builds: token %q", gosArch)
		}
	}
}

func TestInvZen295AdHocCodesignSurfacePresent(t *testing.T) {
	root := repoRootForPhaseD(t)

	scriptPath := filepath.Join(root, "scripts/release-gates/verify_macos_codesign.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("inv-zen-295: %s missing: %v", scriptPath, err)
	}
	info, err := os.Stat(scriptPath)
	if err == nil && info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-295: %s is not executable", scriptPath)
	}

	verifierPath := filepath.Join(root, "internal/release/verifier/verifier.go")
	mdata, err := os.ReadFile(verifierPath)
	if err != nil {
		t.Fatalf("read %s: %v", verifierPath, err)
	}
	for _, frag := range []string{"VerifySignatures"} {
		if !strings.Contains(string(mdata), frag) {
			t.Errorf("inv-zen-295: %s missing fragment %q", verifierPath, frag)
		}
	}
}

func TestInvZen296SigstoreAttestationSurfacePresent(t *testing.T) {
	root := repoRootForPhaseD(t)

	for _, script := range []string{
		"scripts/release-gates/verify_sigstore_attestation.sh",
		"scripts/release-gates/verify_cosign_signature.sh",
	} {
		path := filepath.Join(root, script)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("inv-zen-296: %s missing: %v", path, err)
		}
	}

	for _, item := range []struct {
		file string
		frag string
	}{
		{"internal/release/verifier/attestation.go", "VerifyAttestation"},
		{"internal/release/verifier/cosign.go", "VerifyCosignSignature"},
	} {
		path := filepath.Join(root, item.file)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("inv-zen-296: read %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(data), item.frag) {
			t.Errorf("inv-zen-296: %s missing %q", path, item.frag)
		}
	}
}

func TestInvZen297ReproducibilityMetadataRecorded(t *testing.T) {
	root := repoRootForPhaseD(t)
	path := filepath.Join(root, "internal/buildinfo/buildinfo.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-297: read %s: %v", path, err)
	}
	text := string(data)
	for _, frag := range []string{"version", "commit", "date", "buildinfo"} {
		if !strings.Contains(text, frag) {
			t.Errorf("inv-zen-297: %s missing fragment %q", path, frag)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "cmd/verify-release-checksums/main.go")); err != nil {
		t.Errorf("inv-zen-297: cmd/verify-release-checksums missing: %v", err)
	}
}

func TestInvZen298DockerImageMultiArchSignedAttested(t *testing.T) {
	root := repoRootForPhaseD(t)

	if _, err := os.Stat(filepath.Join(root, "Dockerfile")); err != nil {
		t.Fatalf("inv-zen-298: Dockerfile missing: %v", err)
	}
	scriptPath := filepath.Join(root, "scripts/release-gates/verify_docker_image_signed.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("inv-zen-298: %s missing: %v", scriptPath, err)
	}

	ociPath := filepath.Join(root, "internal/release/verifier/oci.go")
	data, err := os.ReadFile(ociPath)
	if err != nil {
		t.Fatalf("read %s: %v", ociPath, err)
	}
	for _, frag := range []string{"VerifyOCIImageSignature", "ghcr.io"} {
		if !strings.Contains(string(data), frag) {
			t.Errorf("inv-zen-298: %s missing %q", ociPath, frag)
		}
	}
}

func TestPhaseDVerifierSurfaceUnchanged(t *testing.T) {
	root := repoRootForPhaseD(t)
	verifierDir := filepath.Join(root, "internal/release/verifier")
	expectedMethods := []string{
		"VerifyAllArtifacts",
		"VerifyChecksum",
		"VerifyMultiArch",
		"VerifySignatures",
		"VerifySBOMPresent",
		"VerifyAttestation",
		"VerifyCosignSignature",
		"VerifyOCIImageSignature",
		"VerifyCGOSupplementEntries",
	}
	files, err := os.ReadDir(verifierDir)
	if err != nil {
		t.Fatalf("read verifier dir: %v", err)
	}
	combined := strings.Builder{}
	for _, f := range files {
		if f.IsDir() || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(verifierDir, f.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", f.Name(), err)
		}
		combined.WriteString(string(data))
	}
	src := combined.String()
	for _, m := range expectedMethods {
		marker := "func (v *Verifier) " + m
		if !strings.Contains(src, marker) {
			t.Errorf("inv-zen-294..298 surface: missing method %q (looking for %q)", m, marker)
		}
	}
}
