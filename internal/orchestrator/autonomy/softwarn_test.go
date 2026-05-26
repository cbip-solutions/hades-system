package autonomy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/autonomy"
)

type fakeEmitter struct {
	events  []autonomy.AutonomyEvent
	failNth int
	calls   int
}

func (f *fakeEmitter) Emit(_ context.Context, ev autonomy.AutonomyEvent) error {
	f.calls++
	if f.failNth > 0 && f.calls == f.failNth {
		return errors.New("emitter failure")
	}
	f.events = append(f.events, ev)
	return nil
}

func newEngineWithEmitter(t *testing.T, emitter autonomy.EventEmitter, checks ...autonomy.Check) *autonomy.CheckEngine {
	t.Helper()
	e, err := autonomy.NewCheckEngine(autonomy.EngineDeps{
		Checks:  checks,
		Emitter: emitter,
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewCheckEngine: %v", err)
	}
	return e
}

func TestCheckEngine_AllowSoftWarnings_EmitsBypassedEvent(t *testing.T) {
	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "index 60h stale"
		}
	}
	emitter := &fakeEmitter{}
	e := newEngineWithEmitter(t, emitter, checks...)
	out, err := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine:          "default",
		AllowSoftWarnings: true,
	})
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if !out.Proceed {
		t.Fatalf("AllowSoftWarnings=true must allow proceed despite soft failure")
	}
	if len(out.BypassedSoft) != 1 {
		t.Fatalf("expected 1 bypassed soft; got %+v", out.BypassedSoft)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("expected 1 BypassedSoftCheckEvent; got %d", len(emitter.events))
	}
	ev := emitter.events[0]
	if ev.Kind != autonomy.EventBypassedSoftCheck {
		t.Fatalf("event kind: want bypassed-soft-check; got %v", ev.Kind)
	}
	if ev.CheckName != autonomy.CheckCaronteIndexCurrency {
		t.Fatalf("event check name: got %v", ev.CheckName)
	}
	if ev.Doctrine != "default" {
		t.Fatalf("event doctrine: got %v", ev.Doctrine)
	}
	if ev.Reason == "" {
		t.Fatalf("event reason must be populated")
	}
	if ev.OccurredAt.IsZero() {
		t.Fatalf("event OccurredAt must be populated")
	}
}

func TestCheckEngine_AllowSoftWarnings_NoSoftFail_NoEmission(t *testing.T) {
	emitter := &fakeEmitter{}
	e := newEngineWithEmitter(t, emitter, allFakeCheckNames()...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine: "default", AllowSoftWarnings: true,
	})
	if !out.Proceed {
		t.Fatal("all-pass must proceed")
	}
	if len(emitter.events) != 0 {
		t.Fatalf("no soft failure → no event; got %d", len(emitter.events))
	}
}

func TestCheckEngine_NilEmitter_TolerantWhenAllowSoftWarnings(t *testing.T) {

	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "stale"
		}
	}
	e := newEngine(t, checks...)
	out, err := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine: "default", AllowSoftWarnings: true,
	})
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if !out.Proceed || len(out.BypassedSoft) != 1 {
		t.Fatalf("nil emitter must not affect aggregation; got Proceed=%v BypassedSoft=%+v", out.Proceed, out.BypassedSoft)
	}
}

func TestCheckEngine_EmitterError_DoesNotBlockProceed(t *testing.T) {

	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "stale"
		}
	}
	emitter := &fakeEmitter{failNth: 1}
	e := newEngineWithEmitter(t, emitter, checks...)
	out, err := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine: "default", AllowSoftWarnings: true,
	})
	if err != nil {
		t.Fatalf("RunCheck must not surface emitter errors: %v", err)
	}
	if !out.Proceed {
		t.Fatalf("emitter error must not affect Proceed; got blocked")
	}
}

func TestCheckEngine_AllowSoftWarnings_False_NoEmission(t *testing.T) {

	checks := allFakeCheckNames()
	for _, c := range checks {
		fc := c.(*fakeCheck)
		if fc.name == autonomy.CheckCaronteIndexCurrency {
			fc.status = autonomy.CheckFail
			fc.reason = "stale"
		}
	}
	emitter := &fakeEmitter{}
	e := newEngineWithEmitter(t, emitter, checks...)
	out, _ := e.RunCheck(context.Background(), autonomy.RunInput{
		Doctrine: "default", AllowSoftWarnings: false,
	})
	if !out.Proceed {
		t.Fatal("soft fail without bypass must still proceed")
	}
	if len(emitter.events) != 0 {
		t.Fatalf("AllowSoftWarnings=false: no audit event; got %d", len(emitter.events))
	}
}

func TestAutonomyEventKindString(t *testing.T) {
	cases := []struct {
		k    autonomy.AutonomyEventKind
		want string
	}{
		{autonomy.EventBypassedSoftCheck, "bypassed-soft-check"},
		{autonomy.EventAutonomyOverrideRejected, "autonomy-override-rejected"},
	}
	for _, c := range cases {
		if c.k.String() != c.want {
			t.Fatalf("AutonomyEventKind.String %v: want %q got %q", c.k, c.want, c.k.String())
		}
	}
	var bogus autonomy.AutonomyEventKind = 99
	if got := bogus.String(); got != "autonomy-event(99)" {
		t.Fatalf("unknown kind: got %q", got)
	}
}
