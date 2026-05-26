package tmuxlife

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestIdleDepsZeroValueIsBusy(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678",
		Status:       StatusActive,
		CreatedAt:    time.Now().Add(-30 * time.Hour),
		LastAttachAt: time.Now().Add(-30 * time.Hour),
	}
	deps := IdleDeps{}
	if !r.IsIdle(s, deps) {
		t.Errorf("30h-old session with zero deps should be idle (default doctrine, 24h TTL)")
	}
}

func TestIsIdleMaxScopeNeverReaped(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameMaxScope })
	s := &Session{
		Alias: "internal-platform-x", Sha8: "a3f5b2c8", Name: "zen-internal-platform-x-a3f5b2c8",
		Status:       StatusActive,
		LastAttachAt: time.Now().Add(-365 * 24 * time.Hour),
	}
	deps := IdleDeps{
		HasOperatorAttach:   false,
		HasAutonomousWorker: false,
		HasScheduledJob:     false,
		LastAttachAt:        time.Now().Add(-365 * 24 * time.Hour),
	}
	if r.IsIdle(s, deps) {
		t.Errorf("max-scope session should NEVER be idle; inv-zen-119 violated")
	}
}

func TestIsIdleDefault24h(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		HasOperatorAttach:   false,
		HasAutonomousWorker: false,
		HasScheduledJob:     false,
		LastAttachAt:        time.Now().Add(-25 * time.Hour),
	}
	if !r.IsIdle(s, deps) {
		t.Errorf("25h idle (default 24h) should be idle")
	}
}

func TestIsIdleDefaultStillFresh(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		HasOperatorAttach:   false,
		HasAutonomousWorker: false,
		HasScheduledJob:     false,
		LastAttachAt:        time.Now().Add(-23 * time.Hour),
	}
	if r.IsIdle(s, deps) {
		t.Errorf("23h idle (default 24h) should NOT be idle")
	}
}

func TestIsIdleCapaFirewall4h(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameCapaFirewall })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		HasOperatorAttach:   false,
		HasAutonomousWorker: false,
		HasScheduledJob:     false,
		LastAttachAt:        time.Now().Add(-5 * time.Hour),
	}
	if !r.IsIdle(s, deps) {
		t.Errorf("5h idle on capa-firewall (4h TTL) should be idle")
	}
}

func TestIsIdleCapaFirewallStillFresh(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameCapaFirewall })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		LastAttachAt: time.Now().Add(-3 * time.Hour),
	}
	if r.IsIdle(s, deps) {
		t.Errorf("3h idle on capa-firewall (4h TTL) should NOT be idle")
	}
}

// TestIsIdleOperatorAttached any of the 3 activity signals MUST veto idle.
func TestIsIdleOperatorAttached(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		HasOperatorAttach:   true,
		HasAutonomousWorker: false,
		HasScheduledJob:     false,
		LastAttachAt:        time.Now().Add(-100 * time.Hour),
	}
	if r.IsIdle(s, deps) {
		t.Errorf("operator-attached session should NOT be idle regardless of LastAttachAt")
	}
}

func TestIsIdleAutonomousWorkerActive(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		HasAutonomousWorker: true,
		LastAttachAt:        time.Now().Add(-100 * time.Hour),
	}
	if r.IsIdle(s, deps) {
		t.Errorf("autonomous-worker active should veto idle")
	}
}

func TestIsIdleScheduledJobPending(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{
		HasScheduledJob: true,
		LastAttachAt:    time.Now().Add(-100 * time.Hour),
	}
	if r.IsIdle(s, deps) {
		t.Errorf("scheduled-job pending should veto idle")
	}
}

func TestIsIdleStatusNotActiveSkipped(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	for _, st := range []SessionStatus{StatusIdle, StatusArchived, StatusOrphaned} {
		s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: st}
		deps := IdleDeps{LastAttachAt: time.Now().Add(-100 * time.Hour)}
		if r.IsIdle(s, deps) {
			t.Errorf("status %v: IsIdle returned true; only Active is a reap candidate", st)
		}
	}
}

func TestIsIdleNilSessionRejected(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	if r.IsIdle(nil, IdleDeps{}) {
		t.Errorf("IsIdle(nil) returned true; want false")
	}
}

func TestIsIdleMaxScopeWithFreshAttach(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameMaxScope })
	s := &Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive}
	deps := IdleDeps{LastAttachAt: time.Now().Add(-1 * time.Minute)}
	if r.IsIdle(s, deps) {
		t.Errorf("max-scope: never idle even immediately after attach")
	}
}

func TestIsIdleEffectiveTimestampFallback(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })

	s := &Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678",
		Status:    StatusActive,
		CreatedAt: time.Now().Add(-30 * time.Hour),
	}
	if !r.IsIdle(s, IdleDeps{}) {
		t.Errorf("CreatedAt fallback not honoured; expected idle")
	}
}

