package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

type fakeQuota struct {
	allow  bool
	reason string
	err    error
	calls  int32
	mu     sync.Mutex
}

func (f *fakeQuota) PreFlight(_ context.Context, _ string, _ doctrine.Name) (quota.PreFlightDecision, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return quota.PreFlightDecision{}, f.err
	}
	if f.allow {
		return quota.PreFlightDecision{Allowed: true}, nil
	}
	return quota.PreFlightDecision{Allowed: false, Reason: f.reason}, nil
}

func (f *fakeQuota) Calls() int32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeDispatcher struct {
	mu     sync.Mutex
	calls  []scheduler.DispatchInput
	result scheduler.DispatchResult
	err    error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, in scheduler.DispatchInput) (scheduler.DispatchResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, in)
	f.mu.Unlock()
	return f.result, f.err
}

func (f *fakeDispatcher) Calls() []scheduler.DispatchInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]scheduler.DispatchInput, len(f.calls))
	copy(out, f.calls)
	return out
}

type fakeEventLog struct {
	mu     sync.Mutex
	events []scheduler.Event
}

func (f *fakeEventLog) Emit(_ context.Context, e scheduler.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeEventLog) Events() []scheduler.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]scheduler.Event, len(f.events))
	copy(out, f.events)
	return out
}

func (f *fakeEventLog) CountKind(k scheduler.EventKind) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.Kind == k {
			n++
		}
	}
	return n
}

type fakeRateLimit struct {
	mu        sync.Mutex
	allowSeq  []bool
	defaultOK bool
	calls     int
}

func (f *fakeRateLimit) Allow(_ context.Context, _ string, _ time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.allowSeq) > 0 {
		v := f.allowSeq[0]
		f.allowSeq = f.allowSeq[1:]
		return v
	}
	return f.defaultOK
}

func (f *fakeRateLimit) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeStore struct {
	mu          sync.Mutex
	history     []scheduler.HistoryEntry
	nextRunArgs []nextRunCall
	statusArgs  []statusCall
	historyErr  error
	nextRunErr  error
}

type nextRunCall struct {
	id        string
	lastRunAt time.Time
	nextRunAt time.Time
}

type statusCall struct {
	id     string
	status scheduler.Status
}

func (f *fakeStore) Insert(_ context.Context, _ *scheduler.Schedule) error { return nil }
func (f *fakeStore) Get(_ context.Context, _ string) (*scheduler.Schedule, error) {
	return nil, scheduler.ErrNotFound
}

func (f *fakeStore) UpdateNextRun(_ context.Context, id string, lr, nr time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextRunErr != nil {
		return f.nextRunErr
	}
	f.nextRunArgs = append(f.nextRunArgs, nextRunCall{id: id, lastRunAt: lr, nextRunAt: nr})
	return nil
}

func (f *fakeStore) UpdateStatus(_ context.Context, id string, st scheduler.Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusArgs = append(f.statusArgs, statusCall{id: id, status: st})
	return nil
}

func (f *fakeStore) Delete(_ context.Context, _ string) error { return nil }
func (f *fakeStore) ListDue(_ context.Context, _ time.Time) ([]*scheduler.Schedule, error) {
	return nil, nil
}

func (f *fakeStore) ListByProject(_ context.Context, _ string) ([]*scheduler.Schedule, error) {
	return nil, nil
}

func (f *fakeStore) AppendHistory(_ context.Context, h scheduler.HistoryEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.historyErr != nil {
		return f.historyErr
	}
	f.history = append(f.history, h)
	return nil
}

func (f *fakeStore) QueryHistory(_ context.Context, _ string, _, _ time.Time) ([]scheduler.HistoryEntry, error) {
	return nil, nil
}

func (f *fakeStore) History() []scheduler.HistoryEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]scheduler.HistoryEntry, len(f.history))
	copy(out, f.history)
	return out
}

func (f *fakeStore) NextRunCalls() []nextRunCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]nextRunCall, len(f.nextRunArgs))
	copy(out, f.nextRunArgs)
	return out
}

