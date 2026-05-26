package compliance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoRootForLayerC(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestLayerC_CIWorkflowContainsDoctrineSteps(t *testing.T) {
	repo := repoRootForLayerC(t)
	body, err := os.ReadFile(filepath.Join(repo, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	s := string(body)
	requiredSubstrings := []string{
		"Doctrine unit + integration tests (Plan 8)",
		"Doctrine analysistest golden fixtures (Plan 8 Phase L)",
		"verify-doctrine-builtin gate (Plan 8 Phase M)",
		"verify-reconciliation gate (Plan 8 Phase 0)",
		"make verify-doctrine-builtin",
		"make verify-reconciliation",
		"./internal/doctrine/...",
		"./internal/orchestrator/amendment/...",
	}
	for _, want := range requiredSubstrings {
		if !strings.Contains(s, want) {
			t.Errorf("ci.yml missing required Plan 8 Phase M substring %q", want)
		}
	}
}

func TestLayerC_PreReleaseWorkflowExists(t *testing.T) {
	repo := repoRootForLayerC(t)
	body, err := os.ReadFile(filepath.Join(repo, ".github", "workflows", "doctrine-pre-release.yml"))
	if err != nil {
		t.Fatalf("read doctrine-pre-release.yml: %v", err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("doctrine-pre-release.yml is not valid YAML: %v", err)
	}
	jobs, ok := parsed["jobs"].(map[string]any)
	if !ok {
		t.Fatalf("doctrine-pre-release.yml missing 'jobs' top-level key (got: %T)", parsed["jobs"])
	}
	requiredJobs := []string{
		"doctrine-adversarial",
		"doctrine-chaos",
		"doctrine-replay",
		"doctrine-timeaccel",
		"doctrine-orchestrator-chaos",
	}
	for _, jobName := range requiredJobs {
		if _, exists := jobs[jobName]; !exists {
			t.Errorf("doctrine-pre-release.yml missing required job %q", jobName)
		}
	}
}

func TestLayerC_PreReleaseWorkflowTriggersOnTagAndManual(t *testing.T) {
	repo := repoRootForLayerC(t)
	body, err := os.ReadFile(filepath.Join(repo, ".github", "workflows", "doctrine-pre-release.yml"))
	if err != nil {
		t.Fatalf("read doctrine-pre-release.yml: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "workflow_dispatch:") {
		t.Errorf("doctrine-pre-release.yml missing workflow_dispatch trigger")
	}
	if !strings.Contains(s, "tags:") || !strings.Contains(s, "'v*'") {
		t.Errorf("doctrine-pre-release.yml missing 'tags: v*' trigger")
	}
}

func TestLayerC_CIWorkflowLintStepUnchanged(t *testing.T) {
	repo := repoRootForLayerC(t)
	body, err := os.ReadFile(filepath.Join(repo, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	s := string(body)

	if !strings.Contains(s, "name: Lint") {
		t.Errorf("ci.yml missing 'name: Lint' step header")
	}
	if !strings.Contains(s, "run: make lint") {
		t.Errorf("ci.yml lint step does not invoke `make lint` (Layer A == Layer C source-of-truth)")
	}
}
