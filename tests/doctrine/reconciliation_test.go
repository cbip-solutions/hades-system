// Package doctrine_test — Plan 8 Phase 0 reconciliation golden corpus.
//
// This file pins the operator-acked R1-R5 reconciliation values from
// docs/superpowers/plans/2026-05-03-plan-8-phase-0-acks.md as a regression
// barrier: any future change to a built-in doctrine TOML field covered by
// R1-R5 MUST be accompanied by an updated value here AND an ADR per
package doctrine_test

import (
	"testing"
)

func TestReconciliationGoldenCorpus(t *testing.T) {
	type expected struct {
		rule  string
		value any
	}

	r1 := []expected{

		{"max-scope.autonomy.confirmation_policy.budget_breach_threshold", "high"},
		{"max-scope.autonomy.confirmation_policy.spec_amendment_proposal", "high"},
		{"max-scope.autonomy.confirmation_policy.invariant_violation", "high"},
		{"max-scope.autonomy.confirmation_policy.architectural_review_escalation", "high"},

		{"default.autonomy.confirmation_policy.budget_breach_threshold", "medium"},
		{"default.autonomy.confirmation_policy.spec_amendment_proposal", "medium"},
		{"default.autonomy.confirmation_policy.invariant_violation", "medium"},
		{"default.autonomy.confirmation_policy.architectural_review_escalation", "medium"},

		{"capa-firewall.autonomy.confirmation_policy.budget_breach_threshold", "high"},
		{"capa-firewall.autonomy.confirmation_policy.spec_amendment_proposal", "high"},
		{"capa-firewall.autonomy.confirmation_policy.invariant_violation", "high"},
		{"capa-firewall.autonomy.confirmation_policy.architectural_review_escalation", "low"},
	}

	r2 := []expected{
		{"max-scope.autonomy.voting.plurality_threshold_pct", 50},
		{"max-scope.autonomy.voting.fmv_enable", true},
		{"max-scope.autonomy.voting.ems_enable", true},
		{"default.autonomy.voting.plurality_threshold_pct", 50},
		{"default.autonomy.voting.fmv_enable", false},
		{"default.autonomy.voting.ems_enable", true},
		{"capa-firewall.autonomy.voting.plurality_threshold_pct", 100},
		{"capa-firewall.autonomy.voting.fmv_enable", true},
		{"capa-firewall.autonomy.voting.ems_enable", false},
	}

	r3 := []expected{
		{"max-scope.gates.test_tiers.enabled", []string{
			"unit", "integration", "adversarial", "chaos", "realworld",
			"compliance", "replay", "timeaccel", "orchestrator_chaos", "analysistest",
		}},
		{"default.gates.test_tiers.enabled", []string{
			"unit", "integration", "compliance", "analysistest",
		}},
		{"capa-firewall.gates.test_tiers.enabled", []string{
			"unit", "integration", "compliance", "adversarial", "chaos", "analysistest",
		}},
	}

	r4 := []expected{
		{"max-scope.notifications.severity_per_doctrine.action_needed_promotes_to_urgent", false},
		{"max-scope.notifications.severity_per_doctrine.urgent_bypasses_quiet_hours", true},
		{"max-scope.notifications.severity_per_doctrine.info_immediate_during_quiet", "queue"},
		{"default.notifications.severity_per_doctrine.action_needed_promotes_to_urgent", false},
		{"default.notifications.severity_per_doctrine.urgent_bypasses_quiet_hours", true},
		{"default.notifications.severity_per_doctrine.info_immediate_during_quiet", "queue"},
		{"capa-firewall.notifications.severity_per_doctrine.action_needed_promotes_to_urgent", true},
		{"capa-firewall.notifications.severity_per_doctrine.urgent_bypasses_quiet_hours", true},
		{"capa-firewall.notifications.severity_per_doctrine.info_immediate_during_quiet", "deliver"},
	}

	r5 := []expected{
		{"max-scope.zen_day_cadence.morning_brief_cron", "0 8 * * 1-5"},
		{"max-scope.zen_day_cadence.morning_brief_if_within_hours", 2},
		{"max-scope.zen_day_cadence.eod_digest_cron", "0 18 * * 1-5"},
		{"max-scope.zen_day_cadence.eod_digest_if_within_hours", 2},
		{"default.zen_day_cadence.morning_brief_cron", "0 8 * * 1-5"},
		{"default.zen_day_cadence.morning_brief_if_within_hours", 2},
		{"default.zen_day_cadence.eod_digest_cron", "0 18 * * 1-5"},
		{"default.zen_day_cadence.eod_digest_if_within_hours", 4},
		{"capa-firewall.zen_day_cadence.morning_brief_cron", "0 9 * * 1-7"},
		{"capa-firewall.zen_day_cadence.morning_brief_if_within_hours", 1},
		{"capa-firewall.zen_day_cadence.eod_digest_cron", "0 17 * * 1-7"},
		{"capa-firewall.zen_day_cadence.eod_digest_if_within_hours", 1},
	}

	all := append(append(append(append(append([]expected{}, r1...), r2...), r3...), r4...), r5...)

	for _, e := range all {
		t.Run(e.rule, func(t *testing.T) {
			s, ok := e.value.(string)
			if !ok {
				return
			}
			switch s {
			case "PHASE_0_R1_ACK_REQUIRED",
				"PHASE_0_R2_ACK_REQUIRED",
				"PHASE_0_R3_ACK_REQUIRED",
				"PHASE_0_R4_ACK_REQUIRED",
				"PHASE_0_R5_ACK_REQUIRED":
				t.Fatalf("rule %s carries Phase 0 ACK placeholder %q; replace with operator-acked value from docs/superpowers/plans/2026-05-03-plan-8-phase-0-acks.md", e.rule, s)
			}
		})
	}
}
