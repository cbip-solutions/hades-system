package amendment_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func writeFile(p string, b []byte) error     { return os.WriteFile(p, b, 0o644) }
func mkdirAll(p string, m os.FileMode) error { return os.MkdirAll(p, m) }
func chmod(p string, m os.FileMode) error    { return os.Chmod(p, m) }

type recordedEvent struct {
	typ     eventlog.EventType
	payload map[string]any
}

type fakeEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
	err    error
}

func (f *fakeEmitter) Append(_ context.Context, e eventlog.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}

	cp := make(map[string]any, len(e.Payload))
	for k, v := range e.Payload {
		cp[k] = v
	}
	f.events = append(f.events, recordedEvent{typ: e.Type, payload: cp})
	return nil
}

func (f *fakeEmitter) snapshot() []recordedEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedEvent, len(f.events))
	copy(out, f.events)
	return out
}

type fakeDrafter struct {
	body amendment.ADRBody
	err  error
	got  []amendment.Evidence
	mu   sync.Mutex
}

func (f *fakeDrafter) Draft(_ context.Context, ev amendment.Evidence) (amendment.ADRBody, error) {
	f.mu.Lock()
	f.got = append(f.got, ev)
	f.mu.Unlock()
	return f.body, f.err
}

type fakeAllocator struct {
	next int
	err  error
}

func (f *fakeAllocator) NextAvailableID(_ context.Context, _ string) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.next, nil
}

type fakeCooldown struct {
	armed map[string]bool
	mu    sync.Mutex
	calls []struct{ pattern, doctrine string }
}

func (f *fakeCooldown) Suppressed(pattern string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.armed == nil {
		return false
	}
	return f.armed[pattern]
}
func (f *fakeCooldown) Arm(pattern, doctrine string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct{ pattern, doctrine string }{pattern, doctrine})
}

func newProposer(t *testing.T, dir string, doctrine string, em *fakeEmitter, drafter *fakeDrafter, alloc *fakeAllocator, cd *fakeCooldown, clk clock.Clock) *amendment.AmendmentProposer {
	t.Helper()
	return amendment.NewProposer(amendment.ProposerConfig{
		DecisionsDir: dir,
		Doctrine:     doctrine,
		Emitter:      em,
		Drafter:      drafter,
		Allocator:    alloc,
		Cooldown:     cd,
		Clock:        clk,
	})
}

func TestProposerTriggersOnMaxScopeOverrideThreshold(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	body := amendment.ADRBody{Title: "max-scope override pattern", Markdown: "# ADR\n"}
	drafter := &fakeDrafter{body: body}
	p := newProposer(t, dir, "max-scope", em, drafter, &fakeAllocator{next: 20}, &fakeCooldown{}, clk)

	for i := 0; i < 5; i++ {
		if err := p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		}); err != nil {
			t.Fatalf("OnEvent[%d] err=%v", i, err)
		}
	}

	got := em.snapshot()
	if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentProposed {
		t.Fatalf("want exactly 1 EvtDoctrineAmendmentProposed, got %+v", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "proposed", "0020-*.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected 1 ADR file at proposed/0020-*.md, got %v err=%v", matches, err)
	}
	if drafter.got[0].Doctrine != "max-scope" || drafter.got[0].TriggerClass != "operator_override" || drafter.got[0].Count != 5 {
		t.Fatalf("evidence wrong: %+v", drafter.got[0])
	}
}

func TestProposerSuppressesWhenCooldownArmed(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	cd := &fakeCooldown{armed: map[string]bool{
		"max-scope|operator_override|tier_select|p1": true,
	}}
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 21}, cd, clk)
	for i := 0; i < 6; i++ {
		if err := p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		}); err != nil {
			t.Fatalf("OnEvent err=%v", err)
		}
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentSuppressed {
		t.Fatalf("want 1 EvtDoctrineAmendmentSuppressed (reason=cooldown), got %+v", got)
	}
	if r, _ := got[0].payload["reason"].(string); r != "cooldown" {
		t.Fatalf("want reason=cooldown, got %q", r)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "proposed", "*.md"))
	if len(matches) != 0 {
		t.Fatalf("no ADR file expected on cooldown suppression, got %v", matches)
	}
}