func newSchedule(t *testing.T, now time.Time) *scheduler.Schedule {
	t.Helper()
	return &scheduler.Schedule{
		ID:            "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "internal-platform-x",
		Action:        "morning-brief",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  7 * 24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-30 * time.Minute),
		NextRunAt:     now.Add(-1 * time.Minute),
		CreatedAt:     now.Add(-30 * 24 * time.Hour),
	}
}

func newDeps(now time.Time, q *fakeQuota, d *fakeDispatcher, el *fakeEventLog, rl *fakeRateLimit, st *fakeStore, dn doctrine.Name) scheduler.FireDeps {
	return scheduler.FireDeps{
		Now:        func() time.Time { return now },
		Doctrine:   dn,
		Quota:      q,
		Dispatcher: d,
		Eventlog:   el,
		RateLimit:  rl,
		Store:      st,
	}
}

func TestFire_HappyPath_DispatchAndAdvance(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.012, DurationMs: 120, Tier: "tier-1-anthropic-bypass"}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire = %v, want nil", err)
	}
	if got := len(d.Calls()); got != 1 {
		t.Fatalf("dispatcher calls = %d, want 1", got)
	}
	if got := d.Calls()[0].ProjectAlias; got != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q, want internal-platform-x", got)
	}
	if got := d.Calls()[0].Action; got != "morning-brief" {
		t.Errorf("Action = %q, want morning-brief", got)
	}
	if d.Calls()[0].Metadata["schedule_id"] != s.ID {
		t.Errorf("metadata.schedule_id = %q, want %q", d.Calls()[0].Metadata["schedule_id"], s.ID)
	}
	if d.Calls()[0].Metadata["tier"] != "routine" {
		t.Errorf("metadata.tier = %q, want routine", d.Calls()[0].Metadata["tier"])
	}
	if d.Calls()[0].BackfillWindow != nil {
		t.Errorf("BackfillWindow = %+v, want nil on default-skip with no missed", d.Calls()[0].BackfillWindow)
	}
	if got := len(st.History()); got != 1 {
		t.Fatalf("history rows = %d, want 1", got)
	}
	if got := st.History()[0].Outcome; got != scheduler.OutcomeSuccess {
		t.Errorf("history[0].Outcome = %v, want OutcomeSuccess", got)
	}
	if got := st.History()[0].CostUSD; got != 0.012 {
		t.Errorf("history[0].CostUSD = %v, want 0.012", got)
	}
	if got := st.History()[0].DurationMs; got != 120 {
		t.Errorf("history[0].DurationMs = %d, want 120", got)
	}
	if got := el.CountKind(scheduler.EventRoutineFired); got != 1 {
		t.Errorf("EventRoutineFired count = %d, want 1; events=%+v", got, el.Events())
	}
	if got := len(st.NextRunCalls()); got != 1 {
		t.Errorf("UpdateNextRun calls = %d, want 1", got)
	} else {
		nrc := st.NextRunCalls()[0]
		if !nrc.lastRunAt.Equal(now) {
			t.Errorf("UpdateNextRun.lastRunAt = %v, want %v", nrc.lastRunAt, now)
		}
		if !nrc.nextRunAt.After(now) {
			t.Errorf("UpdateNextRun.nextRunAt = %v, want > now", nrc.nextRunAt)
		}
	}
	if !s.LastRunAt.Equal(now) {
		t.Errorf("schedule.LastRunAt = %v, want %v", s.LastRunAt, now)
	}
}

