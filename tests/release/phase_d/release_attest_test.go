// SPDX-License-Identifier: MIT

package phase_d_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAttestBuildProvenanceCoversAllArtifacts verifies every artifact-
// producing step in the release.yml workflow has a corresponding
// attest-build-provenance step downstream. The release job MUST attest:
// binaries + SBOMs + nfpm packages. The docker job MUST attest: Docker
// image (via subject-name + subject-digest).
//
// Drift surfaces a clear missing-coverage error. The test exercises the
// matrix-runner-aware subject-path globs (containing
// matrix.goos / matrix.goarch interpolations) so a silent platform
// renaming would also surface here.
func TestAttestBuildProvenanceCoversAllArtifacts(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))

	releaseJob, ok := w.Jobs["release"]
	if !ok {
		t.Fatal("release job not defined")
	}
	var attestBinary, attestSBOM, attestNFPM bool
	for _, step := range releaseJob.Steps {
		if !strings.Contains(step.Uses, "actions/attest-build-provenance") {
			continue
		}

		subj, _ := step.With["subject-path"].(string)
		switch {
		case strings.Contains(subj, ".deb") || strings.Contains(subj, ".rpm"):

			attestNFPM = true
		case strings.Contains(subj, ".cdx.json"):
			attestSBOM = true
		case strings.Contains(subj, ".tar.gz"):
			attestBinary = true
		}
	}
	if !attestBinary {
		t.Error("inv-zen-296 VIOLATED: release job missing attest step for binaries (*.tar.gz)")
	}
	if !attestSBOM {
		t.Error("inv-zen-296 VIOLATED: release job missing attest step for SBOMs (*.cdx.json)")
	}
	if !attestNFPM {
		t.Error("inv-zen-296 VIOLATED: release job missing attest step for nfpm packages (*.deb / *.rpm)")
	}

	dockerJob, ok := w.Jobs["docker"]
	if !ok {
		t.Fatal("docker job not defined")
	}
	var attestImage bool
	for _, step := range dockerJob.Steps {
		if !strings.Contains(step.Uses, "actions/attest-build-provenance") {
			continue
		}
		name, _ := step.With["subject-name"].(string)
		digest, _ := step.With["subject-digest"].(string)
		if strings.Contains(name, "ghcr.io/cbip-solutions/hades-system") &&
			strings.Contains(digest, "build.outputs.digest") {
			attestImage = true
		}
	}
	if !attestImage {
		t.Error("inv-zen-296 VIOLATED: docker job missing attest step for OCI image (subject-name + subject-digest)")
	}
}

func TestAttestBuildProvenancePushToRegistry(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	dockerJob, ok := w.Jobs["docker"]
	if !ok {
		t.Fatal("docker job not defined")
	}
	for _, step := range dockerJob.Steps {
		if !strings.Contains(step.Uses, "actions/attest-build-provenance") {
			continue
		}

		switch v := step.With["push-to-registry"].(type) {
		case bool:
			if v {
				return
			}
		case string:
			if v == "true" {
				return
			}
		}
	}
	t.Error("inv-zen-296 VIOLATED: docker job's attest-build-provenance step missing push-to-registry: true")
}

func TestAttestBuildProvenancePinnedV2(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	var foundAny bool
	for jobName, job := range w.Jobs {
		for i, step := range job.Steps {
			if !strings.Contains(step.Uses, "actions/attest-build-provenance") {
				continue
			}
			foundAny = true

			if strings.Contains(step.Uses, "@v1") ||
				strings.Contains(step.Uses, "@v3") {
				t.Errorf("job %s step %d uses %q with non-v2 attest-build-provenance pin",
					jobName, i, step.Uses)
			}
		}
	}
	if !foundAny {
		t.Error("inv-zen-296 VIOLATED: no actions/attest-build-provenance step found")
	}
}