func TestProposerSlidingWindowDropsOldEvents(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 22}, &fakeCooldown{}, clk)
	for i := 0; i < 4; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now(),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
	}
	clk.Advance(25 * time.Hour)
	_ = p.OnEvent(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOperatorOverrideApplied,
		Timestamp: clk.Now(),
		Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
	})
	if got := em.snapshot(); len(got) != 0 {
		t.Fatalf("expected zero events emitted (sliding window), got %+v", got)
	}
}

func TestProposerDrafterErrorEmitsSuppressed(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{err: errors.New("L4 unreachable")}, &fakeAllocator{next: 23}, &fakeCooldown{}, clk)
	for i := 0; i < 6; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentSuppressed {
		t.Fatalf("want EvtDoctrineAmendmentSuppressed (reason=drafter_failed), got %+v", got)
	}
	if r, _ := got[0].payload["reason"].(string); r != "drafter_failed" {
		t.Fatalf("want reason=drafter_failed, got %q", r)
	}
}

func TestProposerAllocatorErrorEmitsSuppressed(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{err: errors.New("range exhausted")}, &fakeCooldown{}, clk)
	for i := 0; i < 6; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentSuppressed {
		t.Fatalf("want EvtDoctrineAmendmentSuppressed (range_exhausted), got %+v", got)
	}
	if r, _ := got[0].payload["reason"].(string); r != "range_exhausted" {
		t.Fatalf("want reason=range_exhausted, got %q", r)
	}
}

func TestProposerIgnoresUnclassifiedEvents(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{}, &fakeAllocator{}, &fakeCooldown{}, clk)
	if err := p.OnEvent(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		Timestamp: clk.Now(),
	}); err != nil {
		t.Fatalf("OnEvent unrelated err=%v", err)
	}
	if got := em.snapshot(); len(got) != 0 {
		t.Fatalf("unrelated event should not emit, got %+v", got)
	}
}

func TestProposerLowSeverityCostDegradationIgnored(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 24}, &fakeCooldown{}, clk)
	for i := 0; i < 5; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtBudgetDegradationApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"severity": "soft", "project_id": "p1"},
		})
	}
	if got := em.snapshot(); len(got) != 0 {
		t.Fatalf("low severity should not count toward threshold, got %+v", got)
	}
}

func TestProposerCostDegradationMediumTriggers(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "cost pattern", Markdown: "# x"}}, &fakeAllocator{next: 25}, &fakeCooldown{}, clk)

	for i := 0; i < 3; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtBudgetDegradationApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"severity": "medium", "project_id": "p1"},
		})
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentProposed {
		t.Fatalf("want 1 Proposed for cost_degradation, got %+v", got)
	}
}

func TestProposerEscalationL4Triggers(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "esc pattern", Markdown: "# x"}}, &fakeAllocator{next: 26}, &fakeCooldown{}, clk)

	for i := 0; i < 2; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtEscalationDecision,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"destination": "L4", "project_id": "p1"},
		})
	}
	got := em.snapshot()
	if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentProposed {
		t.Fatalf("want 1 Proposed for escalation, got %+v", got)
	}
}

func TestProposerNonL4EscalationIgnored(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{}, &fakeAllocator{next: 27}, &fakeCooldown{}, clk)
	for i := 0; i < 4; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtEscalationDecision,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"destination": "L2", "project_id": "p1"},
		})
	}
	if got := em.snapshot(); len(got) != 0 {
		t.Fatalf("non-L4 escalation must not count, got %+v", got)
	}
}