func TestIsIdleAllTimestampsZeroFailsafe(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	s := &Session{
		Alias: "x", Sha8: "12345678", Name: "zen-x-12345678",
		Status: StatusActive,
	}
	if r.IsIdle(s, IdleDeps{}) {
		t.Errorf("all-zero timestamps: expected failsafe false; got idle")
	}
}

func TestNewIdleReaperPanicsOnNilManager(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewIdleReaper(nil, fn) did not panic")
		}
	}()
	_ = NewIdleReaper(nil, func(string) doctrine.Name { return doctrine.NameDefault })
}

func TestNewIdleReaperPanicsOnNilDoctrineFor(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewIdleReaper(m, nil) did not panic")
		}
	}()
	_ = NewIdleReaper(New(newFakeSessionStore()), nil)
}

func TestNewIdleReaperDefaults(t *testing.T) {
	m := New(newFakeSessionStore())
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	if r.interval != DefaultIdleReapInterval {
		t.Errorf("interval = %v, want DefaultIdleReapInterval (%v)", r.interval, DefaultIdleReapInterval)
	}
	if r.depsFor == nil {
		t.Errorf("depsFor not wired (should default to LastAttachAt-only)")
	}
	if r.logger == nil {
		t.Errorf("logger not wired (should default to log.Default())")
	}

	now := time.Now()
	got := r.depsFor(Session{LastAttachAt: now})
	if !got.LastAttachAt.Equal(now) {
		t.Errorf("default depsFor LastAttachAt = %v, want %v", got.LastAttachAt, now)
	}
	if got.HasOperatorAttach || got.HasAutonomousWorker || got.HasScheduledJob {
		t.Errorf("default depsFor leaked activity signals: %+v", got)
	}
}

func TestIdleReaperTickReapsOnlyEligible(t *testing.T) {
	st := newFakeSessionStore()
	stale := Session{
		Alias: "stale", Sha8: "11111111", Name: "zen-stale-11111111",
		Status:       StatusActive,
		CreatedAt:    time.Now().Add(-50 * time.Hour),
		LastAttachAt: time.Now().Add(-50 * time.Hour),
	}
	fresh := Session{
		Alias: "fresh", Sha8: "22222222", Name: "zen-fresh-22222222",
		Status:       StatusActive,
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		LastAttachAt: time.Now().Add(-1 * time.Hour),
	}
	maxs := Session{
		Alias: "max", Sha8: "33333333", Name: "zen-max-33333333",
		Status:       StatusActive,
		CreatedAt:    time.Now().Add(-1000 * time.Hour),
		LastAttachAt: time.Now().Add(-1000 * time.Hour),
	}
	if err := st.UpsertSession(stale); err != nil {
		t.Fatalf("UpsertSession stale: %v", err)
	}
	if err := st.UpsertSession(fresh); err != nil {
		t.Fatalf("UpsertSession fresh: %v", err)
	}
	if err := st.UpsertSession(maxs); err != nil {
		t.Fatalf("UpsertSession maxs: %v", err)
	}

	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec

	doctrineFor := func(alias string) doctrine.Name {
		switch alias {
		case "max":
			return doctrine.NameMaxScope
		default:
			return doctrine.NameDefault
		}
	}
	depsFor := func(s Session) IdleDeps {
		return IdleDeps{LastAttachAt: s.LastAttachAt}
	}

	r := NewIdleReaper(m, doctrineFor)
	r.depsFor = depsFor

	var sink testLogSink
	r.logger = sink.logger()

	if err := r.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	gotStale, _ := st.GetSession("zen-stale-11111111")
	if gotStale.Status != StatusActive {
		t.Errorf("stale: Status = %v, want Active (pre-C-10 Save placeholder aborts Teardown)", gotStale.Status)
	}
	if !sink.contains("zen-stale-11111111") {
		t.Errorf("logger missing stale Teardown attempt; output = %q", sink.string())
	}

	gotFresh, _ := st.GetSession("zen-fresh-22222222")
	if gotFresh.Status != StatusActive {
		t.Errorf("fresh: Status = %v, want Active", gotFresh.Status)
	}
	if sink.contains("zen-fresh-22222222") {
		t.Errorf("logger leaked fresh teardown attempt; output = %q", sink.string())
	}

	gotMax, _ := st.GetSession("zen-max-33333333")
	if gotMax.Status != StatusActive {
		t.Errorf("max-scope: Status = %v, want Active (never reap)", gotMax.Status)
	}
	if sink.contains("zen-max-33333333") {
		t.Errorf("logger leaked max-scope teardown attempt; output = %q", sink.string())
	}
}