func TestFire_QuotaCap_BlocksDispatch(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	d := &fakeDispatcher{}
	q := &fakeQuota{allow: false, reason: "layer1:default:hard-deny:project_cap usage=110% cap=10000"}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	err := scheduler.Fire(context.Background(), s, deps)
	if !errors.Is(err, scheduler.ErrQuotaCap) {
		t.Fatalf("Fire = %v, want errors.Is ErrQuotaCap", err)
	}
	if got := len(d.Calls()); got != 0 {
		t.Errorf("dispatcher MUST NOT be called on quota cap; got %d calls", got)
	}
	if got := el.CountKind(scheduler.EventQuotaCapReached); got != 1 {
		t.Errorf("EventQuotaCapReached count = %d, want 1; events=%+v", got, el.Events())
	}

	for _, e := range el.Events() {
		if e.Kind == scheduler.EventQuotaCapReached {
			if e.Reason != q.reason {
				t.Errorf("event.Reason = %q, want %q", e.Reason, q.reason)
			}
		}
	}

	if got := len(st.NextRunCalls()); got != 0 {
		t.Errorf("UpdateNextRun calls on quota cap = %d, want 0", got)
	}
	// schedule.LastRunAt MUST NOT advance.
	if s.LastRunAt.Equal(now) {
		t.Errorf("schedule.LastRunAt advanced to %v on quota cap; should NOT", now)
	}
}

func TestFire_RateLimited_BlocksDispatch(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	d := &fakeDispatcher{}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: false}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	err := scheduler.Fire(context.Background(), s, deps)
	if !errors.Is(err, scheduler.ErrRateLimited) {
		t.Fatalf("Fire = %v, want errors.Is ErrRateLimited", err)
	}
	if got := len(d.Calls()); got != 0 {
		t.Errorf("dispatcher MUST NOT be called on rate limit; got %d calls", got)
	}
	if got := el.CountKind(scheduler.EventRateLimited); got != 1 {
		t.Errorf("EventRateLimited count = %d, want 1; events=%+v", got, el.Events())
	}
	if got := len(st.History()); got != 1 {
		t.Fatalf("history rows = %d, want 1 (rate-limited)", got)
	}
	if got := st.History()[0].Outcome; got != scheduler.OutcomeRateLimited {
		t.Errorf("history[0].Outcome = %v, want OutcomeRateLimited", got)
	}
}

func TestFire_DispatchFailure_PersistsFailure(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	upstream := errors.New("upstream provider down")
	d := &fakeDispatcher{err: upstream}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	err := scheduler.Fire(context.Background(), s, deps)
	if err == nil {
		t.Fatalf("Fire = nil, want wrapped dispatcher error")
	}
	if !errors.Is(err, upstream) {
		t.Errorf("Fire err = %v, want errors.Is upstream", err)
	}
	if got := len(st.History()); got != 1 {
		t.Fatalf("history rows = %d, want 1 (failed)", got)
	}
	if got := st.History()[0].Outcome; got != scheduler.OutcomeFailed {
		t.Errorf("history[0].Outcome = %v, want OutcomeFailed", got)
	}
	if got := el.CountKind(scheduler.EventRoutineFailed); got != 1 {
		t.Errorf("EventRoutineFailed count = %d, want 1; events=%+v", got, el.Events())
	}
	// schedule.LastRunAt MUST NOT advance on failure (caller will retry).
	if s.LastRunAt.Equal(now) {
		t.Errorf("schedule.LastRunAt advanced on failure; should NOT")
	}
}

func TestFire_QuotaError_Propagates(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	infraErr := errors.New("preflight: store unavailable")
	d := &fakeDispatcher{}
	q := &fakeQuota{err: infraErr}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	err := scheduler.Fire(context.Background(), s, deps)
	if !errors.Is(err, infraErr) {
		t.Fatalf("Fire = %v, want errors.Is infraErr", err)
	}
	if got := len(d.Calls()); got != 0 {
		t.Errorf("dispatcher MUST NOT be called on quota error; got %d calls", got)
	}
	if got := el.CountKind(scheduler.EventQuotaCapReached); got != 0 {
		t.Errorf("EventQuotaCapReached must NOT fire on infra error; got %d", got)
	}
}

