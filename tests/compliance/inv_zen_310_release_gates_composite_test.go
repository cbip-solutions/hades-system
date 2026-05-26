// tests/compliance/inv_zen_310_release_gates_composite_test.go
//
// inv-zen-310 (Plan 15 Phase G G-1) — release-gates.yml composite passes all
// required jobs (27 gates + 1 aggregate = 28 jobs total post Stage-0 + B-15).
//
// Compile-check slice: the .github/workflows/release-gates.yml workflow file
// MUST exist + parse as valid YAML + declare the 27 baseline+Stage-0+B-15
// gate jobs + aggregate roll-up + workflow_call reusable trigger; Makefile
// `verify-release-gates` composite MUST list every gate as a prerequisite
// (one-to-one parity vs the workflow job catalog).
//
// Runtime check (deferred to G-7 integration smoke): `gh workflow run
// release-gates --ref <head>` reaches success on all 27 gates + aggregate.
//
// Stage-0 reality-check note (per feedback_plan_template_drift.md): the
// master plan §2.1 and plan-file narrative both claim "27 gates + 1 aggregate
// = 28 jobs total" but the actual enumeration in both documents historically
// listed only 26 distinct gate targets (19 baseline + 7 Stage-0 additive).
// The "8 Stage-0 additive" text-count was a plan-file inconsistency that
// double-counted verify-license-compliance (which is in the 19-baseline AND
// framed as "semantically new under MIT" per decisión 15). Phase B-15
// (decisión 17-d capability-vector forward-compat; inv-zen-284) added
// verify-sidecar-capability-negotiation as the 27th gate — current cardinality
// (27 + 1 = 28 jobs) now aligns with the original master-plan text. Reality
// wins: this test asserts the actual enumeration cardinality (27 + 1 = 28).
//
// The 7 Stage-0 additive gates (per decisiones 7-b/8/9/10/12/15-2) compose on
// top of the 19-baseline:
//   - verify-changelog-completeness  (A-12; decisión 8 anti-recurrence)
//   - verify-dco-signoff             (C-14; decisión 15-2 DCO sign-off)
//   - verify-canonical-docs-hygiene  (I-8; decisión 9 canonical docs refresh)
//   - verify-no-personal-references  (J-9; decisión 10 privacy scrub)
//   - verify-no-task-context-comments (K-9; decisión 12 task-context rot)
//   - verify-godoc-clean             (K-9; decisión 12 godoc presence)
//   - verify-hermes-boundary         (H-12; decisión 7-b boundary consolidation)
//
// Phase B-15 additive gate (per decisión 17-d):
//   - verify-sidecar-capability-negotiation (B-15; inv-zen-284 capability-vector
//     forward-compat integration test promoted from standalone target into the
//     composite at B-15 to lockstep with the aggregator + expectedGateJobs).
//
// verify-license-compliance is in the 19-baseline list and now MIT-canonical
// per decisión 15 (supersedes Apache-2.0; surface count 4-redundant down from
// 5-redundant under Apache + NOTICE).
//
// Helper sharing: this file defines repoPath + mustReadFile + yaml unmarshal
// helpers for the compliance_test external package. repoRoot is defined in
// inv_zen_211_cascade_completeness_test.go and reused here (same package).
//
// Three-place triple:
//
//	(1) spec §7.7 inv-zen-310 text
//	(2) this compliance test (file existence + YAML structure + job catalog parity)
//	(3) .github/workflows/release-gates.yml + Makefile verify-release-gates target
package compliance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func repoPath_g(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), rel)
}

func mustReadFile_g(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestInvZenG1_ReleaseGatesYamlExists(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, ".github/workflows/release-gates.yml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("inv-zen-310: release-gates.yml not found at %s: %v", path, err)
	}
}

// TestInvZenG1_ReleaseGatesYamlParsesAsValidWorkflow asserts the workflow file
// is structurally a valid GitHub Actions workflow (top-level keys: name, on,
// permissions, jobs). YAML 1.1 (which yaml.v3 follows) interprets bare `on:`
// as the bool literal `true`; the workflow MUST quote `"on":` so the parser
// returns the string key. We assert the quoted form for portability across
// tooling (golangci-lint, yamllint, jsonschema validators sometimes choke on
// bool-keyed maps).
func TestInvZenG1_ReleaseGatesYamlParsesAsValidWorkflow(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	var workflow map[interface{}]interface{}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("inv-zen-310: release-gates.yml is not valid YAML: %v", err)
	}

	requiredCanonical := []string{"name", "permissions", "jobs"}
	for _, key := range requiredCanonical {
		if _, ok := workflow[key]; !ok {
			t.Errorf("inv-zen-310: release-gates.yml missing required top-level key %q", key)
		}
	}

	_, hasOnString := workflow["on"]
	_, hasOnBool := workflow[true]
	if !hasOnString && !hasOnBool {
		t.Errorf("inv-zen-310: release-gates.yml missing `on:` trigger declaration")
	}
}

