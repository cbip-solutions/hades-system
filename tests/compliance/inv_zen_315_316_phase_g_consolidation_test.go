package compliance_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var stage0AdditiveGates = []string{
	"verify-changelog-completeness",
	"verify-dco-signoff",
	"verify-canonical-docs-hygiene",
	"verify-no-personal-references",
	"verify-no-task-context-comments",
	"verify-godoc-clean",
	"verify-hermes-boundary",
}

var flipAwareGates = []string{
	"verify-changelog-completeness",
	"verify-dco-signoff",
	"verify-canonical-docs-hygiene",
	"verify-no-personal-references",
}

func TestInvZenG6_Stage0AdditiveGatesComposedInWorkflow(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	var workflow struct {
		Jobs map[string]interface{} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("inv-zen-315: yaml unmarshal: %v", err)
	}

	for _, gate := range stage0AdditiveGates {
		if _, ok := workflow.Jobs[gate]; !ok {
			t.Errorf("inv-zen-315: release-gates.yml missing Stage-0 additive gate job %q", gate)
		}
	}
}

func TestInvZenG6_Stage0AdditiveGatesComposedInMakefile(t *testing.T) {
	t.Parallel()

	makefile := string(mustReadFile_g(t, repoPath_g(t, "Makefile")))

	targetMarker := "\nverify-release-gates:"
	targetIdx := strings.Index(makefile, targetMarker)
	if targetIdx < 0 {
		t.Fatal("inv-zen-315: Makefile missing verify-release-gates target")
	}
	tail := makefile[targetIdx+1:]
	endIdx := strings.Index(tail, "\n\n")
	if endIdx < 0 {
		endIdx = len(tail)
	}
	block := tail[:endIdx]

	for _, gate := range stage0AdditiveGates {
		if !strings.Contains(block, gate) {
			t.Errorf("inv-zen-315: Makefile verify-release-gates missing Stage-0 additive prereq %q", gate)
		}
	}
}

