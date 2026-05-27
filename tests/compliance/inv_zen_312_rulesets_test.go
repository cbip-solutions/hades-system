// tests/compliance/inv_zen_312_rulesets_test.go
//
// invariant —.github/rulesets/main-branch.json
// committed + Rulesets applied post public flip to
// cbip-solutions/hades-system PUBLIC repo. Modelo B
// dual-repo policy: PRIVATE dev source runs release-gates.yml without
// the strict ruleset (operator velocity); PUBLIC mirror enforces.
//
// Compile-check slice: ruleset JSON file MUST exist + parse as valid GitHub
// Rulesets API schema + declare 27 release-gate gate job names as required
// status checks (one-to-one parity vs release-gates.yml) + linear-history +
// signed-commits + restrict-deletions rules + bypass-actors list +
// scripts/apply-rulesets.sh executable wrapper.
//
// Runtime check (operator-paced post-flip): `gh api
// /repos/cbip-solutions/hades-system/rulesets` returns matching ruleset entry. Guarded by
// ZEN_INTEGRATION_LIVE_GH=1 env var (skipped by default).
//
// Helper sharing: repoPath_g + mustReadFile_g defined in
// inv_zen_310_release_gates_composite_test.go (same compliance_test package).
//
// Three-place triple:
//
// (1) spec §7.7 invariant text
// (2) this compliance test (file existence + JSON schema + parity)
// (3).github/rulesets/main-branch.json + scripts/apply-rulesets.sh
package compliance_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestInvZenG2_RulesetsJsonExists(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, ".github/rulesets/main-branch.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("inv-zen-312: .github/rulesets/main-branch.json not found: %v", err)
	}
}

func TestInvZenG2_RulesetsJsonParsesAsValidGhRuleset(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/rulesets/main-branch.json"))

	var ruleset struct {
		Name        string `json:"name"`
		Target      string `json:"target"`
		Enforcement string `json:"enforcement"`
		Conditions  struct {
			RefName struct {
				Include []string `json:"include"`
				Exclude []string `json:"exclude"`
			} `json:"ref_name"`
		} `json:"conditions"`
		Rules        []map[string]interface{} `json:"rules"`
		BypassActors []map[string]interface{} `json:"bypass_actors"`
	}
	if err := json.Unmarshal(data, &ruleset); err != nil {
		t.Fatalf("inv-zen-312: main-branch.json is not valid JSON or schema mismatch: %v", err)
	}

	if ruleset.Name == "" {
		t.Error("inv-zen-312: ruleset.name is empty")
	}
	if ruleset.Target != "branch" {
		t.Errorf("inv-zen-312: ruleset.target = %q, want \"branch\"", ruleset.Target)
	}
	if ruleset.Enforcement != "active" {
		t.Errorf("inv-zen-312: ruleset.enforcement = %q, want \"active\"", ruleset.Enforcement)
	}
	if len(ruleset.Conditions.RefName.Include) == 0 {
		t.Error("inv-zen-312: ruleset.conditions.ref_name.include is empty")
	}
	if len(ruleset.Rules) == 0 {
		t.Error("inv-zen-312: ruleset.rules is empty")
	}
	if len(ruleset.BypassActors) == 0 {
		t.Error("inv-zen-312: ruleset.bypass_actors is empty (no operator override list)")
	}
}

func TestInvZenG2_RulesetsRequiredStatusChecksList(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/rulesets/main-branch.json"))

	var ruleset struct {
		Rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				RequiredStatusChecks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
				StrictRequiredStatusChecksPolicy bool `json:"strict_required_status_checks_policy"`
			} `json:"parameters"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &ruleset); err != nil {
		t.Fatalf("inv-zen-312: json unmarshal: %v", err)
	}

	var requiredChecks []string
	for _, rule := range ruleset.Rules {
		if rule.Type == "required_status_checks" {
			for _, check := range rule.Parameters.RequiredStatusChecks {
				requiredChecks = append(requiredChecks, check.Context)
			}
		}
	}

	checkSet := make(map[string]bool, len(requiredChecks))
	for _, c := range requiredChecks {
		checkSet[c] = true
	}

	missing := []string{}
	for _, gate := range expectedGateJobs {
		if !checkSet[gate] {
			missing = append(missing, gate)
		}
	}
	if len(missing) > 0 {
		t.Errorf("inv-zen-312: required_status_checks missing %d expected gates (drift vs release-gates.yml): %v",
			len(missing), missing)
	}
}

func TestInvZenG2_RulesetsLinearHistoryDeletionSignatures(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/rulesets/main-branch.json"))

	var ruleset struct {
		Rules []struct {
			Type string `json:"type"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &ruleset); err != nil {
		t.Fatalf("inv-zen-312: json unmarshal: %v", err)
	}

	hasLinear := false
	hasDeletion := false
	hasSignatures := false
	for _, rule := range ruleset.Rules {
		switch rule.Type {
		case "non_fast_forward":
			hasLinear = true
		case "deletion":
			hasDeletion = true
		case "required_signatures":
			hasSignatures = true
		}
	}

	if !hasLinear {
		t.Error("inv-zen-312: ruleset missing non_fast_forward rule (linear history requirement)")
	}
	if !hasDeletion {
		t.Error("inv-zen-312: ruleset missing deletion rule (restrict deletions)")
	}
	if !hasSignatures {
		t.Error("inv-zen-312: ruleset missing required_signatures rule (signed commits per Phase D sigstore)")
	}
}

func TestInvZenG2_RulesetsBypassActorsConfigured(t *testing.T) {
	t.Parallel()

	data := mustReadFile_g(t, repoPath_g(t, ".github/rulesets/main-branch.json"))

	var ruleset struct {
		BypassActors []struct {
			ActorID    interface{} `json:"actor_id"`
			ActorType  string      `json:"actor_type"`
			BypassMode string      `json:"bypass_mode"`
		} `json:"bypass_actors"`
	}
	if err := json.Unmarshal(data, &ruleset); err != nil {
		t.Fatalf("inv-zen-312: json unmarshal: %v", err)
	}

	if len(ruleset.BypassActors) == 0 {
		t.Fatal("inv-zen-312: ruleset.bypass_actors is empty; expected operator override list")
	}

	for i, actor := range ruleset.BypassActors {
		if actor.ActorType == "" {
			t.Errorf("inv-zen-312: bypass_actors[%d].actor_type is empty", i)
		}
		if actor.BypassMode != "always" && actor.BypassMode != "pull_request" {
			t.Errorf("inv-zen-312: bypass_actors[%d].bypass_mode = %q; expected \"always\" or \"pull_request\"",
				i, actor.BypassMode)
		}
	}
}

func TestInvZenG2_ApplyRulesetsScriptExists(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/apply-rulesets.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inv-zen-312: scripts/apply-rulesets.sh not found: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("inv-zen-312: scripts/apply-rulesets.sh is not executable (chmod +x needed)")
	}

	data := mustReadFile_g(t, path)
	content := string(data)
	for _, expected := range []string{
		"gh api",
		"rulesets",
		"main-branch.json",
		"--dry-run",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("inv-zen-312: apply-rulesets.sh missing expected token %q", expected)
		}
	}
}