func TestIdleReaperTickListSessionsError(t *testing.T) {
	failing := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("disk full"),
	}
	m := New(failing)
	m.exec = (&fakeExecutor{}).Exec

	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })

	err := r.tick(context.Background())
	if err == nil {
		t.Fatalf("tick: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ListSessions") {
		t.Errorf("err = %v; expected wrap referencing ListSessions", err)
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v; expected wrap of underlying %q", err, "disk full")
	}
}

func TestIdleReaperRunCycleStops(t *testing.T) {
	st := newFakeSessionStore()
	m := New(st)
	m.exec = (&fakeExecutor{}).Exec
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	r.interval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:

	case <-time.After(time.Second):
		t.Fatalf("Run did not return after ctx cancel")
	}
}

func TestIdleReaperRunLogsTickError(t *testing.T) {
	failing := &failingStore{
		fakeSessionStore: newFakeSessionStore(),
		listErr:          errors.New("disk full"),
	}
	m := New(failing)
	m.exec = (&fakeExecutor{}).Exec

	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	r.interval = 5 * time.Millisecond

	var sink testLogSink
	r.logger = sink.logger()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Run did not return after ctx cancel")
	}
	if !sink.contains("disk full") {
		t.Errorf("logger did not capture tick error; output = %q", sink.string())
	}
}

func TestIdleReaperTickSkipsTeardownErrors(t *testing.T) {
	st := newFakeSessionStore()
	a := Session{Alias: "a", Sha8: "aaaaaaaa", Name: "zen-a-aaaaaaaa", Status: StatusActive, LastAttachAt: time.Now().Add(-50 * time.Hour)}
	b := Session{Alias: "b", Sha8: "bbbbbbbb", Name: "zen-b-bbbbbbbb", Status: StatusActive, LastAttachAt: time.Now().Add(-50 * time.Hour)}
	if err := st.UpsertSession(a); err != nil {
		t.Fatalf("UpsertSession a: %v", err)
	}
	if err := st.UpsertSession(b); err != nil {
		t.Fatalf("UpsertSession b: %v", err)
	}

	exec := &fakeExecutor{}

	m := New(st)
	m.exec = exec.Exec

	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	r.depsFor = func(s Session) IdleDeps { return IdleDeps{LastAttachAt: s.LastAttachAt} }

	var sink testLogSink
	r.logger = sink.logger()

	if err := r.tick(context.Background()); err != nil {
		t.Errorf("tick returned non-nil despite per-session failures being non-fatal: %v", err)
	}

	if !sink.contains("zen-a-aaaaaaaa") || !sink.contains("zen-b-bbbbbbbb") {
		t.Errorf("logger missing one of the expected sessions; output = %q", sink.string())
	}
}

func TestIdleReaperTickSnapshotFailureWrapped(t *testing.T) {
	st := newFakeSessionStore()
	s := Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive, LastAttachAt: time.Now().Add(-50 * time.Hour)}
	if err := st.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec

	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	r.depsFor = func(s Session) IdleDeps { return IdleDeps{LastAttachAt: s.LastAttachAt} }

	var sink testLogSink
	r.logger = sink.logger()

	if err := r.tick(context.Background()); err != nil {
		t.Errorf("tick returned non-nil despite recoverable per-session failure: %v", err)
	}
	if !sink.contains("snapshot") {
		t.Errorf("logger did not capture snapshot wrap; output = %q", sink.string())
	}
}

func TestIdleReaperRaceProtection(t *testing.T) {
	st := newFakeSessionStore()
	s := Session{Alias: "x", Sha8: "12345678", Name: "zen-x-12345678", Status: StatusActive, LastAttachAt: time.Now().Add(-50 * time.Hour)}
	if err := st.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	exec := &fakeExecutor{}
	m := New(st)
	m.exec = exec.Exec

	var calls int32
	r := NewIdleReaper(m, func(string) doctrine.Name { return doctrine.NameDefault })
	r.depsFor = func(s Session) IdleDeps {

		n := atomic.AddInt32(&calls, 1)
		return IdleDeps{
			HasOperatorAttach: n >= 2,
			LastAttachAt:      s.LastAttachAt,
		}
	}

	if err := r.tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	got, _ := st.GetSession("zen-x-12345678")
	if got.Status != StatusActive {
		t.Errorf("Status = %v, want Active (race protection)", got.Status)
	}

	for _, c := range exec.calls {
		if strings.HasPrefix(c, "kill-session-") {
			t.Errorf("unexpected kill-session call: %v", exec.calls)
		}
	}
}

func TestDefaultIdleReapIntervalIs5Minutes(t *testing.T) {
	if DefaultIdleReapInterval != 5*time.Minute {
		t.Errorf("DefaultIdleReapInterval = %v, want 5m (spec §1 Q7 D)", DefaultIdleReapInterval)
	}
}