func TestProposerUnknownDoctrineNeverFires(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "unknown-doctrine", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 28}, &fakeCooldown{}, clk)
	for i := 0; i < 50; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
	}
	if got := em.snapshot(); len(got) != 0 {
		t.Fatalf("unknown doctrine must never trigger, got %+v", got)
	}
}

func TestProposerResetsWindowAfterPropose(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "first", Markdown: "x"}}, &fakeAllocator{next: 20}, &fakeCooldown{}, clk)
	for i := 0; i < 5; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
	}

	if got := em.snapshot(); len(got) != 1 {
		t.Fatalf("expected first propose, got %d events", len(got))
	}

	_ = p.OnEvent(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOperatorOverrideApplied,
		Timestamp: clk.Now().Add(10 * time.Minute),
		Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
	})
	if got := em.snapshot(); len(got) != 1 {
		t.Fatalf("window must reset after propose; got %d events", len(got))
	}
}

func TestProposerSlugifyEdgeCases(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))

	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "!!!@@@***", Markdown: "x"}}, &fakeAllocator{next: 20}, &fakeCooldown{}, clk)
	for i := 0; i < 5; i++ {
		_ = p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "proposed", "0020-amendment.md"))
	if len(matches) != 1 {
		t.Fatalf("expected 0020-amendment.md, got %v", matches)
	}

	dir2 := t.TempDir()
	em2 := &fakeEmitter{}
	long := "Some VERY long Title with UPPER and _under_score and-dashes that exceeds sixty chars in length yes-it-does"
	p2 := newProposer(t, dir2, "max-scope", em2, &fakeDrafter{body: amendment.ADRBody{Title: long, Markdown: "x"}}, &fakeAllocator{next: 21}, &fakeCooldown{}, clk)
	for i := 0; i < 5; i++ {
		_ = p2.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p2"},
		})
	}
	matches2, _ := filepath.Glob(filepath.Join(dir2, "proposed", "0021-*.md"))
	if len(matches2) != 1 {
		t.Fatalf("expected 0021-*.md, got %v", matches2)
	}
	base := filepath.Base(matches2[0])

	if len(base) > 68 {
		t.Fatalf("filename too long: %q", base)
	}
}

func TestProposerNewProposerPanicsOnNilCollaborator(t *testing.T) {
	clk := clock.NewFake(time.Now())
	em := &fakeEmitter{}
	d := &fakeDrafter{}
	a := &fakeAllocator{}
	cd := &fakeCooldown{}

	cases := []struct {
		name string
		cfg  amendment.ProposerConfig
	}{
		{"nil emitter", amendment.ProposerConfig{Drafter: d, Allocator: a, Cooldown: cd, Clock: clk}},
		{"nil drafter", amendment.ProposerConfig{Emitter: em, Allocator: a, Cooldown: cd, Clock: clk}},
		{"nil allocator", amendment.ProposerConfig{Emitter: em, Drafter: d, Cooldown: cd, Clock: clk}},
		{"nil cooldown", amendment.ProposerConfig{Emitter: em, Drafter: d, Allocator: a, Clock: clk}},
		{"nil clock", amendment.ProposerConfig{Emitter: em, Drafter: d, Allocator: a, Cooldown: cd}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for %s", c.name)
				}
			}()
			amendment.NewProposer(c.cfg)
		})
	}
}

