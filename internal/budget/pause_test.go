package budget

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakePauseStore struct {
	mu                 sync.Mutex
	rows               map[string]PauseRow
	errOn              string
	listErr            bool
	clearErr           bool
	getErr             bool
	clearIfExpErr      bool
	afterListActive    func()
	afterListActiveErr func()
}

func newFakePauseStore() *fakePauseStore {
	return &fakePauseStore{rows: map[string]PauseRow{}}
}

func (f *fakePauseStore) InsertCostAxisTag(context.Context, int64, string, string) error {
	return nil
}
func (f *fakePauseStore) EmitAxisTagLoss(context.Context, int64, string) error { return nil }
func (f *fakePauseStore) QueryAxisTags(context.Context, int64) (map[string]string, error) {
	return nil, nil
}
func (f *fakePauseStore) QueryCostIDsByAxis(context.Context, string, string) ([]int64, error) {
	return nil, nil
}
func (f *fakePauseStore) QueryAxisTagLosses(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (f *fakePauseStore) PauseGet(_ context.Context, scope, val string) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr {
		return false, 0, errors.New("injected pause-get error")
	}
	r, ok := f.rows[scope+":"+val]
	if !ok {
		return false, 0, nil
	}
	return true, r.AutoResumeAtMs, nil
}
func (f *fakePauseStore) PauseSet(_ context.Context, scope, val, reason string, startedAtMs, autoResumeAt int64) error {
	if f.errOn == "set" {
		return errors.New("injected set error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	if startedAtMs == 0 {
		startedAtMs = time.Now().UnixMilli()
	}
	f.rows[scope+":"+val] = PauseRow{
		Scope:          scope,
		ScopeValue:     val,
		Reason:         reason,
		StartedAtMs:    startedAtMs,
		AutoResumeAtMs: autoResumeAt,
	}
	return nil
}
func (f *fakePauseStore) PauseClear(_ context.Context, scope, val string) error {
	if f.clearErr {
		return errors.New("injected clear error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, scope+":"+val)
	return nil
}
func (f *fakePauseStore) PauseListActive(_ context.Context) ([]PauseRow, error) {
	f.mu.Lock()
	if f.listErr {
		errHook := f.afterListActiveErr
		f.mu.Unlock()
		if errHook != nil {
			errHook()
		}
		return nil, errors.New("injected list error")
	}
	out := make([]PauseRow, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	f.mu.Unlock()

	if f.afterListActive != nil {
		f.afterListActive()
	}
	return out, nil
}

func (f *fakePauseStore) PauseClearIfExpired(_ context.Context, scope, val string, beforeMs int64) error {
	if f.clearIfExpErr {
		return errors.New("injected clear-if-expired error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[scope+":"+val]
	if !ok {
		return nil
	}
	if r.AutoResumeAtMs > 0 && r.AutoResumeAtMs <= beforeMs {
		delete(f.rows, scope+":"+val)
	}
	return nil
}
func (f *fakePauseStore) AnomalyAppend(context.Context, AnomalyRow) error { return nil }
func (f *fakePauseStore) AnomalyWindow(context.Context, string, string, int) ([]float64, error) {
	return nil, nil
}
func (f *fakePauseStore) RolledUSDByAxis(context.Context, string, string, int64) (float64, error) {
	return 0, nil
}

func TestPauserTriggerActivatesPause(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	if err := p.Trigger(context.Background(), "worker_id", "w-42", "manual", time.Hour); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	paused, err := p.IsPaused(context.Background(), "worker_id", "w-42")
	if err != nil {
		t.Fatalf("IsPaused: %v", err)
	}
	if !paused {
		t.Errorf("paused = false, want true")
	}
}

func TestPauserResumeClearsPause(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = p.Trigger(context.Background(), "stage", "design", "manual", 0)
	if err := p.Resume(context.Background(), "stage", "design"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	paused, _ := p.IsPaused(context.Background(), "stage", "design")
	if paused {
		t.Errorf("paused = true after Resume, want false")
	}
}

func TestPauserTriggerRejectsUnknownScope(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	err := p.Trigger(context.Background(), "unknown_scope", "x", "r", 0)
	if !errors.Is(err, ErrUnknownPauseScope) {
		t.Errorf("err = %v, want ErrUnknownPauseScope", err)
	}
}

func TestPauserResumeRejectsUnknownScope(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	err := p.Resume(context.Background(), "unknown_scope", "x")
	if !errors.Is(err, ErrUnknownPauseScope) {
		t.Errorf("err = %v, want ErrUnknownPauseScope", err)
	}
}

func TestPauserTriggerRejectsEmptyScopeValue(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	err := p.Trigger(context.Background(), "stage", "", "r", 0)
	if err == nil {
		t.Error("err = nil, want error on empty scope_value")
	}
}

func TestPauserTriggerEmptyReasonBecomesUnspecified(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = p.Trigger(context.Background(), "stage", "design", "", 0)
	rows, _ := store.PauseListActive(context.Background())
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].Reason != "unspecified" {
		t.Errorf("reason = %q, want unspecified", rows[0].Reason)
	}
}

func TestPauserTriggerIndefiniteWhenZeroDuration(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = p.Trigger(context.Background(), "project", "internal-platform-x", "manual", 0)
	paused, _ := p.IsPaused(context.Background(), "project", "internal-platform-x")
	if !paused {
		t.Errorf("paused = false, want true")
	}
	rows, _ := store.PauseListActive(context.Background())
	if rows[0].AutoResumeAtMs != 0 {
		t.Errorf("AutoResumeAtMs = %d, want 0 (indefinite)", rows[0].AutoResumeAtMs)
	}
}

func TestPauserTriggerNegativeDurationIsIndefinite(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = p.Trigger(context.Background(), "stage", "design", "r", -time.Hour)
	rows, _ := store.PauseListActive(context.Background())
	if rows[0].AutoResumeAtMs != 0 {
		t.Errorf("AutoResumeAtMs = %d, want 0 (negative duration → indefinite)", rows[0].AutoResumeAtMs)
	}
}

func TestPauserListActive(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = p.Trigger(context.Background(), "stage", "design", "r1", 0)
	_ = p.Trigger(context.Background(), "worker_id", "w-1", "r2", 0)
	rows, err := p.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("len = %d, want 2", len(rows))
	}
}

func TestPauserListActiveErrorPropagated(t *testing.T) {
	store := newFakePauseStore()
	store.listErr = true
	p := NewPauser(store)
	_, err := p.ListActive(context.Background())
	if err == nil {
		t.Error("err = nil, want injected list error")
	}
}

func TestSchedulerAutoResumesExpired(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)

	_ = store.PauseSet(context.Background(), "stage", "design", "expired", 0, time.Now().Add(-time.Hour).UnixMilli())
	if err := p.RunSchedulerOnce(context.Background()); err != nil {
		t.Fatalf("RunSchedulerOnce: %v", err)
	}
	paused, _ := p.IsPaused(context.Background(), "stage", "design")
	if paused {
		t.Errorf("paused = true after auto-resume, want false")
	}
}

func TestSchedulerKeepsActivePauses(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = store.PauseSet(context.Background(), "stage", "design", "active", 0, time.Now().Add(time.Hour).UnixMilli())
	_ = p.RunSchedulerOnce(context.Background())
	paused, _ := p.IsPaused(context.Background(), "stage", "design")
	if !paused {
		t.Errorf("paused = false, want true (still active)")
	}
}

func TestSchedulerKeepsIndefinitePauses(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	_ = p.Trigger(context.Background(), "project", "internal-platform-x", "indefinite", 0)
	_ = p.RunSchedulerOnce(context.Background())
	paused, _ := p.IsPaused(context.Background(), "project", "internal-platform-x")
	if !paused {
		t.Errorf("paused = false, want true (indefinite)")
	}
}

func TestRunSchedulerOnceListErrorPropagated(t *testing.T) {
	store := newFakePauseStore()
	store.listErr = true
	p := NewPauser(store)
	if err := p.RunSchedulerOnce(context.Background()); err == nil {
		t.Error("err = nil, want injected list error")
	}
}

func TestRunSchedulerOnceClearErrorPropagated(t *testing.T) {
	store := newFakePauseStore()
	_ = store.PauseSet(context.Background(), "stage", "design", "expired", 0, time.Now().Add(-time.Hour).UnixMilli())

	store.clearIfExpErr = true
	p := NewPauser(store)
	if err := p.RunSchedulerOnce(context.Background()); err == nil {
		t.Error("err = nil, want injected clear-if-expired error")
	}
}

func TestStartSchedulerStopsOnContextCancel(t *testing.T) {
	store := newFakePauseStore()
	tickObserved := make(chan struct{}, 1)
	store.afterListActive = func() {
		select {
		case tickObserved <- struct{}{}:
		default:
		}
	}
	p := NewPauser(store)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.StartScheduler(ctx, time.Millisecond)
		close(done)
	}()

	select {
	case <-tickObserved:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("scheduler did not run a single tick within 2s")
	}
	cancel()
	select {
	case <-done:

	case <-time.After(2 * time.Second):
		t.Error("scheduler did not exit within 2s after ctx cancel")
	}
}

func TestStartSchedulerSwallowsTickErrors(t *testing.T) {
	store := newFakePauseStore()
	store.listErr = true
	tickObserved := make(chan struct{}, 1)
	store.afterListActiveErr = func() {
		select {
		case tickObserved <- struct{}{}:
		default:
		}
	}
	p := NewPauser(store)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.StartScheduler(ctx, time.Millisecond)
		close(done)
	}()

	select {
	case <-tickObserved:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("scheduler did not run an error-y tick within 2s")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("scheduler did not exit; errors should be swallowed")
	}
}

func TestStartSchedulerRejectsZeroCadence(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	if err := p.StartScheduler(context.Background(), 0); err == nil {
		t.Error("err = nil, want error on cadence=0")
	}
}

func TestStartSchedulerRejectsNegativeCadence(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	if err := p.StartScheduler(context.Background(), -time.Second); err == nil {
		t.Error("err = nil, want error on cadence<0")
	}
}

func TestPauseSetErrorPropagated(t *testing.T) {
	store := newFakePauseStore()
	store.errOn = "set"
	p := NewPauser(store)
	err := p.Trigger(context.Background(), "stage", "design", "r", 0)
	if err == nil {
		t.Error("err = nil, want injected set error wrap")
	}
}

func TestPauseClearErrorPropagated(t *testing.T) {
	store := newFakePauseStore()
	store.clearErr = true
	p := NewPauser(store)
	err := p.Resume(context.Background(), "stage", "design")
	if err == nil {
		t.Error("err = nil, want injected clear error")
	}
}

func TestIsPausedGetErrorPropagated(t *testing.T) {
	store := newFakePauseStore()
	store.getErr = true
	p := NewPauser(store)
	_, err := p.IsPaused(context.Background(), "stage", "design")
	if err == nil {
		t.Error("err = nil, want injected get error")
	}
}

func TestIsPausedRowExistsAutoResumeInPast(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)

	_ = store.PauseSet(context.Background(), "stage", "design", "expired",
		0, time.Now().Add(-time.Hour).UnixMilli())
	paused, err := p.IsPaused(context.Background(), "stage", "design")
	if err != nil {
		t.Fatalf("IsPaused: %v", err)
	}
	if paused {
		t.Error("paused = true, want false (auto_resume_at < now)")
	}
}

func TestValidPauseScopesReturns4(t *testing.T) {
	got := ValidPauseScopes()
	if len(got) != 4 {
		t.Errorf("len = %d, want 4", len(got))
	}
	want := map[string]bool{"project": true, "doctrine": true, "stage": true, "worker_id": true}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected scope %q", s)
		}
	}
}

func TestNewPauserNilStorePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewPauser(nil)
}

func TestSetClock(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.SetClock(func() time.Time { return fixed })

	store := p.store.(*fakePauseStore)
	_ = p.Trigger(context.Background(), "stage", "design", "r", time.Hour)
	rows, _ := store.PauseListActive(context.Background())
	wantMs := fixed.Add(time.Hour).UnixMilli()
	if rows[0].AutoResumeAtMs != wantMs {
		t.Errorf("AutoResumeAtMs = %d, want %d (clock-injected)", rows[0].AutoResumeAtMs, wantMs)
	}
}

func TestSetClockStartedAtAndAutoResumeShareClockI2(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	p.SetClock(func() time.Time { return fixed })

	if err := p.Trigger(context.Background(), "stage", "design", "r", time.Hour); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	store := p.store.(*fakePauseStore)
	rows, _ := store.PauseListActive(context.Background())
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	wantStarted := fixed.UnixMilli()
	wantResume := fixed.Add(time.Hour).UnixMilli()
	if rows[0].StartedAtMs != wantStarted {
		t.Errorf("StartedAtMs = %d, want %d (clock-injected; not wall-clock)", rows[0].StartedAtMs, wantStarted)
	}
	if rows[0].AutoResumeAtMs != wantResume {
		t.Errorf("AutoResumeAtMs = %d, want %d", rows[0].AutoResumeAtMs, wantResume)
	}

	if rows[0].AutoResumeAtMs-rows[0].StartedAtMs != int64(time.Hour/time.Millisecond) {
		t.Errorf("delta = %d ms, want 3600000 ms (clock consistency)", rows[0].AutoResumeAtMs-rows[0].StartedAtMs)
	}
}

