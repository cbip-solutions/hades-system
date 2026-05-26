package hra_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

type fakeLease struct {
	path        string
	released    *atomic.Int32
	releaseErr  error
	releaseOnce atomic.Bool
}

func (l *fakeLease) Path() string { return l.path }
func (l *fakeLease) Release(_ context.Context) error {

	if !l.releaseOnce.CompareAndSwap(false, true) {
		return errors.New("fakeLease: double release")
	}
	l.released.Add(1)
	return l.releaseErr
}

type fakePool struct {
	released   atomic.Int32
	cap        int
	seq        atomic.Int32
	releaseErr error
}

func (p *fakePool) Lease(_ context.Context) (hra.Lease, error) {
	if p.cap > 0 && int(p.seq.Load()) >= p.cap {
		return nil, hra.ErrPoolExhausted
	}
	n := p.seq.Add(1)
	return &fakeLease{
		path:       fmt.Sprintf("/tmp/wt-%d", n),
		released:   &p.released,
		releaseErr: p.releaseErr,
	}, nil
}

type fakeApply struct {
	failOn map[string]error
	mu     sync.Mutex
	seen   []string
}

func (a *fakeApply) ApplyFix(_ context.Context, _ string, c hra.FixProposal) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = append(a.seen, c.ID)
	if e, ok := a.failOn[c.ID]; ok {
		return e
	}
	return nil
}

type scriptRunner struct {
	pass   []int
	fail   []int
	runErr error
	idx    atomic.Int32
}

func (r *scriptRunner) Run(_ context.Context, _ string) (int, int, error) {
	i := r.idx.Add(1) - 1
	if int(i) >= len(r.pass) {

		return 0, 0, fmt.Errorf("scriptRunner: out of scripted results (idx=%d, scripted=%d)", i, len(r.pass))
	}
	if r.runErr != nil {
		return r.pass[i], r.fail[i], r.runErr
	}
	return r.pass[i], r.fail[i], nil
}

type captureAppender struct {
	mu     sync.Mutex
	events []eventlog.Event
}

func (c *captureAppender) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return int64(len(c.events)), nil
}

func (c *captureAppender) snapshot() []eventlog.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]eventlog.Event, len(c.events))
	copy(out, c.events)
	return out
}

func (c *captureAppender) firstOf(t eventlog.EventType) (eventlog.Event, bool) {
	for _, ev := range c.snapshot() {
		if ev.Type == t {
			return ev, true
		}
	}
	return eventlog.Event{}, false
}

func newFMV(p hra.Pool, a hra.ApplyEngine, r hra.TestRunner, em eventlog.Appender) *hra.FMV {
	return hra.NewFMV(hra.FMVDeps{
		Pool:       p,
		Apply:      a,
		TestRunner: r,
		EventLog:   em,
		SessionID:  "sess-fmv",
		ProjectID:  "proj-fmv",
	})
}

func TestFMV_PicksHighestTestPassCount(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10, 7}, fail: []int{0, 3}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if res.Winner.ID != "X" {
		t.Fatalf("winner = %q, want X", res.Winner.ID)
	}
	if got := pool.seq.Load(); got != 2 {
		t.Fatalf("leased = %d, want 2", got)
	}
	if got := pool.released.Load(); got != 2 {
		t.Fatalf("released = %d, want 2", got)
	}
	ev, ok := app.firstOf(eventlog.EvtVotingDecisionMade)
	if !ok {
		t.Fatalf("no EvtVotingDecisionMade emitted; got %d events", len(app.snapshot()))
	}
	if ev.Payload["mechanism"] != "fmv" {
		t.Fatalf("mechanism = %v, want fmv", ev.Payload["mechanism"])
	}
	if ev.Payload["winner"] != "X" {
		t.Fatalf("winner payload = %v, want X", ev.Payload["winner"])
	}
}

func TestFMV_TieOnPassCount_BreaksByReviewerAgreement(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10, 10}, fail: []int{0, 0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 2},
		{ID: "Y", SupportingReviewers: 1},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if res.Winner.ID != "X" {
		t.Fatalf("winner = %q, want X (tied pass-count broken by reviewer agreement)", res.Winner.ID)
	}
}

func TestFMV_TrueTie_EscalatesL3(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10, 10}, fail: []int{0, 0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
	}
	_, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if !errors.Is(err, hra.ErrFMVTie) {
		t.Fatalf("err = %v, want ErrFMVTie", err)
	}
	if _, ok := app.firstOf(eventlog.EvtVotingDecisionMade); ok {
		t.Fatal("EvtVotingDecisionMade emitted on tie path; should escalate without picking a winner")
	}
}