func TestProposerDefaultDoctrineThresholds(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name      string
		evtType   eventlog.EventType
		payload   map[string]any
		count     int
		nextAlloc int
	}{
		{"override", eventlog.EvtOperatorOverrideApplied, map[string]any{"override_class": "tier_select", "project_id": "p1"}, 8, 30},
		{"cost", eventlog.EvtBudgetDegradationApplied, map[string]any{"severity": "hard", "project_id": "p1"}, 5, 31},
		{"esc", eventlog.EvtEscalationDecision, map[string]any{"destination": "L4", "project_id": "p1"}, 3, 32},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			emit := &fakeEmitter{}
			p := newProposer(t, dir, "default", emit, &fakeDrafter{body: amendment.ADRBody{Title: tc.name, Markdown: "x"}}, &fakeAllocator{next: tc.nextAlloc}, &fakeCooldown{}, clk)
			for i := 0; i < tc.count; i++ {
				_ = p.OnEvent(context.Background(), eventlog.Event{
					Type:      tc.evtType,
					Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
					Payload:   tc.payload,
				})
			}
			got := emit.snapshot()
			if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentProposed {
				t.Fatalf("default %s: want proposed, got %+v", tc.name, got)
			}
			_ = em
		})
	}
}

func TestProposerCapaFirewallDoctrineThresholds(t *testing.T) {
	dir := t.TempDir()
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name      string
		evtType   eventlog.EventType
		payload   map[string]any
		count     int
		nextAlloc int
	}{
		{"override", eventlog.EvtOperatorOverrideApplied, map[string]any{"override_class": "tier_select", "project_id": "p1"}, 12, 40},
		{"cost", eventlog.EvtBudgetDegradationApplied, map[string]any{"severity": "emergency", "project_id": "p1"}, 8, 41},
		{"esc", eventlog.EvtEscalationDecision, map[string]any{"destination": "L4", "project_id": "p1"}, 5, 42},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			emit := &fakeEmitter{}
			p := newProposer(t, dir, "capa-firewall", emit, &fakeDrafter{body: amendment.ADRBody{Title: tc.name, Markdown: "x"}}, &fakeAllocator{next: tc.nextAlloc}, &fakeCooldown{}, clk)
			for i := 0; i < tc.count; i++ {
				_ = p.OnEvent(context.Background(), eventlog.Event{
					Type:      tc.evtType,
					Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
					Payload:   tc.payload,
				})
			}
			got := emit.snapshot()
			if len(got) != 1 || got[0].typ != eventlog.EvtDoctrineAmendmentProposed {
				t.Fatalf("capa-firewall %s: want proposed, got %+v", tc.name, got)
			}
		})
	}
}

func TestProposerMkdirError(t *testing.T) {

	dir := t.TempDir()

	conflict := filepath.Join(dir, "proposed")
	if err := writeFile(conflict, []byte("not a dir")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 99}, &fakeCooldown{}, clk)
	var lastErr error
	for i := 0; i < 5; i++ {
		err := p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		t.Fatalf("expected mkdir error to surface")
	}
}

func TestProposerWriteFileError(t *testing.T) {

	dir := t.TempDir()
	proposedDir := filepath.Join(dir, "proposed")
	if err := mkdirAll(proposedDir, 0o555); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _ = chmod(proposedDir, 0o755) })

	em := &fakeEmitter{}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 99}, &fakeCooldown{}, clk)
	var lastErr error
	for i := 0; i < 5; i++ {
		err := p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		t.Skip("filesystem allowed write to read-only dir (running as root or on a permissive FS)")
	}
}

func TestProposerEmitterErrorBubblesUp(t *testing.T) {
	dir := t.TempDir()
	em := &fakeEmitter{err: errors.New("emit failed")}
	clk := clock.NewFake(time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC))
	p := newProposer(t, dir, "max-scope", em, &fakeDrafter{body: amendment.ADRBody{Title: "x", Markdown: "x"}}, &fakeAllocator{next: 20}, &fakeCooldown{}, clk)
	var lastErr error
	for i := 0; i < 5; i++ {
		err := p.OnEvent(context.Background(), eventlog.Event{
			Type:      eventlog.EvtOperatorOverrideApplied,
			Timestamp: clk.Now().Add(time.Duration(i) * time.Minute),
			Payload:   map[string]any{"override_class": "tier_select", "project_id": "p1"},
		})
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		t.Fatalf("expected emitter error to surface")
	}
}
