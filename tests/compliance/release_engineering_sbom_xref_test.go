// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseEngineeringSBOMCrossReference(t *testing.T) {
	root := findRepoRootPhaseE(t)
	path := filepath.Join(root, "docs", "operations", "release-engineering.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(data)

	wantSubstrings := []string{
		"sbom-verification.md",
		"CycloneDX 1.6",
		"SPDX 3.0.1",
		"gh attestation verify",
		"cosign verify-blob",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(body, s) {
			t.Errorf("missing required substring: %q", s)
		}
	}
}
