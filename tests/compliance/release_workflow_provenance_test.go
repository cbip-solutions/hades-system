// SPDX-License-Identifier: MIT

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseWorkflowSLSAL2Provenance(t *testing.T) {
	root := findRepoRootPhaseE(t)
	path := filepath.Join(root, ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var wf struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string                 `yaml:"name"`
				Uses string                 `yaml:"uses"`
				With map[string]interface{} `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}

	for _, key := range []string{"id-token", "attestations", "contents"} {
		got, ok := wf.Permissions[key]
		if !ok || got != "write" {
			t.Errorf("workflow-level permissions: %s must be 'write', got ok=%v val=%q", key, ok, got)
		}
	}

	type provStep struct {
		jobName     string
		stepName    string
		uses        string
		subjectPath string
	}
	var provSteps []provStep
	for jobName, job := range wf.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/attest-build-provenance@") {
				continue
			}

			if !strings.HasPrefix(step.Uses, "actions/attest-build-provenance@v2") {
				t.Errorf("job=%s step=%q: attest-build-provenance must be pinned to v2.x (got %q)",
					jobName, step.Name, step.Uses)
			}
			subj := ""
			if v, ok := step.With["subject-path"]; ok {
				if s, isStr := v.(string); isStr {
					subj = s
				}
			}
			provSteps = append(provSteps, provStep{
				jobName:     jobName,
				stepName:    step.Name,
				uses:        step.Uses,
				subjectPath: subj,
			})
		}
	}

	if len(provSteps) < 2 {
		t.Fatalf("expected >=2 attest-build-provenance steps (D-9 OCI image + E-3 binaries+SBOMs), got %d", len(provSteps))
	}

	cdxCovered := false
	spdxCovered := false
	binaryCovered := false
	for _, s := range provSteps {
		if strings.Contains(s.subjectPath, ".cdx.json") {
			cdxCovered = true
		}
		if strings.Contains(s.subjectPath, ".spdx.json") {
			spdxCovered = true
		}
		if strings.Contains(s.subjectPath, ".tar.gz") {
			binaryCovered = true
		}
	}
	if !cdxCovered {
		t.Error("inv-zen-302: no attest-build-provenance step covers *.cdx.json (Phase E SBOM CycloneDX)")
	}
	if !spdxCovered {
		t.Error("inv-zen-302: no attest-build-provenance step covers *.spdx.json (Phase E-1 SBOM SPDX dual-emit)")
	}
	if !binaryCovered {
		t.Error("inv-zen-302: no attest-build-provenance step covers *.tar.gz (Phase D binary tarballs)")
	}
}
