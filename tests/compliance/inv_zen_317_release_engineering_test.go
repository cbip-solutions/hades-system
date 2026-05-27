// SPDX-License-Identifier: MIT

// go:build !race
//go:build !race
// +build !race

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func releaseEngHandbookPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(findRepoRoot(t), "docs", "operations", "release-engineering.md")
}

func readReleaseEngHandbook(t *testing.T) ([]byte, string) {
	t.Helper()
	path := releaseEngHandbookPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read release-engineering.md (%s): %v", path, err)
	}
	return data, path
}

func TestInvZen317_ReleaseEngHandbookExists(t *testing.T) {
	t.Parallel()
	data, path := readReleaseEngHandbook(t)
	if len(data) == 0 {
		t.Fatalf("release-engineering.md is empty: %s", path)
	}
	const minLines = 100
	lineCount := strings.Count(string(data), "\n")
	if lineCount < minLines {
		t.Fatalf("release-engineering.md has %d lines; expected ≥ %d for substantive handbook baseline", lineCount, minLines)
	}
}

func TestInvZen317_ReleaseEngHandbookSigstoreRecipe(t *testing.T) {
	t.Parallel()
	data, _ := readReleaseEngHandbook(t)
	text := string(data)

	if !strings.Contains(text, "gh attestation verify") {
		t.Fatalf("release-engineering.md missing `gh attestation verify` recipe — sigstore SLSA L2 attestation verification not documented")
	}
}

func TestInvZen317_ReleaseEngHandbookCosignRecipe(t *testing.T) {
	t.Parallel()
	data, _ := readReleaseEngHandbook(t)
	text := string(data)

	hasVerify := strings.Contains(text, "cosign verify")
	hasVerifyBlob := strings.Contains(text, "cosign verify-blob")
	if !hasVerify && !hasVerifyBlob {
		t.Fatalf("release-engineering.md missing `cosign verify` or `cosign verify-blob` recipe — cosign keyless signature verification not documented")
	}
}

func TestInvZen317_ReleaseEngHandbookSBOMCoverage(t *testing.T) {
	t.Parallel()
	data, _ := readReleaseEngHandbook(t)
	text := string(data)

	if !strings.Contains(text, "CycloneDX") {
		t.Fatalf("release-engineering.md missing \"CycloneDX\" — Phase E SBOM canonical format not documented")
	}
	if !strings.Contains(text, "SPDX") {
		t.Fatalf("release-engineering.md missing \"SPDX\" — Phase E SBOM cross-tool compatibility format not documented")
	}
}

func TestInvZen317_ReleaseEngHandbookReproducibility(t *testing.T) {
	t.Parallel()
	data, _ := readReleaseEngHandbook(t)
	text := string(data)

	markers := []string{
		"mod_timestamp",
		"buildid=",
		"trimpath",
		"-X main.version",
		"reproducib",
	}
	matched := false
	for _, m := range markers {
		if strings.Contains(text, m) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("release-engineering.md missing reproducibility-procedure markers (any of: %v)", markers)
	}
}

func TestInvZen317_ReleaseEngHandbookDockerVerification(t *testing.T) {
	t.Parallel()
	data, _ := readReleaseEngHandbook(t)
	text := string(data)

	if !strings.Contains(text, "ghcr.io/cbip-solutions/hades-system") {
		t.Fatalf("release-engineering.md missing GHCR image reference (`ghcr.io/cbip-solutions/hades-system`) — Docker image verification not documented")
	}

	hasDigestMarker := strings.Contains(text, "@${DIGEST}") ||
		strings.Contains(text, "@sha256:") ||
		strings.Contains(text, "manifest digest") ||
		strings.Contains(text, "Manifest.Digest")
	if !hasDigestMarker {
		t.Fatalf("release-engineering.md missing Docker manifest-digest verification context (cosign verify always operates on the digest, not the tag)")
	}
}

func TestInvZen317_ReleaseEngHandbookInvariantsEnumerated(t *testing.T) {
	t.Parallel()
	data, _ := readReleaseEngHandbook(t)
	text := string(data)

	invariants := []string{
		"inv-zen-294",
		"inv-zen-295",
		"inv-zen-296",
		"inv-zen-297",
		"inv-zen-298",
	}
	found := 0
	for _, inv := range invariants {
		if strings.Contains(text, inv) {
			found++
		}
	}
	const minFound = 3
	if found < minFound {
		t.Fatalf("release-engineering.md enumerates %d of %d Phase D + E invariants by literal id (looked for %v); expected ≥ %d for traceability", found, len(invariants), invariants, minFound)
	}
}
