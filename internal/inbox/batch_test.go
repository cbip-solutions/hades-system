package inbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func mkBatchNotification(t *testing.T, sv Severity, ev string, at time.Time) Notification {
	t.Helper()
	return Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    sv,
		EventType:   ev,
		ContentHash: ComputeContentHash(map[string]any{"k": ev + at.String()}),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   at,
	}
}

func TestDefaultBatchWindowUrgent(t *testing.T) {
	w := DefaultBatchWindow(SeverityUrgent)
	if w.Idle != 0 || w.MaxCount != 1 || w.MaxWait != 0 {
		t.Errorf("urgent window = %+v, want {0,1,0}", w)
	}
}

func TestDefaultBatchWindowActionNeeded(t *testing.T) {
	w := DefaultBatchWindow(SeverityActionNeeded)
	if w.Idle != 30*time.Second || w.MaxCount != 10 || w.MaxWait != 5*time.Minute {
		t.Errorf("action-needed window = %+v, want {30s,10,5min}", w)
	}
}

func TestDefaultBatchWindowInfoImmediate(t *testing.T) {
	w := DefaultBatchWindow(SeverityInfoImmediate)
	if w.Idle != 0 || w.MaxCount != 1 || w.MaxWait != 0 {
		t.Errorf("info-immediate window = %+v, want {0,1,0}", w)
	}
}

func TestDefaultBatchWindowInfoDigest(t *testing.T) {
	w := DefaultBatchWindow(SeverityInfoDigest)
	if w.Idle != 5*time.Minute || w.MaxCount != 50 || w.MaxWait != 30*time.Minute {
		t.Errorf("info-digest window = %+v, want {5min,50,30min}", w)
	}
}

func TestBatcherUrgentEmitsImmediately(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	b := NewBatcher(SeverityUrgent)
	b.Add(mkBatchNotification(t, SeverityUrgent, "x.y", now))

	out := b.ReadyToEmit(now)
	if len(out) != 1 {
		t.Errorf("urgent: emitted len = %d, want 1", len(out))
	}

	out2 := b.ReadyToEmit(now.Add(time.Hour))
	if len(out2) != 0 {
		t.Errorf("urgent post-drain: len = %d, want 0", len(out2))
	}
}

func TestBatcherActionNeededIdleTrigger(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	b := NewBatcher(SeverityActionNeeded)

	b.Add(mkBatchNotification(t, SeverityActionNeeded, "gate.failed", now))

	out := b.ReadyToEmit(now.Add(29 * time.Second))
	if len(out) != 0 {
		t.Errorf("action-needed at 29s: len = %d, want 0", len(out))
	}

	out = b.ReadyToEmit(now.Add(30 * time.Second))
	if len(out) != 1 {
		t.Errorf("action-needed at 30s idle: len = %d, want 1", len(out))
	}
}

func TestBatcherActionNeededCountTrigger(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	b := NewBatcher(SeverityActionNeeded)

	for i := 0; i < 9; i++ {
		b.Add(mkBatchNotification(t, SeverityActionNeeded, "gate.failed", now.Add(time.Duration(i)*time.Second)))
	}
	out := b.ReadyToEmit(now.Add(9 * time.Second))
	if len(out) != 0 {
		t.Errorf("action-needed count=9: len = %d, want 0", len(out))
	}

	b.Add(mkBatchNotification(t, SeverityActionNeeded, "gate.failed", now.Add(10*time.Second)))
	out = b.ReadyToEmit(now.Add(10 * time.Second))
	if len(out) != 10 {
		t.Errorf("action-needed count=10: len = %d, want 10", len(out))
	}
}

func TestBatcherActionNeededMaxWaitTrigger(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	w := BatchWindow{
		Severity: SeverityActionNeeded,
		Idle:     30 * time.Second,
		MaxCount: 999,
		MaxWait:  5 * time.Minute,
	}
	b := NewBatcherFromWindow(w)

	for off := time.Duration(0); off < 4*time.Minute+59*time.Second; off += 25 * time.Second {
		b.Add(mkBatchNotification(t, SeverityActionNeeded, "gate.failed", now.Add(off)))
	}
	out := b.ReadyToEmit(now.Add(4*time.Minute + 59*time.Second))
	if len(out) != 0 {
		t.Errorf("action-needed maxwait=4m59s: len = %d, want 0", len(out))
	}

	b.Add(mkBatchNotification(t, SeverityActionNeeded, "gate.failed", now.Add(5*time.Minute+1*time.Second)))
	out = b.ReadyToEmit(now.Add(5*time.Minute + 1*time.Second))
	if len(out) == 0 {
		t.Errorf("action-needed at maxwait reached: len = 0, want non-zero")
	}
}

func TestBatcherInfoDigestIdle5Min(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	b := NewBatcher(SeverityInfoDigest)

	b.Add(mkBatchNotification(t, SeverityInfoDigest, "milestone", now))

	out := b.ReadyToEmit(now.Add(4*time.Minute + 59*time.Second))
	if len(out) != 0 {
		t.Errorf("info-digest at 4m59s: len = %d, want 0", len(out))
	}

	out = b.ReadyToEmit(now.Add(5 * time.Minute))
	if len(out) != 1 {
		t.Errorf("info-digest at 5min idle: len = %d, want 1", len(out))
	}
}

