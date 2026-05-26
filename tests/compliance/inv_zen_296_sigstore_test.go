// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRootInvZen296(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-296: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-296: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

type invZen296WorkflowSchema struct {
	Permissions map[string]string               `yaml:"permissions"`
	Jobs        map[string]invZen296WorkflowJob `yaml:"jobs"`
}

type invZen296WorkflowJob struct {
	Steps []invZen296WorkflowStep `yaml:"steps"`
}

type invZen296WorkflowStep struct {
	Uses string                 `yaml:"uses"`
	With map[string]interface{} `yaml:"with"`
}

func TestInvZen296_VerifySigstoreHelperPresentAndExecutable(t *testing.T) {
	root := repoRootInvZen296(t)
	helperPath := filepath.Join(root, "scripts", "release-gates", "verify_sigstore_attestation.sh")
	info, err := os.Stat(helperPath)
	if err != nil {
		t.Fatalf("inv-zen-296 VIOLATED: verify_sigstore_attestation.sh missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-296 VIOLATED: verify_sigstore_attestation.sh not executable; mode=%v",
			info.Mode())
	}
}

func TestInvZen296_ReleaseWorkflowAttestStepsPresent(t *testing.T) {
	root := repoRootInvZen296(t)
	wfPath := filepath.Join(root, ".github", "workflows", "release.yml")
	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("inv-zen-296 VIOLATED: release.yml missing: %v", err)
	}
	var w invZen296WorkflowSchema
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatalf("inv-zen-296: parse release.yml: %v", err)
	}

	if w.Permissions["attestations"] != "write" {

		var jobsMissing []string
		for name, job := range w.Jobs {
			var hasOverride bool
			for _, step := range job.Steps {
				if strings.Contains(step.Uses, "actions/attest-build-provenance") {
					hasOverride = true
					break
				}
			}
			if !hasOverride {
				continue
			}

			jobsMissing = append(jobsMissing, name)
		}
		if len(jobsMissing) > 0 {
			t.Errorf("inv-zen-296 VIOLATED: attestations: write permission not workflow-level + jobs use attest: %v",
				jobsMissing)
		}
	}

	releaseJob, ok := w.Jobs["release"]
	if !ok {
		t.Fatal("inv-zen-296 VIOLATED: release job missing")
	}
	wantSubjects := map[string]bool{
		"binary": false,
		"sbom":   false,
		"nfpm":   false,
	}
	for _, step := range releaseJob.Steps {
		if !strings.Contains(step.Uses, "actions/attest-build-provenance") {
			continue
		}
		subj, _ := step.With["subject-path"].(string)
		switch {
		case strings.Contains(subj, ".deb") || strings.Contains(subj, ".rpm"):
			wantSubjects["nfpm"] = true
		case strings.Contains(subj, ".cdx.json"):
			wantSubjects["sbom"] = true
		case strings.Contains(subj, ".tar.gz"):
			wantSubjects["binary"] = true
		}
	}
	for name, found := range wantSubjects {
		if !found {
			t.Errorf("inv-zen-296 VIOLATED: release job missing attest-build-provenance step for %s artifacts",
				name)
		}
	}
	dockerJob, ok := w.Jobs["docker"]
	if !ok {
		t.Fatal("inv-zen-296 VIOLATED: docker job missing")
	}
	var imageAttest bool
	for _, step := range dockerJob.Steps {
		if !strings.Contains(step.Uses, "actions/attest-build-provenance") {
			continue
		}
		name, _ := step.With["subject-name"].(string)
		digest, _ := step.With["subject-digest"].(string)
		if strings.Contains(name, "ghcr.io/cbip-solutions/hades-system") &&
			strings.Contains(digest, "build.outputs.digest") {
			imageAttest = true
		}
	}
	if !imageAttest {
		t.Error("inv-zen-296 VIOLATED: docker job missing attest-build-provenance step for OCI image")
	}
}