func TestSetClockNilPanics(t *testing.T) {
	p := NewPauser(newFakePauseStore())
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	p.SetClock(nil)
}

// TestRunSchedulerPreservesConcurrentExtension is the C-1 regression test
// (post-review fix): the scheduler MUST NOT erase a pause whose
// auto_resume_at was extended after the scheduler's snapshot read.
//
// Pre-fix bug: RunSchedulerOnce reads pauses at T0, then calls
// PauseClear(scope, value) unconditionally without re-checking
// auto_resume_at. A concurrent Trigger that pushes auto_resume_at into
// the future between snapshot and delete is silently erased — the
// pause vanishes despite a fresh extension.
//
// Post-fix contract: the scheduler routes through the new
// PauseClearIfExpired(scope, value, beforeMs) method which deletes
// only when auto_resume_at <= beforeMs. Any concurrent extension that
// pushes auto_resume_at past beforeMs leaves the row intact (the SQL
// WHERE clause is the CAS).
//
// Test reproduces the race deterministically via an injected hook on
// the fake store: between PauseListActive (snapshot) and the per-row
// expire decision, the hook extends the pause. With the unconditional
// PauseClear (pre-fix), the extension is erased. With
// PauseClearIfExpired (post-fix), the extension survives.
func TestRunSchedulerPreservesConcurrentExtension(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.SetClock(func() time.Time { return t0 })

	if err := p.Trigger(context.Background(), "stage", "design", "short", time.Millisecond); err != nil {
		t.Fatalf("Trigger short: %v", err)
	}

	p.SetClock(func() time.Time { return t0.Add(10 * time.Millisecond) })

	store.afterListActive = func() {

		store.afterListActive = nil
		_ = p.Trigger(context.Background(), "stage", "design", "extended", time.Hour)
	}

	if err := p.RunSchedulerOnce(context.Background()); err != nil {
		t.Fatalf("RunSchedulerOnce: %v", err)
	}

	// Pause MUST still be active (extension preserved).
	paused, err := p.IsPaused(context.Background(), "stage", "design")
	if err != nil {
		t.Fatalf("IsPaused: %v", err)
	}
	if !paused {
		t.Error("paused = false after scheduler; want true (extension preserved by CAS)")
	}

	rows, _ := store.PauseListActive(context.Background())
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	wantMs := t0.Add(10*time.Millisecond + time.Hour).UnixMilli()
	if rows[0].AutoResumeAtMs != wantMs {
		t.Errorf("AutoResumeAtMs = %d, want %d (extension preserved)", rows[0].AutoResumeAtMs, wantMs)
	}
}

func TestRunSchedulerStillExpiresExpiredRows(t *testing.T) {
	store := newFakePauseStore()
	p := NewPauser(store)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.SetClock(func() time.Time { return t0 })

	if err := p.Trigger(context.Background(), "stage", "design", "short", time.Millisecond); err != nil {
		t.Fatalf("Trigger: %v", err)
	}

	p.SetClock(func() time.Time { return t0.Add(time.Hour) })

	if err := p.RunSchedulerOnce(context.Background()); err != nil {
		t.Fatalf("RunSchedulerOnce: %v", err)
	}

	paused, _ := p.IsPaused(context.Background(), "stage", "design")
	if paused {
		t.Error("paused = true after expiry+scheduler, want false (CAS still expires)")
	}

	rows, _ := store.PauseListActive(context.Background())
	if len(rows) != 0 {
		t.Errorf("len(rows) = %d, want 0 (row cleared)", len(rows))
	}
}