func TestFire_MissPolicySkip_EmitsSkipEvents(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.MissPolicy = scheduler.MissPolicySkip

	s.LastRunAt = now.Add(-3 * 24 * time.Hour)
	s.MissLookback = 7 * 24 * time.Hour
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.01}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire (skip) = %v, want nil", err)
	}

	if got := el.CountKind(scheduler.EventRoutineSkipped); got == 0 {
		t.Errorf("EventRoutineSkipped count = 0, want >=1; events=%+v", el.Events())
	}

	if got := len(d.Calls()); got != 1 {
		t.Errorf("dispatcher calls under skip = %d, want 1 (current tick)", got)
	}
}

func TestFire_MissPolicyNotifyOnly_EmitsActionNeeded(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.MissPolicy = scheduler.MissPolicyNotifyOnly
	s.LastRunAt = now.Add(-2 * 24 * time.Hour)
	s.MissLookback = 7 * 24 * time.Hour
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.01}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameCapaFirewall)

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire (notify-only) = %v, want nil", err)
	}
	if got := el.CountKind(scheduler.EventMissedFire); got != 1 {
		t.Errorf("EventMissedFire count = %d, want 1; events=%+v", got, el.Events())
	}
	for _, e := range el.Events() {
		if e.Kind == scheduler.EventMissedFire {
			if e.Reason == "" {
				t.Errorf("EventMissedFire.Reason is empty; expected action-needed text")
			}
		}
	}
	if got := len(d.Calls()); got != 1 {
		t.Errorf("dispatcher calls under notify-only = %d, want 1 (current tick)", got)
	}
}

func TestFire_MissPolicyCoalesce_OneFireWithBackfill(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.MissPolicy = scheduler.MissPolicyCoalesce
	s.LastRunAt = now.Add(-3 * 24 * time.Hour)
	s.MissLookback = 7 * 24 * time.Hour
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.05}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameMaxScope)

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire (coalesce) = %v, want nil", err)
	}
	if got := len(d.Calls()); got != 1 {
		t.Fatalf("coalesce should fire ONCE; got %d", got)
	}
	if d.Calls()[0].BackfillWindow == nil {
		t.Errorf("coalesce dispatch BackfillWindow = nil, want non-nil")
	} else {
		bw := d.Calls()[0].BackfillWindow
		if !bw.To.Equal(now) {
			t.Errorf("BackfillWindow.To = %v, want %v", bw.To, now)
		}
		if !bw.From.Before(bw.To) {
			t.Errorf("BackfillWindow.From (%v) must be before To (%v)", bw.From, bw.To)
		}
	}
}

func TestFire_MissPolicyCatchUpBounded_FiresMultipleRateLimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.MissPolicy = scheduler.MissPolicyCatchUpBounded
	s.LastRunAt = now.Add(-5 * 24 * time.Hour)
	s.MissLookback = 7 * 24 * time.Hour
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.01}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}

	rl := &fakeRateLimit{allowSeq: []bool{true, true, false, false, true}, defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameMaxScope)

	err := scheduler.Fire(context.Background(), s, deps)
	if err != nil && !errors.Is(err, scheduler.ErrRateLimited) {
		t.Fatalf("Fire (catchup) = %v, want nil or ErrRateLimited", err)
	}

	if got := len(d.Calls()); got < 2 {
		t.Errorf("catch-up dispatcher calls = %d, want >=2 (catch-up before rate-limit)", got)
	}

	if got := el.CountKind(scheduler.EventRateLimited); got == 0 {
		t.Errorf("EventRateLimited count = 0, want >=1 on rate-limit during catch-up")
	}
}

func TestFire_NilSchedule_Rejected(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	deps := newDeps(now, &fakeQuota{allow: true}, &fakeDispatcher{}, &fakeEventLog{}, &fakeRateLimit{defaultOK: true}, &fakeStore{}, doctrine.NameDefault)
	err := scheduler.Fire(context.Background(), nil, deps)
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Fire(nil) = %v, want errors.Is ErrInvalidSchedule", err)
	}
}