func TestFMV_AllFailed_EmitsFMVAllFailed(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{0, 0}, fail: []int{10, 10}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
	}
	_, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if !errors.Is(err, hra.ErrFMVAllFailed) {
		t.Fatalf("err = %v, want ErrFMVAllFailed", err)
	}
	ev, ok := app.firstOf(eventlog.EvtFMVAllFailed)
	if !ok {
		t.Fatalf("no EvtFMVAllFailed emitted; got events: %+v", app.snapshot())
	}
	if got := ev.Payload["candidate_count"]; got != float64(2) && got != 2 {

		t.Fatalf("candidate_count = %v (%T), want 2", got, got)
	}
	if got := ev.Payload["test_failures"]; got != float64(20) && got != 20 {
		t.Fatalf("test_failures = %v (%T), want 20", got, got)
	}
	if _, ok := app.firstOf(eventlog.EvtVotingDecisionMade); ok {
		t.Fatal("EvtVotingDecisionMade emitted on all-failed path; should NOT")
	}
}

func TestFMV_ApplyError_CountsAsFailure(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	applyErr := errors.New("apply: patch did not apply cleanly")
	apply := &fakeApply{failOn: map[string]error{"X": applyErr}}

	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if res.Winner.ID != "Y" {
		t.Fatalf("winner = %q, want Y", res.Winner.ID)
	}
	if got := pool.released.Load(); got != 2 {
		t.Fatalf("released = %d, want 2 (X's lease must release even after apply failure)", got)
	}
	if len(res.Trace) != 2 {
		t.Fatalf("trace len = %d, want 2", len(res.Trace))
	}

	var xRow hra.FMVTrace
	for _, r := range res.Trace {
		if r.Candidate.ID == "X" {
			xRow = r
		}
	}
	if xRow.ApplyErr == nil {
		t.Fatal("X.ApplyErr = nil, want non-nil")
	}
	if !errors.Is(xRow.ApplyErr, applyErr) {
		t.Fatalf("X.ApplyErr = %v, want wraps %v", xRow.ApplyErr, applyErr)
	}
}

func TestFMV_NoCandidates_ReturnsErrNoVotes(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	_, err := newFMV(pool, apply, runner, app).Run(context.Background(), nil, hra.FMVOptions{})
	if !errors.Is(err, hra.ErrNoVotes) {
		t.Fatalf("err = %v, want ErrNoVotes", err)
	}
	if pool.seq.Load() != 0 {
		t.Fatal("pool was leased on empty-candidate path")
	}
	if len(app.snapshot()) != 0 {
		t.Fatalf("events emitted on empty-candidate path: %+v", app.snapshot())
	}
}

func TestFMV_SingleCandidate_Passes_Wins(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{{ID: "lone", SupportingReviewers: 1}}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if res.Winner.ID != "lone" {
		t.Fatalf("winner = %q, want lone", res.Winner.ID)
	}
}

func TestFMV_SingleCandidate_Fails_AllFailed(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{0}, fail: []int{10}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{{ID: "lone", SupportingReviewers: 1}}
	_, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if !errors.Is(err, hra.ErrFMVAllFailed) {
		t.Fatalf("err = %v, want ErrFMVAllFailed", err)
	}
}

func TestFMV_PoolExhausted_DegradesToPlurality(t *testing.T) {
	t.Parallel()
	pool := &fakePool{cap: 1}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 2},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v (I-5: degraded path with clear plurality must not error)", err)
	}
	if !res.Degraded {
		t.Fatal("res.Degraded = false, want true")
	}
	if res.Reason != "pool_exhausted" {
		t.Fatalf("res.Reason = %q, want pool_exhausted", res.Reason)
	}
	if res.Winner.ID != "Y" {
		t.Fatalf("winner = %q, want Y (highest agreement on FULL set)", res.Winner.ID)
	}
	if len(res.Trace) != 1 {
		t.Fatalf("trace len = %d, want 1 (first candidate processed before exhaustion)", len(res.Trace))
	}
	if got := pool.released.Load(); got != 1 {
		t.Fatalf("released = %d, want 1 (the one successful lease must release)", got)
	}
	if errors.Is(err, hra.ErrPoolExhausted) {
		t.Fatal("err matches ErrPoolExhausted; I-5 contract: pool exhaustion is degradation, not error")
	}
}

func TestFMV_LeaseRelease_Balanced_OnSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10, 7, 3}, fail: []int{0, 3, 7}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "A", SupportingReviewers: 1},
		{ID: "B", SupportingReviewers: 1},
		{ID: "C", SupportingReviewers: 1},
	}
	if _, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{}); err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if got, want := pool.seq.Load(), int32(3); got != want {
		t.Fatalf("leased = %d, want %d", got, want)
	}
	if got, want := pool.released.Load(), int32(3); got != want {
		t.Fatalf("released = %d, want %d", got, want)
	}
}

