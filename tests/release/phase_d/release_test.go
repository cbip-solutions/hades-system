// SPDX-License-Identifier: MIT

package phase_d_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflowSchema struct {
	Name        string                 `yaml:"name"`
	On          map[string]interface{} `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Env         map[string]string      `yaml:"env"`
	Jobs        map[string]jobSchema   `yaml:"jobs"`
}

type jobSchema struct {
	Name        string                 `yaml:"name,omitempty"`
	RunsOn      interface{}            `yaml:"runs-on"`
	Needs       interface{}            `yaml:"needs,omitempty"`
	Permissions map[string]string      `yaml:"permissions,omitempty"`
	Strategy    map[string]interface{} `yaml:"strategy,omitempty"`
	Env         map[string]string      `yaml:"env,omitempty"`
	Steps       []stepSchema           `yaml:"steps"`
}

type stepSchema struct {
	Name string                 `yaml:"name,omitempty"`
	Uses string                 `yaml:"uses,omitempty"`
	Run  string                 `yaml:"run,omitempty"`
	With map[string]interface{} `yaml:"with,omitempty"`
	Env  map[string]string      `yaml:"env,omitempty"`
	If   string                 `yaml:"if,omitempty"`
	ID   string                 `yaml:"id,omitempty"`
}

func loadWorkflow(t *testing.T, path string) workflowSchema {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var w workflowSchema
	if err := yaml.Unmarshal(data, &w); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return w
}

func TestReleaseWorkflowHasMatrixJob(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	job, ok := w.Jobs["release"]
	if !ok {
		t.Fatal("release job not defined")
	}
	strat, ok := job.Strategy["matrix"].(map[string]interface{})
	if !ok {
		t.Fatal("release job has no strategy.matrix")
	}
	include, ok := strat["include"].([]interface{})
	if !ok {
		t.Fatal("strategy.matrix.include not an array")
	}
	wantPlatforms := map[string]bool{
		"macos-latest|darwin|arm64":    false,
		"ubuntu-latest|linux|amd64":    false,
		"ubuntu-22.04-arm|linux|arm64": false,
	}
	for _, item := range include {
		m, _ := item.(map[string]interface{})
		runner, _ := m["runner"].(string)
		goos, _ := m["goos"].(string)
		goarch, _ := m["goarch"].(string)
		key := runner + "|" + goos + "|" + goarch
		if _, expected := wantPlatforms[key]; expected {
			wantPlatforms[key] = true
		} else {
			t.Errorf("unexpected matrix entry: %s", key)
		}
	}
	for k, found := range wantPlatforms {
		if !found {
			t.Errorf("missing matrix entry: %s", k)
		}
	}
}

func TestReleaseWorkflowHasDockerJob(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	job, ok := w.Jobs["docker"]
	if !ok {
		t.Fatal("docker job not defined")
	}
	var foundRelease bool
	switch needs := job.Needs.(type) {
	case []interface{}:
		for _, n := range needs {
			if s, _ := n.(string); s == "release" {
				foundRelease = true
			}
		}
	case string:
		if needs == "release" {
			foundRelease = true
		}
	}
	if !foundRelease {
		t.Error("docker job missing needs: [release]")
	}
	var foundBuildx, foundCosign, foundAttest, foundLogin bool
	for _, step := range job.Steps {
		if strings.Contains(step.Uses, "docker/setup-buildx-action") {
			foundBuildx = true
		}
		if strings.Contains(step.Uses, "sigstore/cosign-installer") {
			foundCosign = true
		}
		if strings.Contains(step.Uses, "actions/attest-build-provenance") {
			foundAttest = true
		}
		if strings.Contains(step.Uses, "docker/login-action") {
			foundLogin = true
		}
	}
	if !foundBuildx {
		t.Error("docker job missing setup-buildx-action")
	}
	if !foundCosign {
		t.Error("docker job missing cosign-installer")
	}
	if !foundAttest {
		t.Error("docker job missing attest-build-provenance")
	}
	if !foundLogin {
		t.Error("docker job missing docker/login-action for GHCR")
	}
}

func TestReleaseWorkflowHasIDTokenPermission(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if w.Permissions["id-token"] == "write" {
		return
	}
	for name, job := range w.Jobs {
		if job.Permissions["id-token"] != "write" {
			t.Errorf("job %q missing id-token: write permission (workflow-level absent)", name)
		}
	}
}

func TestReleaseWorkflowHasPackagesPermission(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if w.Permissions["packages"] == "write" {
		return
	}
	job, ok := w.Jobs["docker"]
	if !ok {
		t.Skip("docker job not defined; checked elsewhere")
	}
	if job.Permissions["packages"] != "write" {
		t.Error("docker job missing packages: write permission (workflow-level absent)")
	}
}

func TestReleaseWorkflowHasAttestationsPermission(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	if w.Permissions["attestations"] == "write" {
		return
	}
	var jobsMissing []string
	for name, job := range w.Jobs {
		if job.Permissions["attestations"] != "write" {
			jobsMissing = append(jobsMissing, name)
		}
	}
	if len(jobsMissing) > 0 {
		t.Errorf("jobs missing attestations: write (workflow-level absent): %v", jobsMissing)
	}
}

func TestReleaseWorkflowActionsNoFloatingPins(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	forbidden := []string{"@main", "@master", "@latest", "@HEAD"}
	for jobName, job := range w.Jobs {
		for i, step := range job.Steps {
			if step.Uses == "" {
				continue
			}
			for _, f := range forbidden {
				if strings.HasSuffix(step.Uses, f) {
					t.Errorf("job %s step %d uses %q with forbidden floating pin",
						jobName, i, step.Uses)
				}
			}
		}
	}
}

func TestReleaseWorkflowGoVersionPinned(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))

	if envVer, ok := w.Env["GO_VERSION"]; ok {
		if !strings.HasPrefix(envVer, "1.25") {
			t.Errorf("workflow env.GO_VERSION=%q; want 1.25.x", envVer)
		}
		return
	}
	var found bool
	for jobName, job := range w.Jobs {
		for _, step := range job.Steps {
			if !strings.Contains(step.Uses, "actions/setup-go") {
				continue
			}
			ver, _ := step.With["go-version"].(string)
			if ver == "" {
				continue
			}

			if strings.Contains(ver, "env.GO_VERSION") {
				found = true
				continue
			}
			if !strings.HasPrefix(ver, "1.25") {
				t.Errorf("job %s setup-go go-version=%q; want 1.25.x", jobName, ver)
			} else {
				found = true
			}
		}
	}
	if !found {
		t.Error("no setup-go step found with pinned go-version")
	}
}

func TestReleaseWorkflowTagTrigger(t *testing.T) {
	root := repoRoot(t)
	w := loadWorkflow(t, filepath.Join(root, ".github", "workflows", "release.yml"))
	push, ok := w.On["push"].(map[string]interface{})
	if !ok {
		t.Fatal("on.push not defined as a map")
	}
	tags, ok := push["tags"].([]interface{})
	if !ok {
		t.Fatal("on.push.tags not an array")
	}
	var found bool
	for _, tag := range tags {
		if s, _ := tag.(string); s == "v*" {
			found = true
		}
	}
	if !found {
		t.Error("on.push.tags missing 'v*' pattern")
	}
}
