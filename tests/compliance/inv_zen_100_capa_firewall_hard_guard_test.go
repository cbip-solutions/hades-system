// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/inv_zen_100_capa_firewall_hard_guard_test.go
//
// invariant (capa-firewall autonomy hard guard, spec §10.2.5):
//
// When the active doctrine is "capa-firewall", autonomy.Resolve MUST
// force ModeManual irrespective of any per-project or per-build-flag
// override. The Source field of the resulting Resolution MUST be
// SourceCapaFirewallGuard. When a non-manual override was attempted,
// the highest-precedence such attempt MUST be recorded in
// Resolution.RejectedOverride so the caller can emit
// AutonomyOverrideRejected to the event log.
//
// Tests:
// 1. TestInvZen100_CapaFirewallHardGuard_ExhaustiveOverrides — exhaustive
// cartesian product {nil, manual, semi, full}^2 × capa-firewall asserts
// Mode=manual + Source=capa-firewall-guard + correct RejectedOverride.
// 2. TestInvZen100_NonCapaFirewall_NotGuarded — the hard guard is
// strictly a function of doctrine name; max-scope + flag=full must
// resolve to ModeFull from SourceBuildFlag (not the guard).
package compliance_test

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

func TestInvZen100_CapaFirewallHardGuard_ExhaustiveOverrides(t *testing.T) {
	modeOpts := []*autonomy.Mode{
		nil,
		modePtrCompliance(autonomy.ModeManual),
		modePtrCompliance(autonomy.ModeSemi),
		modePtrCompliance(autonomy.ModeFull),
	}
	for _, flag := range modeOpts {
		for _, proj := range modeOpts {
			t.Run(label("flag", flag)+"/"+label("proj", proj), func(t *testing.T) {
				got := autonomy.Resolve(autonomy.ResolveInput{
					Doctrine:      "capa-firewall",
					BuildFlag:     flag,
					ProjectConfig: proj,
				})
				if got.Mode != autonomy.ModeManual {
					t.Fatalf("inv-zen-100 violated: capa-firewall must force manual; got %v", got.Mode)
				}
				if got.Source != autonomy.SourceCapaFirewallGuard {
					t.Fatalf("inv-zen-100 violated: source must be capa-firewall-guard; got %v", got.Source)
				}
				wantRej := highestNonManualAttempt(flag, proj)
				switch {
				case wantRej == nil && got.RejectedOverride != nil:
					t.Fatalf("expected no RejectedOverride, got %+v", got.RejectedOverride)
				case wantRej != nil && got.RejectedOverride == nil:
					t.Fatalf("expected RejectedOverride %+v, got nil", *wantRej)
				case wantRej != nil:
					if got.RejectedOverride.AttemptedMode != wantRej.AttemptedMode ||
						got.RejectedOverride.AttemptedFrom != wantRej.AttemptedFrom {
						t.Fatalf("RejectedOverride mismatch: want %+v got %+v", *wantRej, *got.RejectedOverride)
					}
				}
			})
		}
	}
}

func TestInvZen100_NonCapaFirewall_NotGuarded(t *testing.T) {
	got := autonomy.Resolve(autonomy.ResolveInput{
		Doctrine:  "max-scope",
		BuildFlag: modePtrCompliance(autonomy.ModeFull),
	})
	if got.Source == autonomy.SourceCapaFirewallGuard {
		t.Fatalf("max-scope must not trigger capa-firewall-guard; got %+v", got)
	}
	if got.Mode != autonomy.ModeFull {
		t.Fatalf("max-scope + flag=full: want full, got %v", got.Mode)
	}
}

func modePtrCompliance(m autonomy.Mode) *autonomy.Mode { return &m }

func label(name string, m *autonomy.Mode) string {
	if m == nil {
		return name + "=nil"
	}
	return name + "=" + m.String()
}

func highestNonManualAttempt(flag, proj *autonomy.Mode) *autonomy.RejectedOverride {
	if flag != nil && *flag != autonomy.ModeManual {
		return &autonomy.RejectedOverride{AttemptedMode: *flag, AttemptedFrom: autonomy.SourceBuildFlag}
	}
	if proj != nil && *proj != autonomy.ModeManual {
		return &autonomy.RejectedOverride{AttemptedMode: *proj, AttemptedFrom: autonomy.SourceProjectConfig}
	}
	return nil
}