func TestFMV_NonExhaustedLeaseError_PropagatesAsFatal(t *testing.T) {
	t.Parallel()
	leaseFatal := errors.New("worktreepool: pool closed")
	pool := &errLeasePool{err: leaseFatal}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	candidates := []hra.FixProposal{{ID: "X", SupportingReviewers: 1}}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if !errors.Is(err, leaseFatal) {
		t.Fatalf("err = %v, want errors.Is(leaseFatal)=true", err)
	}
	if errors.Is(err, hra.ErrPoolExhausted) {
		t.Fatalf("err = %v, must NOT match ErrPoolExhausted (substrate bug, not cost pressure)", err)
	}
	if res.Winner.ID != "" {
		t.Fatalf("winner picked despite fatal lease error: %q", res.Winner.ID)
	}
}

type errLeasePool struct{ err error }

func (p *errLeasePool) Lease(_ context.Context) (hra.Lease, error) {
	return nil, p.err
}

// TestFMV_NewFMV_PanicsOnNilOrEmpty each contract violation (nil
// dependency, empty session/project ID) MUST panic at NewFMV time so
// orchestrator-init contract bugs surface immediately rather than at
// first Run. Mirrors the documented invariant in NewFMV's docstring.
func TestFMV_NewFMV_PanicsOnNilOrEmpty(t *testing.T) {
	t.Parallel()

	base := func() hra.FMVDeps {
		return hra.FMVDeps{
			Pool:       &fakePool{},
			Apply:      &fakeApply{},
			TestRunner: &scriptRunner{},
			EventLog:   &captureAppender{},
			SessionID:  "sess",
			ProjectID:  "proj",
		}
	}
	cases := []struct {
		name   string
		mutate func(*hra.FMVDeps)
	}{
		{"nil Pool", func(d *hra.FMVDeps) { d.Pool = nil }},
		{"nil Apply", func(d *hra.FMVDeps) { d.Apply = nil }},
		{"nil TestRunner", func(d *hra.FMVDeps) { d.TestRunner = nil }},
		{"nil EventLog", func(d *hra.FMVDeps) { d.EventLog = nil }},
		{"empty SessionID", func(d *hra.FMVDeps) { d.SessionID = "" }},
		{"empty ProjectID", func(d *hra.FMVDeps) { d.ProjectID = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("NewFMV did not panic on %s", c.name)
				}
			}()
			d := base()
			c.mutate(&d)
			_ = hra.NewFMV(d)
		})
	}
}

func TestFMV_NewFMV_DefaultsClock(t *testing.T) {
	t.Parallel()
	fmv := hra.NewFMV(hra.FMVDeps{
		Pool:       &fakePool{},
		Apply:      &fakeApply{},
		TestRunner: &scriptRunner{pass: []int{10}, fail: []int{0}},
		EventLog:   &captureAppender{},
		SessionID:  "sess",
		ProjectID:  "proj",
	})

	res, err := fmv.Run(context.Background(),
		[]hra.FixProposal{{ID: "lone", SupportingReviewers: 1}},
		hra.FMVOptions{})
	if err != nil {
		t.Fatalf("Run with default Clock: %v", err)
	}
	if res.Winner.ID != "lone" {
		t.Fatalf("winner = %q, want lone", res.Winner.ID)
	}
}

func TestFMV_AdaptPool_NilReturnsNil(t *testing.T) {
	t.Parallel()
	if hra.AdaptPool(nil) != nil {
		t.Fatal("AdaptPool(nil) returned non-nil; want nil so NewFMV catches the misconfiguration")
	}
}

func TestFMV_ReleaseError_DoesNotAbortRun(t *testing.T) {
	t.Parallel()
	releaseErr := errors.New("release: gitReset failed")
	pool := &fakePool{releaseErr: releaseErr}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10, 7}, fail: []int{0, 3}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v (release errors must not abort the run)", err)
	}
	if res.Winner.ID != "X" {
		t.Fatalf("winner = %q, want X", res.Winner.ID)
	}

	for _, row := range res.Trace {
		if row.RunErr == nil {
			t.Fatalf("row %s: RunErr = nil, want non-nil release error", row.Candidate.ID)
		}
		if !errors.Is(row.RunErr, releaseErr) {
			t.Fatalf("row %s: RunErr = %v, want wraps %v", row.Candidate.ID, row.RunErr, releaseErr)
		}
	}
}
