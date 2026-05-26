package compliance

import (
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/tmuxlife"
)

func TestInvZen119DoctrineMaxScopeInfinity(t *testing.T) {
	got := tmuxlife.DoctrineIdleTTL(doctrine.NameMaxScope)
	if !tmuxlife.IdleTTLIsInfinity(got) {
		t.Errorf("DoctrineIdleTTL(max-scope) = %v, want infinity (IdleTTLInfinity = -1)", int(got))
	}

	if int(got) != tmuxlife.IdleTTLInfinity {
		t.Errorf("DoctrineIdleTTL(max-scope) = %d, want IdleTTLInfinity = %d", int(got), tmuxlife.IdleTTLInfinity)
	}
}

func TestInvZen119DoctrineDefault24h(t *testing.T) {
	got := tmuxlife.DoctrineIdleTTL(doctrine.NameDefault)
	if int(got) != 24 {
		t.Errorf("DoctrineIdleTTL(default) = %d, want 24", int(got))
	}
	if tmuxlife.IdleTTLIsInfinity(got) {
		t.Errorf("DoctrineIdleTTL(default) reported as infinity; matrix carriers diverged")
	}
}

func TestInvZen119DoctrineCapaFirewall4h(t *testing.T) {
	got := tmuxlife.DoctrineIdleTTL(doctrine.NameCapaFirewall)
	if int(got) != 4 {
		t.Errorf("DoctrineIdleTTL(capa-firewall) = %d, want 4", int(got))
	}
	if tmuxlife.IdleTTLIsInfinity(got) {
		t.Errorf("DoctrineIdleTTL(capa-firewall) reported as infinity; matrix carriers diverged")
	}
}

// TestInvZen119UnknownDoctrinePanics asserts the spec's "panic on unknown
// doctrine" contract. The function explicitly diverges from
// quota.DoctrineDefaults (which falls back to "default" on unknown) per
// the rationale documented in internal/tmuxlife/lifecycle.go:
//
//   - quota's fallback is conservative because cost-side overshoot is
//     recoverable (operator notices, refunds, adjusts threshold);
//   - tmuxlife mismapping silently leaves stale tmux sessions running
//     for hours past the intended TTL — an inv-zen-119 violation. The
//     panic path keeps the bug visible.
//
// Callers consuming untrusted input MUST validate via doctrine.IsValid
// BEFORE calling DoctrineIdleTTL.
func TestInvZen119UnknownDoctrinePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("DoctrineIdleTTL(unknown) did not panic; inv-zen-119 fail-closed contract violated")
			return
		}

		s, ok := r.(string)
		if !ok {
			return
		}
		if !strings.Contains(s, "DoctrineIdleTTL") {
			t.Errorf("panic message %q does not reference DoctrineIdleTTL; diagnostic clarity at risk", s)
		}
	}()
	_ = tmuxlife.DoctrineIdleTTL(doctrine.Name("typo-or-drift"))
}

func TestInvZen119EmptyDoctrinePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("DoctrineIdleTTL(\"\") did not panic; inv-zen-119 fail-closed contract violated for empty input")
		}
	}()
	_ = tmuxlife.DoctrineIdleTTL(doctrine.Name(""))
}

func TestInvZen119DoctrineNamesByteIdentical(t *testing.T) {
	cases := map[doctrine.Name]string{
		doctrine.NameMaxScope:     "max-scope",
		doctrine.NameDefault:      "default",
		doctrine.NameCapaFirewall: "capa-firewall",
	}
	for d, want := range cases {
		if string(d) != want {
			t.Errorf("doctrine.Name const = %q, want canonical %q; inv-zen-119 byte-identity violated", string(d), want)
		}
	}
}

func TestInvZen119MatrixExhaustiveCoverage(t *testing.T) {
	// Enumerate the three canonical doctrine names. If a fourth doctrine
	// is added in a future plan, doctrine.IsValid will accept it AND
	// this slice MUST be extended in the same change.
	all := []doctrine.Name{
		doctrine.NameMaxScope,
		doctrine.NameDefault,
		doctrine.NameCapaFirewall,
	}
	for _, d := range all {
		if !doctrine.IsValid(d) {
			t.Fatalf("doctrine.IsValid(%q) = false; canonical name became invalid (schema drift)", d)
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DoctrineIdleTTL(%q) panicked %v; valid doctrine treated as unknown", d, r)
				}
			}()
			_ = tmuxlife.DoctrineIdleTTL(d)
		}()
	}
}