func TestInvZenG6_Stage0AdditiveGatesComposedInRuleset(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/rulesets/main-branch.json"))

	var ruleset struct {
		Rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				RequiredStatusChecks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			} `json:"parameters"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &ruleset); err != nil {
		t.Fatalf("inv-zen-315: json unmarshal: %v", err)
	}

	contexts := map[string]bool{}
	for _, r := range ruleset.Rules {
		if r.Type == "required_status_checks" {
			for _, c := range r.Parameters.RequiredStatusChecks {
				contexts[c.Context] = true
			}
		}
	}

	for _, gate := range stage0AdditiveGates {
		if !contexts[gate] {
			t.Errorf("inv-zen-315: main-branch.json required_status_checks missing Stage-0 additive context %q", gate)
		}
	}
}

func TestInvZenG6_ModeloBFlipAwareWorkflowInput(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	var doc interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("inv-zen-316: yaml unmarshal: %v", err)
	}

	top, ok := doc.(map[string]interface{})
	if !ok {

		var altTop map[interface{}]interface{}
		if err := yaml.Unmarshal(data, &altTop); err != nil {
			t.Fatalf("inv-zen-316: alt unmarshal failed: %v", err)
		}
		top = make(map[string]interface{}, len(altTop))
		for k, v := range altTop {
			switch kk := k.(type) {
			case string:
				top[kk] = v
			case bool:
				if kk {
					top["on"] = v
				}
			}
		}
	}

	onSection := top["on"]
	if onSection == nil {
		t.Fatal("inv-zen-316: release-gates.yml missing `on:` block")
	}

	triggers, ok := onSection.(map[string]interface{})
	if !ok {
		if alt, ok2 := onSection.(map[interface{}]interface{}); ok2 {
			triggers = make(map[string]interface{}, len(alt))
			for k, v := range alt {
				if ks, ok3 := k.(string); ok3 {
					triggers[ks] = v
				}
			}
		} else {
			t.Fatalf("inv-zen-316: `on:` is not a mapping; got %T", onSection)
		}
	}

	workflowCall, ok := triggers["workflow_call"].(map[string]interface{})
	if !ok {
		if alt, ok2 := triggers["workflow_call"].(map[interface{}]interface{}); ok2 {
			workflowCall = make(map[string]interface{}, len(alt))
			for k, v := range alt {
				if ks, ok3 := k.(string); ok3 {
					workflowCall[ks] = v
				}
			}
		} else {
			t.Fatalf("inv-zen-316: `workflow_call` trigger is not a mapping; got %T", triggers["workflow_call"])
		}
	}

	inputs, ok := workflowCall["inputs"].(map[string]interface{})
	if !ok {
		if alt, ok2 := workflowCall["inputs"].(map[interface{}]interface{}); ok2 {
			inputs = make(map[string]interface{}, len(alt))
			for k, v := range alt {
				if ks, ok3 := k.(string); ok3 {
					inputs[ks] = v
				}
			}
		} else {
			t.Fatalf("inv-zen-316: workflow_call.inputs missing or not a mapping; got %T", workflowCall["inputs"])
		}
	}

	if _, hasGateMode := inputs["gate-mode"]; !hasGateMode {
		t.Errorf("inv-zen-316: workflow_call.inputs missing `gate-mode` input (Modelo B decisión 14 flip-aware contract)")
	}
}

func TestInvZenG6_ModeloBFlipAwareEnvWiring(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	content := string(data)

	if !strings.Contains(content, "GATE_MODE:") {
		t.Error("inv-zen-316: release-gates.yml missing `GATE_MODE:` env binding (Modelo B flip-aware posture)")
	}
	if !strings.Contains(content, "inputs.gate-mode") {
		t.Error("inv-zen-316: release-gates.yml `GATE_MODE` env should reference inputs.gate-mode for Modelo B flip-aware override")
	}
}

func TestInvZenG6_ModeloBFlipAwareGatesHonourEnvVar(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	content := string(data)

	gateModeCount := strings.Count(content, "GATE_MODE: ${{ env.GATE_MODE }}")
	if gateModeCount < len(flipAwareGates) {
		t.Errorf("inv-zen-316: expected at least %d `GATE_MODE: ${{ env.GATE_MODE }}` step-level wirings (one per flip-aware gate); got %d",
			len(flipAwareGates), gateModeCount)
	}
}

func TestInvZenG6_PhaseGFileStructureComplete(t *testing.T) {
	t.Parallel()

	expectedFiles := []struct {
		path     string
		minBytes int64
	}{

		{".github/workflows/release-gates.yml", 1000},

		{".github/rulesets/main-branch.json", 500},
		{"scripts/apply-rulesets.sh", 200},

		{"scripts/release-gates/flake-quarantine.txt", 100},
		{"scripts/release-gates/validate-flake-quarantine.sh", 200},

		{"scripts/release-gates/check-workflow-freshness.sh", 500},

		{"docs/operations/ci-aggregator.md", 5000},

		{"tests/compliance/inv_zen_310_release_gates_composite_test.go", 500},
		{"tests/compliance/inv_zen_311_classifier_integration_test.go", 500},
		{"tests/compliance/inv_zen_312_rulesets_test.go", 500},
		{"tests/compliance/inv_zen_313_flake_quarantine_test.go", 500},
		{"tests/compliance/inv_zen_314_cross_workflow_freshness_test.go", 500},
		{"tests/compliance/inv_zen_315_316_phase_g_consolidation_test.go", 500},
	}

	for _, ef := range expectedFiles {
		path := repoPath_g(t, ef.path)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("Phase G file missing: %s (%v)", ef.path, err)
			continue
		}
		if info.Size() < ef.minBytes {
			t.Errorf("Phase G file too small: %s (size=%d; expected ≥%d)", ef.path, info.Size(), ef.minBytes)
		}
	}
}

func TestInvZenG6_PhaseGMakefileTargetsComplete(t *testing.T) {
	t.Parallel()

	makefile := string(mustReadFile_g(t, repoPath_g(t, "Makefile")))

	requiredTargets := []string{
		"verify-release-gates:",
		"validate-flake-quarantine:",
		"verify-cross-workflow-freshness:",
		"verify-hermes:",
		"verify-bypass-sidecar:",
		"verify-no-bypass-references:",
		"verify-multi-arch:",
		"verify-signatures:",
		"verify-docs-maturity:",
	}

	for _, target := range requiredTargets {
		if !strings.Contains(makefile, target) {
			t.Errorf("Makefile missing Phase G target: %q", target)
		}
	}
}

func TestInvZenG6_PhaseGScriptsExecutable(t *testing.T) {
	t.Parallel()

	scripts := []string{
		"scripts/apply-rulesets.sh",
		"scripts/release-gates/validate-flake-quarantine.sh",
		"scripts/release-gates/check-workflow-freshness.sh",
	}

	for _, s := range scripts {
		path := repoPath_g(t, s)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("script not found: %s (%v)", s, err)
			continue
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("script not executable: %s (mode=%o)", s, info.Mode())
		}
	}
}

func TestInvZenG6_LiveRulesetsApplied(t *testing.T) {
	t.Parallel()

	if os.Getenv("ZEN_INTEGRATION_LIVE_GH") != "1" {
		t.Skip("inv-zen-312: skipped (ZEN_INTEGRATION_LIVE_GH != 1; runs only post Phase C-9 flip + apply-rulesets.sh)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "api", "/repos/cbip-solutions/hades-system/rulesets")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inv-zen-312: gh api rulesets: %v\n%s", err, out)
	}

	var rulesets []struct {
		Name        string `json:"name"`
		Enforcement string `json:"enforcement"`
	}
	if err := json.Unmarshal(out, &rulesets); err != nil {
		t.Fatalf("inv-zen-312: json unmarshal: %v", err)
	}

	found := false
	for _, rs := range rulesets {
		if rs.Name == "main-branch-release-gates" {
			found = true
			if rs.Enforcement != "active" {
				t.Errorf("inv-zen-312: ruleset enforcement = %q; want \"active\"", rs.Enforcement)
			}
			break
		}
	}
	if !found {
		t.Error("inv-zen-312: main-branch-release-gates ruleset not found in live GH API; verify scripts/apply-rulesets.sh was executed post Phase C-9")
	}
}

func TestInvZenG6_LiveCrossWorkflowFreshness(t *testing.T) {
	t.Parallel()

	if os.Getenv("ZEN_INTEGRATION_LIVE_GH") != "1" {
		t.Skip("inv-zen-314: skipped (ZEN_INTEGRATION_LIVE_GH != 1; live GH API access required)")
	}

	scriptPath := repoPath_g(t, "scripts/release-gates/check-workflow-freshness.sh")
	cmd := exec.Command(scriptPath)
	cmd.Env = append(os.Environ(), "GH_TOKEN="+os.Getenv("GH_TOKEN"))
	out, err := cmd.CombinedOutput()

	t.Logf("inv-zen-314: check-workflow-freshness.sh output:\n%s", out)
	if err != nil {
		t.Errorf("inv-zen-314: check-workflow-freshness.sh failed: %v", err)
	}
}
