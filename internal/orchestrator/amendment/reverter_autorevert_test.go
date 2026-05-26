package amendment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestAutoRevertEmitsAutonomousRevertedAfterDelegateSucceeds(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
	})

	reason := "cost aggregator: pct_passing=0.500 below threshold=0.700 over 10 sessions"
	if err := r.AutoRevert(context.Background(), 20, reason); err != nil {
		t.Fatalf("AutoRevert: %v", err)
	}

	var autonomous, operator bool
	for _, ev := range em.snapshot() {
		switch ev.typ {
		case eventlog.EvtDoctrineAutonomousReverted:
			autonomous = true
			if got, _ := ev.payload["telemetry_category"].(string); got != "cost" {
				t.Errorf("telemetry_category=%q, want cost", got)
			}
			if got, _ := ev.payload["reason"].(string); got != reason {
				t.Errorf("reason=%q, want %q", got, reason)
			}
			if got, _ := ev.payload["adr_id"].(string); got != "ADR-0020" {
				t.Errorf("adr_id=%q, want ADR-0020", got)
			}
		case eventlog.EvtDoctrineAmendmentReverted:
			operator = true
		}
	}
	if !autonomous {
		t.Errorf("expected DoctrineAutonomousReverted emission")
	}
	if !operator {
		t.Errorf("expected DoctrineAmendmentReverted emission (Plan 5 inner Revert)")
	}
}

func TestAutoRevertCarriesMergeAndRecoveryCategoryLabels(t *testing.T) {
	for _, c := range []string{"merge", "recovery"} {
		t.Run(c, func(t *testing.T) {
			dir := initTrackedRepo(t)
			em := &fakeEmitter{}
			applyAmendment(t, dir, em, 20)
			r := amendment.NewReverter(amendment.ReverterConfig{
				RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
			})

			reason := c + " aggregator: pct_passing=0.500 below threshold=0.700 over 10 sessions"
			if err := r.AutoRevert(context.Background(), 20, reason); err != nil {
				t.Fatalf("AutoRevert: %v", err)
			}

			var found bool
			for _, ev := range em.snapshot() {
				if ev.typ == eventlog.EvtDoctrineAutonomousReverted {
					if got, _ := ev.payload["telemetry_category"].(string); got == c {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("expected telemetry_category=%q", c)
			}
		})
	}
}

func TestAutoRevertMalformedReasonStillEmits(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot: dir, Emitter: em, ReloadSignal: &fakeReloadSignal{},
	})

	reason := "BOGUS REASON STRING"
	if err := r.AutoRevert(context.Background(), 20, reason); err != nil {
		t.Fatalf("AutoRevert: %v", err)
	}

	for _, ev := range em.snapshot() {
		if ev.typ == eventlog.EvtDoctrineAutonomousReverted {
			if got, _ := ev.payload["telemetry_category"].(string); got != "" {
				t.Errorf("telemetry_category=%q, want \"\" for non-canonical reason", got)
			}
			if got, _ := ev.payload["reason"].(string); got != reason {
				t.Errorf("reason=%q, want %q", got, reason)
			}
			return
		}
	}
	t.Errorf("expected DoctrineAutonomousReverted emission even on malformed reason")
}

func TestAutoRevertPropagatesInnerRevertError(t *testing.T) {
	dir := initTrackedRepo(t)
	em := &fakeEmitter{}
	applyAmendment(t, dir, em, 20)
	r := amendment.NewReverter(amendment.ReverterConfig{
		RepoRoot:     dir,
		Emitter:      em,
		ReloadSignal: &fakeReloadSignal{},
		Git:          &failingGit{failOn: "revert"},
	})

	err := r.AutoRevert(context.Background(), 20, "cost aggregator: x")
	if err == nil {
		t.Fatalf("AutoRevert silently swallowed inner Revert error")
	}
	if !strings.Contains(err.Error(), "AutoRevert ADR-0020: inner Revert") {
		t.Errorf("err = %v, want AutoRevert ADR-0020 inner Revert wrapping", err)
	}

	for _, ev := range em.snapshot() {
		if ev.typ == eventlog.EvtDoctrineAutonomousReverted {
			t.Errorf("DoctrineAutonomousReverted emitted on failed AutoRevert; events=%+v", em.snapshot())
		}
	}
}

func TestAutoRevertSatisfiesTelemetrySubscriberAutoReverterContract(t *testing.T) {
	var ar amendment.AutoReverter = (*amendment.AmendmentReverter)(nil)
	_ = ar

	_ = errors.New
}