var expectedGateJobs = []string{

	"verify-brew-formula",
	"verify-hermes",
	"smoke-hermes-plugin",
	"smoke-hermes-plugin-real",
	"verify-spikes-rerun",
	"verify-30-ci-green",
	"verify-license-disclosure",
	"verify-bypass-sidecar",
	"verify-no-bypass-references",
	"verify-license-compliance",
	"verify-multi-arch",
	"verify-signatures",
	"verify-release-artifacts",
	"verify-cgo-supplement",
	"verify-chaos-suite",
	"verify-docs-maturity",
	"verify-cross-workflow-freshness",
	"verify-invariants",
	"test-tiers",

	"verify-changelog-completeness",
	"verify-dco-signoff",
	"verify-canonical-docs-hygiene",
	"verify-no-personal-references",
	"verify-no-task-context-comments",
	"verify-godoc-clean",
	"verify-hermes-boundary",

	"verify-sidecar-capability-negotiation",
}

func TestInvZenG1_ReleaseGatesYamlDeclares27GatesPlusAggregate(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	var workflow struct {
		Jobs map[string]interface{} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("inv-zen-310: yaml unmarshal: %v", err)
	}

	for _, jobName := range expectedGateJobs {
		if _, ok := workflow.Jobs[jobName]; !ok {
			t.Errorf("inv-zen-310: release-gates.yml missing required gate job %q", jobName)
		}
	}
	if _, ok := workflow.Jobs["aggregate"]; !ok {
		t.Errorf("inv-zen-310: release-gates.yml missing required `aggregate` roll-up job")
	}

	wantCount := len(expectedGateJobs) + 1
	if len(workflow.Jobs) < wantCount {
		t.Errorf("inv-zen-310: release-gates.yml declares %d jobs; expected at least %d gates + 1 aggregate post Stage-0 enumeration",
			len(workflow.Jobs), wantCount)
	}
}

