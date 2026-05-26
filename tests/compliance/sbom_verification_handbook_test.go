// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSBOMVerificationHandbookSectionsPresent(t *testing.T) {
	root := findRepoRootPhaseE(t)
	path := filepath.Join(root, "docs", "operations", "sbom-verification.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(data)

	wantSections := []string{
		"## What's in a hades-system release",
		"## Verification UX",
		"### Primary path: `gh attestation verify`",
		"### Fallback path: `cosign verify-blob`",
		"## SBOM consumption",
		"### View the SBOM",
		"### Check known vulnerabilities",
		"## CGO supplement maintenance",
		"## Apache §4(d) NOTICE cross-reference",
		"## EU CRA Article 14 compliance context",
		"## Pre/post-flip Rekor transparency log",
		"## Troubleshooting",
	}
	for _, sec := range wantSections {
		if !strings.Contains(body, sec) {
			t.Errorf("missing required section: %q", sec)
		}
	}

	codeBlockCount := strings.Count(body, "```bash") + strings.Count(body, "```sh")
	if codeBlockCount < 3 {
		t.Errorf("expected >=3 shell command code blocks, got %d", codeBlockCount)
	}

	wantRefs := []string{
		"gh attestation verify",
		"cosign verify-blob",
		"cosign verify ghcr.io/",
		"syft cat",
		"grype",
		"configs/cgo-supplement.cdx.json",
		"verify-cgo-supplement",
		"verify-release-artifacts",
	}
	for _, ref := range wantRefs {
		if !strings.Contains(body, ref) {
			t.Errorf("missing required reference: %q", ref)
		}
	}

	if len(data) < 6000 {
		t.Errorf("handbook seems too short (got %d bytes, want >=6000)", len(data))
	}
}