func TestFire_InvalidSchedule_Rejected(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.ProjectAlias = ""
	deps := newDeps(now, &fakeQuota{allow: true}, &fakeDispatcher{}, &fakeEventLog{}, &fakeRateLimit{defaultOK: true}, &fakeStore{}, doctrine.NameDefault)
	err := scheduler.Fire(context.Background(), s, deps)
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Fire(invalid) = %v, want errors.Is ErrInvalidSchedule", err)
	}
}

func TestFire_ContextCancellation(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	d := &fakeDispatcher{err: context.Canceled}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := scheduler.Fire(ctx, s, deps)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("Fire(canceled) = %v, want errors.Is context.Canceled", err)
	}
	if got := len(st.History()); got != 1 {
		t.Errorf("history rows on cancel = %d, want 1 (failed)", got)
	}
}

func TestFire_CatchUp_DispatcherErrorAborts(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.MissPolicy = scheduler.MissPolicyCatchUpBounded
	s.LastRunAt = now.Add(-3 * 24 * time.Hour)
	s.MissLookback = 7 * 24 * time.Hour
	upstream := errors.New("catch-up dispatch boom")
	d := &fakeDispatcher{err: upstream}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameMaxScope)

	err := scheduler.Fire(context.Background(), s, deps)
	if !errors.Is(err, upstream) {
		t.Errorf("Fire = %v, want errors.Is upstream (catch-up dispatch failure)", err)
	}
	if got := el.CountKind(scheduler.EventRoutineFailed); got == 0 {
		t.Errorf("EventRoutineFailed count = 0, want >=1 on catch-up dispatch failure")
	}
}

// TestFire_NonCronTrigger_DoesNotAdvanceCursor asserts: a TierRoutine
// with TriggerHTTP fires successfully but does NOT call
// store.UpdateNextRun (HTTP triggers own their own cursor; the
// scheduler MUST NOT clobber it).
//
// This exercises the early-return branch in nextRunAfter for
// non-cron trigger types.
func TestFire_NonCronTrigger_DoesNotAdvanceCursor(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	s.TriggerType = scheduler.TriggerHTTP
	s.TriggerConfig = scheduler.TriggerConfig{BearerTokenHash: "deadbeef"}
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.01}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire (HTTP) = %v, want nil", err)
	}
	if got := len(d.Calls()); got != 1 {
		t.Errorf("dispatcher calls = %d, want 1", got)
	}
	if got := len(st.NextRunCalls()); got != 0 {
		t.Errorf("UpdateNextRun calls (HTTP trigger) = %d, want 0 (HTTP owns its cursor)", got)
	}
}

func TestFire_CorruptCron_NoNextRunAdvance(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)

	s.TriggerConfig.CronExpr = "this is not a cron expression"

	s.LastRunAt = now.Add(-30 * time.Second)
	d := &fakeDispatcher{result: scheduler.DispatchResult{CostUSD: 0.01}}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire (corrupt cron) = %v, want nil (live tick still dispatches)", err)
	}
	if got := len(d.Calls()); got != 1 {
		t.Errorf("dispatcher calls = %d, want 1 (live tick fires through)", got)
	}
	if got := len(st.NextRunCalls()); got != 0 {
		t.Errorf("UpdateNextRun calls (corrupt cron) = %d, want 0 (refuse to advance unknown cursor)", got)
	}
}

func TestFire_HistoryAppendError_StillReturnsDispatchError(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	s := newSchedule(t, now)
	upstream := errors.New("dispatch boom")
	d := &fakeDispatcher{err: upstream}
	q := &fakeQuota{allow: true}
	el := &fakeEventLog{}
	rl := &fakeRateLimit{defaultOK: true}
	st := &fakeStore{historyErr: errors.New("history db down")}
	deps := newDeps(now, q, d, el, rl, st, doctrine.NameDefault)

	err := scheduler.Fire(context.Background(), s, deps)
	if !errors.Is(err, upstream) {
		t.Errorf("Fire = %v, want errors.Is upstream (dispatcher error wins)", err)
	}
}
