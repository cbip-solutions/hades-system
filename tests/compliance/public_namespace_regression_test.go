// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootPublicNamespace(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestPublicNamespaceOperationalSurfacesUseCbipSolutions(t *testing.T) {
	root := repoRootPublicNamespace(t)
	paths := []string{
		"README.md",
		"INSTALL.md",
		"CONTRIBUTING.md",
		"SECURITY.md",
		".github/workflows/anti-bypass-reintroduction-on-pr.yml",
		".github/workflows/dco-check.yml",
		".github/workflows/release-gates.yml",
		".github/workflows/release.yml",
		".goreleaser.yml",
		"Dockerfile",
		"cmd/verify-docker-image/main.go",
		"cmd/verify-release-artifacts/main.go",
		"docs/operations/ci-aggregator.md",
		"docs/operations/release-engineering.md",
		"docs/operations/sbom-verification.md",
		"docs/operations/security-disclosure.md",
		"internal/release/verifier/verifier.go",
		"scripts/apply-rulesets.sh",
		"scripts/release-gates/check-workflow-freshness.sh",
		"scripts/release-gates/verify_cosign_signature.sh",
		"scripts/release-gates/verify_docker_image_signed.sh",
		"scripts/release-gates/verify_release_artifacts_dryrun.sh",
		"scripts/release-gates/verify_sigstore_attestation.sh",
		"scripts/verify_brew_formula.sh",
	}
	oldOrg := "hades-" + "system"
	forbidden := []string{
		"github.com/" + oldOrg + "/" + oldOrg,
		"github.com/" + oldOrg + "/homebrew-tap",
		oldOrg + "/" + oldOrg,
		oldOrg + "/homebrew-tap",
		oldOrg + "/tap",
		"ghcr.io/" + oldOrg + "/" + oldOrg,
		"--owner " + oldOrg,
		"--repo " + oldOrg,
	}

	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(data)
		for _, bad := range forbidden {
			if strings.Contains(text, bad) {
				t.Errorf("%s still contains stale public namespace token %q; public source/tap/release namespace is cbip-solutions", rel, bad)
			}
		}
	}
}