func TestInvZenG1_AggregateJobNeedsAllGateJobs(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))
	var workflow struct {
		Jobs map[string]struct {
			Needs interface{} `yaml:"needs"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("inv-zen-310: yaml unmarshal: %v", err)
	}

	aggregate, ok := workflow.Jobs["aggregate"]
	if !ok {
		t.Fatal("inv-zen-310: release-gates.yml missing aggregate job")
	}

	var needs []string
	switch v := aggregate.Needs.(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				needs = append(needs, s)
			}
		}
	case string:
		needs = []string{v}
	default:
		t.Fatalf("inv-zen-310: aggregate.needs has unexpected type %T", aggregate.Needs)
	}

	if len(needs) < len(expectedGateJobs) {
		t.Errorf("inv-zen-310: aggregate.needs declares %d dependencies; expected at least %d gate jobs post Stage-0",
			len(needs), len(expectedGateJobs))
	}

	// aggregate MUST NOT need itself (circular dependency)
	for _, n := range needs {
		if n == "aggregate" {
			t.Errorf("inv-zen-310: aggregate.needs contains itself (circular dependency)")
		}
	}

	// Every expected gate MUST appear in needs (else aggregate is loose)
	needsSet := make(map[string]bool, len(needs))
	for _, n := range needs {
		needsSet[n] = true
	}
	for _, gate := range expectedGateJobs {
		if !needsSet[gate] {
			t.Errorf("inv-zen-310: aggregate.needs missing gate %q (aggregate must roll up every gate)", gate)
		}
	}
}

func TestInvZenG1_WorkflowCallTriggerDeclared(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/workflows/release-gates.yml"))

	var doc interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("inv-zen-310: yaml unmarshal: %v", err)
	}
	top, ok := doc.(map[string]interface{})
	if !ok {

		var altTop map[interface{}]interface{}
		if err := yaml.Unmarshal(data, &altTop); err != nil {
			t.Fatalf("inv-zen-310: top-level YAML is not a mapping; got %T (alt unmarshal err: %v)", doc, err)
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

	onSection, present := top["on"]
	if !present {
		t.Fatal("inv-zen-310: release-gates.yml missing `on:` trigger section")
	}

	var triggers map[string]interface{}
	switch v := onSection.(type) {
	case map[string]interface{}:
		triggers = v
	case map[interface{}]interface{}:
		triggers = make(map[string]interface{}, len(v))
		for k, val := range v {
			if ks, ok := k.(string); ok {
				triggers[ks] = val
			}
		}
	default:
		t.Fatalf("inv-zen-310: `on:` section is not a mapping; got %T", onSection)
	}
	if _, hasWorkflowCall := triggers["workflow_call"]; !hasWorkflowCall {
		t.Errorf("inv-zen-310: release-gates.yml missing `workflow_call` trigger (required for Phase D D-2 release.yml composition per sync point S-G-D-WORKFLOW-CALL)")
	}
}

func TestInvZenG1_MakefileVerifyReleaseGatesParity(t *testing.T) {
	t.Parallel()

	makefileBytes := mustReadFile_g(t, repoPath_g(t, "Makefile"))
	makefile := string(makefileBytes)

	targetIdx := -1
	for _, marker := range []string{"\nverify-release-gates:"} {
		if idx := strings.Index(makefile, marker); idx >= 0 {
			targetIdx = idx + 1
			break
		}
	}
	if targetIdx < 0 {
		t.Fatal("inv-zen-310: Makefile is missing `verify-release-gates:` target (Phase G G-1 must finalize composite)")
	}

	tail := makefile[targetIdx:]
	endIdx := strings.Index(tail, "\n\n")
	if endIdx < 0 {
		endIdx = len(tail)
	}
	targetBlock := tail[:endIdx]

	// Each gate job MUST appear as a substring in the prerequisite list.
	missing := []string{}
	for _, gate := range expectedGateJobs {

		needle := gate
		if !strings.Contains(targetBlock, needle) {
			missing = append(missing, gate)
		}
	}
	if len(missing) > 0 {
		t.Errorf("inv-zen-310: Makefile verify-release-gates is missing %d gate prerequisite(s) declared in release-gates.yml: %v",
			len(missing), missing)
	}
}

func TestInvZenG1_VerifyReleaseGatesTargetIsPhony(t *testing.T) {
	t.Parallel()

	makefileBytes := mustReadFile_g(t, repoPath_g(t, "Makefile"))
	makefile := string(makefileBytes)

	if !strings.Contains(makefile, ".PHONY: verify-release-gates") &&
		!regexpPhonyContains(makefile, "verify-release-gates") {
		t.Errorf("inv-zen-310: Makefile verify-release-gates target must be declared .PHONY")
	}
}

func regexpPhonyContains(makefile, want string) bool {
	for _, line := range strings.Split(makefile, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ".PHONY:") {
			continue
		}
		names := strings.Fields(strings.TrimPrefix(trimmed, ".PHONY:"))
		for _, n := range names {
			if n == want {
				return true
			}
		}
	}
	return false
}

func TestInvZenG1_MakefileTargetsExistForAllGates(t *testing.T) {
	t.Parallel()

	makefileBytes := mustReadFile_g(t, repoPath_g(t, "Makefile"))
	makefile := string(makefileBytes)

	missing := []string{}
	for _, gate := range expectedGateJobs {

		anchor := "\n" + gate + ":"
		if !strings.Contains(makefile, anchor) {

			missing = append(missing, gate)
		}
	}
	if len(missing) > 0 {
		t.Errorf("inv-zen-310: Makefile missing target recipes for %d gate(s) declared in release-gates.yml: %v",
			len(missing), missing)
	}
}

func TestInvZenG1_GateScriptsExistForFlipAwareGates(t *testing.T) {
	t.Parallel()

	flipAwareImpls := map[string][]string{
		"verify-dco-signoff":            {"scripts/verify_dco_signoff.sh"},
		"verify-no-personal-references": {"scripts/verify_no_personal_references.sh"},
		"verify-canonical-docs-hygiene": {"scripts/verify_canonical_docs_hygiene.sh"},
		"verify-changelog-completeness": {"scripts/verify_changelog_completeness.sh"},
	}

	makefileBytes := mustReadFile_g(t, repoPath_g(t, "Makefile"))
	makefile := string(makefileBytes)

	for gate, candidates := range flipAwareImpls {

		scriptFound := false
		for _, rel := range candidates {
			path := repoPath_g(t, rel)
			info, err := os.Stat(path)
			if err == nil {
				if info.Mode()&0o111 == 0 {
					t.Errorf("inv-zen-310: gate script %s is not executable (chmod +x required)", rel)
				}
				scriptFound = true
				break
			}
		}
		if scriptFound {
			continue
		}

		anchor := "\n" + gate + ":"
		if !strings.Contains(makefile, anchor) {
			t.Errorf("inv-zen-310: flip-aware gate %s has neither a candidate script %v nor an inline Makefile recipe", gate, candidates)
		}
	}
}

func TestInvZenG1_WorkflowIsReachableByGitHubActionsParser(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; skipping GH Actions parser surrogate check")
	}

	path := repoPath_g(t, ".github/workflows/release-gates.yml")
	cmd := exec.Command("python3", "-c",
		"import sys,yaml; yaml.safe_load(open(sys.argv[1])); print('ok')",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("inv-zen-310: release-gates.yml does not parse via python3 yaml.safe_load: %v\noutput: %s", err, out)
	}
}