func TestBatcherEmptyNeverEmits(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	b := NewBatcher(SeverityUrgent)
	out := b.ReadyToEmit(now)
	if len(out) != 0 {
		t.Errorf("empty batcher: len = %d, want 0", len(out))
	}
}

func TestBatcherDrainsOnEmit(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	b := NewBatcher(SeverityActionNeeded)

	for i := 0; i < 10; i++ {
		b.Add(mkBatchNotification(t, SeverityActionNeeded, "x.y", now.Add(time.Duration(i)*time.Second)))
	}
	out1 := b.ReadyToEmit(now.Add(11 * time.Second))
	if len(out1) != 10 {
		t.Errorf("first emit: len = %d, want 10", len(out1))
	}

	out2 := b.ReadyToEmit(now.Add(20 * time.Second))
	if len(out2) != 0 {
		t.Errorf("post-drain: len = %d, want 0", len(out2))
	}
}

func TestManagerRoutesByProjectAndSeverity(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m := NewBatchManager()

	pidA := "a" + strings.Repeat("0", 63)
	pidB := "b" + strings.Repeat("1", 63)

	nA := Notification{
		ProjectID:   pidA,
		Severity:    SeverityActionNeeded,
		EventType:   "gate.failed",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now,
	}
	nB := Notification{
		ProjectID:   pidB,
		Severity:    SeverityActionNeeded,
		EventType:   "gate.failed",
		ContentHash: strings.Repeat("b", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now,
	}
	m.Add(nA)
	m.Add(nB)

	out := m.Tick(now.Add(31 * time.Second))
	if len(out) != 2 {
		t.Errorf("manager Tick: total len = %d, want 2 (one per project)", len(out))
	}
}

func TestNewBatcherFromWindowUsesProvidedPolicy(t *testing.T) {

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	w := BatchWindow{
		Severity: SeverityActionNeeded,
		Idle:     1 * time.Minute,
		MaxCount: 3,
		MaxWait:  10 * time.Minute,
	}
	b := NewBatcherFromWindow(w)

	for i := 0; i < 2; i++ {
		b.Add(mkBatchNotification(t, SeverityActionNeeded, "x.y", now.Add(time.Duration(i)*time.Second)))
	}
	if out := b.ReadyToEmit(now.Add(2 * time.Second)); len(out) != 0 {
		t.Errorf("custom window count=2: len = %d, want 0", len(out))
	}

	b.Add(mkBatchNotification(t, SeverityActionNeeded, "x.y", now.Add(3*time.Second)))
	if out := b.ReadyToEmit(now.Add(3 * time.Second)); len(out) != 3 {
		t.Errorf("custom window count=3: len = %d, want 3", len(out))
	}
}

func TestManagerPendingTracksAccumulation(t *testing.T) {

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m := NewBatchManager()

	if got := m.Pending(); got != 0 {
		t.Errorf("empty manager Pending() = %d, want 0", got)
	}

	pid := "a" + strings.Repeat("0", 63)
	for i := 0; i < 3; i++ {
		m.Add(Notification{
			ProjectID:   pid,
			Severity:    SeverityActionNeeded,
			EventType:   "gate.failed",
			ContentHash: strings.Repeat("a", 64),
			Payload:     json.RawMessage(`{}`),
			CreatedAt:   now.Add(time.Duration(i) * time.Second),
		})
	}
	if got := m.Pending(); got != 3 {
		t.Errorf("after 3 Adds Pending() = %d, want 3", got)
	}

	out := m.Tick(now.Add(33 * time.Second))
	if len(out) != 3 {
		t.Fatalf("expected 3 emitted, got %d", len(out))
	}
	if got := m.Pending(); got != 0 {
		t.Errorf("after emit Pending() = %d, want 0", got)
	}
}

func TestManagerSeparatesUrgentFromActionNeeded(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	m := NewBatchManager()

	pid := "a" + strings.Repeat("0", 63)
	urgent := Notification{
		ProjectID: pid, Severity: SeverityUrgent,
		EventType: "panic", ContentHash: strings.Repeat("u", 64),
		Payload: json.RawMessage(`{}`), CreatedAt: now,
	}
	action := Notification{
		ProjectID: pid, Severity: SeverityActionNeeded,
		EventType: "gate", ContentHash: strings.Repeat("a", 64),
		Payload: json.RawMessage(`{}`), CreatedAt: now,
	}
	m.Add(urgent)
	m.Add(action)

	out := m.Tick(now)
	if len(out) != 1 {
		t.Fatalf("urgent should emit immediately, action should hold: len = %d, want 1", len(out))
	}
	if out[0].Severity != SeverityUrgent {
		t.Errorf("emitted severity = %q, want urgent", out[0].Severity)
	}
}
