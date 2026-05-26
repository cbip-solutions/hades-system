package worktreepool_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNewPool_HappyPath(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       2,
		ElasticMax:  4,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	emitter := &fakeEmitter{}
	exec := &fakeExec{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	if p == nil {
		t.Fatal("NewPool returned nil pool with nil error")
	}
}

func TestNewPool_RejectsInvalidConfig(t *testing.T) {
	tmpRepo := t.TempDir()
	tmpWT := t.TempDir()
	cases := []struct {
		name string
		cfg  worktreepool.PoolConfig
	}{
		{"zero floor", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 0, ElasticMax: 4}},
		{"floor exceeds elasticMax", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 8, ElasticMax: 4}},
		{"negative floor", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: -1, ElasticMax: 4}},
		{"empty RepoRoot", worktreepool.PoolConfig{RepoRoot: "", WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4}},
		{"empty WorktreeDir", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: "", BranchBase: "main", Floor: 1, ElasticMax: 4}},
		{"empty BranchBase", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "", Floor: 1, ElasticMax: 4}},
		{"relative RepoRoot", worktreepool.PoolConfig{RepoRoot: "rel/repo", WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4}},
		{"relative WorktreeDir", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: "rel/wt", BranchBase: "main", Floor: 1, ElasticMax: 4}},
		{"negative GCCadence", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4, GCCadence: -1}},

		{"GCCadence below 1s floor", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4, GCCadence: 1 * time.Nanosecond}},
		{"GCCadence 999ms below 1s floor", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4, GCCadence: 999 * time.Millisecond}},

		{"ElasticMax above sanity ceiling", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 1025}},
		{"ElasticMax million", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 1_000_000}},

		{"Doctrine typo max-scoped", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4, Doctrine: "max-scoped"}},
		{"Doctrine typo capafirewall", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4, Doctrine: "capafirewall"}},
		{"Doctrine arbitrary value", worktreepool.PoolConfig{RepoRoot: tmpRepo, WorktreeDir: tmpWT, BranchBase: "main", Floor: 1, ElasticMax: 4, Doctrine: "wow"}},
	}
	emitter := &fakeEmitter{}
	exec := &fakeExec{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := worktreepool.NewPool(c.cfg, emitter, exec)
			if err == nil {
				_ = p.Close(context.Background())
				t.Fatal("NewPool accepted invalid config")
			}
			if !errors.Is(err, worktreepool.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestNewPool_RejectsNilEmitter(t *testing.T) {
	cfg := worktreepool.PoolConfig{RepoRoot: t.TempDir(), WorktreeDir: t.TempDir(), BranchBase: "main", Floor: 1, ElasticMax: 2}
	exec := &fakeExec{}
	p, err := worktreepool.NewPool(cfg, nil, exec)
	if err == nil {
		_ = p.Close(context.Background())
		t.Fatal("NewPool accepted nil emitter")
	}
	if !errors.Is(err, worktreepool.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestNewPool_RejectsNilExec(t *testing.T) {
	cfg := worktreepool.PoolConfig{RepoRoot: t.TempDir(), WorktreeDir: t.TempDir(), BranchBase: "main", Floor: 1, ElasticMax: 2}
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, nil)
	if err == nil {
		_ = p.Close(context.Background())
		t.Fatal("NewPool accepted nil exec")
	}
	if !errors.Is(err, worktreepool.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestNewPool_AppliesDefaults(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestPool_CloseIsIdempotent(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second Close MUST be a no-op + return nil (idempotent shutdown for crash-recovery paths).
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("third Close: %v", err)
	}
}

// TestPool_CloseHonorsCtxDeadline asserts Close honors ctx-deadline.
//
// With B-1 shells (real goroutines that exit on ctx.Done immediately),
// Close MUST return nil within the deadline. White-box tests in
// pool_internal_test.go exercise the actual ctx.DeadlineExceeded arm
// (with simulated slow goroutines).
//
// When B-6/B-7 introduce real goroutines that may take longer to drain,
// this test will need to either: (a) be loosened to accept
// DeadlineExceeded with a longer initial timeout, or (b) be deleted in
// favor of the white-box tests. Permissive "nil or deadline" semantics
// were a B-1 anti-pattern.
func TestPool_CloseHonorsCtxDeadline(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v (B-1 shells must drain within 50ms)", err)
	}
}

func TestPool_OperationsAfterCloseReturnErrPoolClosed(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := p.Lease(context.Background()); !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("Lease after close: err = %v, want ErrPoolClosed", err)
	}
	if err := p.Release(context.Background(), nil); !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("Release after close: err = %v, want ErrPoolClosed", err)
	}
	if _, err := p.PruneOrphans(context.Background()); !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("PruneOrphans after close: err = %v, want ErrPoolClosed", err)
	}
}

func TestPool_PreCompletionMethodsReturnError(t *testing.T) {

	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	if _, err := p.Lease(context.Background()); err == nil {
		t.Fatal("Lease slow-path pre-completion: expected error, got nil")
	}
	if err := p.Release(context.Background(), nil); err == nil {
		t.Fatal("Release pre-completion: expected error, got nil")
	}
}

func TestPool_Lease_FastPath_WarmAvailable(t *testing.T) {

	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       2,
		ElasticMax:  4,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	if err := worktreepool.SeedWarmForTest(p, 2); err != nil {
		t.Fatalf("SeedWarmForTest: %v", err)
	}

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if w == nil {
		t.Fatal("Lease returned nil worktree")
	}
	if w.Path() == "" {
		t.Errorf("Worktree.Path() empty")
	}
	if w.ID() == 0 {
		t.Errorf("Worktree.ID() must be > 0")
	}
	if w.Branch() == "" {
		t.Errorf("Worktree.Branch() empty")
	}
	if w.CreatedAt().IsZero() {
		t.Errorf("Worktree.CreatedAt() zero")
	}

	w2, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease (2nd): %v", err)
	}
	if w2.ID() == w.ID() {
		t.Fatalf("Lease returned same id twice: %d", w.ID())
	}

	if _, err := p.Lease(context.Background()); err == nil {
		t.Fatal("Lease (3rd, warm-empty): want non-nil error, got nil")
	}
}

func TestPool_Lease_AfterClose_ReturnsErrPoolClosed(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := p.Lease(context.Background()); !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("Lease after Close: err = %v, want ErrPoolClosed", err)
	}
}

func TestPool_Lease_FastPath_RaceCloseBetweenFastPathAndMu(t *testing.T) {
	// Adversarial Close races with Lease such that the closed.Load
	// fast-path hint passes (false), then Close fires (Store(true) +
	// nil maps under mu). Lease's mu re-check MUST observe closed=true
	// and return ErrPoolClosed without touching the now-nil leased map.
	//
	// We seed warm so the fast path would otherwise run; then we call
	// Close before Lease takes mu. Since Lease's outer closed.Load is
	// also true post-Close, this is naturally an ErrPoolClosed return.
	// To exercise the under-mu re-check specifically, we use the white-
	// box helper SeedAndCloseForRaceTest in pool_testhelpers_test.go
	// which forces warm seeded + closed=true while leaving signalSlot
	// non-nil (mimicking a tight race window).
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := p.Lease(context.Background()); !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("Lease post-Close: err = %v, want ErrPoolClosed", err)
	}
}

func TestWorktree_Accessors(t *testing.T) {

	var w worktreepool.Worktree
	if got := w.Path(); got != "" {
		t.Fatalf("zero Worktree.Path() = %q, want \"\"", got)
	}
	if got := w.ID(); got != 0 {
		t.Fatalf("zero Worktree.ID() = %d, want 0", got)
	}
	if got := w.Branch(); got != "" {
		t.Fatalf("zero Worktree.Branch() = %q, want \"\"", got)
	}
	if got := w.CreatedAt(); !got.IsZero() {
		t.Fatalf("zero Worktree.CreatedAt() = %v, want zero", got)
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(worktreepool.ErrInvalidConfig, worktreepool.ErrPoolClosed) {
		t.Fatal("ErrInvalidConfig should not match ErrPoolClosed")
	}
	if errors.Is(worktreepool.ErrPoolClosed, worktreepool.ErrPoolExhausted) {
		t.Fatal("ErrPoolClosed should not match ErrPoolExhausted")
	}
	if errors.Is(worktreepool.ErrInvalidConfig, worktreepool.ErrPoolExhausted) {
		t.Fatal("ErrInvalidConfig should not match ErrPoolExhausted")
	}
}

type fakeEmitter struct {
	mu     sync.Mutex
	events []eventlog.Event
	nextID int64
}

func (f *fakeEmitter) Append(_ context.Context, evt eventlog.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, evt)
	f.nextID++
	return f.nextID, nil
}

func (f *fakeEmitter) eventTypes() []eventlog.EventType {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eventlog.EventType, len(f.events))
	for i, e := range f.events {
		out[i] = e.Type
	}
	return out
}

func (f *fakeEmitter) eventsSnapshot() []eventlog.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eventlog.Event, len(f.events))
	copy(out, f.events)
	return out
}

type fakeExec struct{}

var errFakeExecNotWired = errors.New("fakeExec: deliberate error-injection driving spawn-failure branch")

func (f *fakeExec) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, errFakeExecNotWired
}

type recordingExec struct {
	mu        sync.Mutex
	calls     [][]string
	scenarios map[string]struct {
		out []byte
		err error
	}
}

func newRecordingExec() *recordingExec {
	return &recordingExec{
		scenarios: make(map[string]struct {
			out []byte
			err error
		}),
	}
}

func (r *recordingExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := append([]string{name}, args...)
	r.calls = append(r.calls, rec)
	key := strings.Join(rec, " ")
	for prefix, sc := range r.scenarios {
		if strings.Contains(key, prefix) {
			return sc.out, sc.err
		}
	}
	return nil, nil
}

func (r *recordingExec) setScenario(prefix string, out []byte, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scenarios[prefix] = struct {
		out []byte
		err error
	}{out: out, err: err}
}

func (r *recordingExec) callsSnapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	for i, c := range r.calls {
		out[i] = append([]string(nil), c...)
	}
	return out
}

func TestPool_Lease_SlowPath_SpawnsElasticBelowM(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  3,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if w == nil {
		t.Fatal("nil worktree on slow path")
	}
	if w.Path() == "" {
		t.Errorf("Worktree.Path() empty after spawn")
	}
	if w.ID() == 0 {
		t.Errorf("Worktree.ID() must be > 0")
	}
	if w.Branch() == "" {
		t.Errorf("Worktree.Branch() empty after spawn")
	}
	if w.CreatedAt().IsZero() {
		t.Errorf("Worktree.CreatedAt() zero after spawn")
	}

	calls := exec.callsSnapshot()
	saw := false
	for _, c := range calls {

		if len(c) >= 5 && c[0] == "git" && c[3] == "worktree" && c[4] == "add" {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("expected `git worktree add` invocation; got: %v", calls)
	}

	for _, evt := range emitter.eventTypes() {
		if evt == eventlog.EvtWorktreePoolDegraded || evt == eventlog.EvtWorktreePoolExhausted {
			t.Errorf("unexpected pool-health event on success path: %v", evt)
		}
	}
}

func TestPool_Lease_SlowPath_BlocksAtElasticMaxThenCtxExpires(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w1, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease 1: %v", err)
	}
	if w1 == nil {
		t.Fatal("Lease 1 returned nil worktree")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = p.Lease(ctx)
	elapsed := time.Since(start)
	if !errors.Is(err, worktreepool.ErrPoolExhausted) {
		t.Fatalf("err=%v, want ErrPoolExhausted", err)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want errors.Is(context.DeadlineExceeded) (IMP-3 double-%%w)", err)
	}
	if elapsed < 80*time.Millisecond {
		t.Errorf("Lease returned too quickly (%v); should have blocked until ctx (~100ms)", elapsed)
	}

	sawExhausted := false
	for _, evt := range emitter.eventTypes() {
		if evt == eventlog.EvtWorktreePoolExhausted {
			sawExhausted = true
			break
		}
	}
	if !sawExhausted {
		t.Errorf("EvtWorktreePoolExhausted not emitted; saw: %v", emitter.eventTypes())
	}
}

func TestPool_Lease_SlowPath_ENOSPC_EmitsDegradedAndReturnsError(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "max-scope",
		PoolID:      "myproj",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree add",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"),
	)
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	_, err = p.Lease(context.Background())
	if err == nil {
		t.Fatal("want error on ENOSPC")
	}
	if !errors.Is(err, worktreepool.ErrPoolDegraded) {
		t.Errorf("err=%v, want errors.Is(ErrPoolDegraded)", err)
	}

	events := emitter.eventsSnapshot()
	var degraded *eventlog.Event
	for i, e := range events {
		if e.Type == eventlog.EvtWorktreePoolDegraded {
			degraded = &events[i]
			break
		}
	}
	if degraded == nil {
		t.Fatalf("EvtWorktreePoolDegraded not emitted; saw: %v", emitter.eventTypes())
	}

	if r, ok := degraded.Payload["reason"]; !ok || r != "ENOSPC" {
		t.Errorf("payload reason = %v, want ENOSPC", degraded.Payload["reason"])
	}
	if d, ok := degraded.Payload["doctrine"]; !ok || d != "max-scope" {
		t.Errorf("payload doctrine = %v, want max-scope", degraded.Payload["doctrine"])
	}
	if pid, ok := degraded.Payload["pool_id"]; !ok || pid != "myproj" {
		t.Errorf("payload pool_id = %v, want myproj", degraded.Payload["pool_id"])
	}
	if em, ok := degraded.Payload["elastic_max"]; !ok || em != 2 {
		t.Errorf("payload elastic_max = %v, want 2", degraded.Payload["elastic_max"])
	}

	errStr, _ := degraded.Payload["error"].(string)
	if errStr == "" {
		t.Errorf("payload error empty; want sanitized subprocess error string")
	}

	if _, err := p.Lease(context.Background()); err == nil {
		t.Logf("second Lease error (expected): %v", err)
	}
}

func TestPool_Lease_SlowPath_ConcurrentSpawnsRespectElasticMax(t *testing.T) {
	const M = 4
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  M,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	const N = M + 2
	results := make(chan error, N)
	worktrees := make(chan *worktreepool.Worktree, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			w, err := p.Lease(ctx)
			results <- err
			if w != nil {
				worktrees <- w
			}
		}()
	}
	wg.Wait()
	close(results)
	close(worktrees)

	successes := 0
	exhausted := 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, worktreepool.ErrPoolExhausted) {
			exhausted++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if successes != M {
		t.Errorf("successes = %d, want %d (ElasticMax)", successes, M)
	}
	if exhausted != N-M {
		t.Errorf("exhausted = %d, want %d (N-M)", exhausted, N-M)
	}

	seen := make(map[int64]bool)
	for w := range worktrees {
		if seen[w.ID()] {
			t.Errorf("duplicate Worktree.ID()=%d under concurrent spawn", w.ID())
		}
		seen[w.ID()] = true
	}
}

func TestPool_Lease_SlowPath_ExhaustedEmitDebounced(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	if _, err := p.Lease(context.Background()); err != nil {
		t.Fatalf("Lease 1 (saturating): %v", err)
	}

	const N = 10
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, _ = p.Lease(ctx)
		}()
	}
	wg.Wait()

	count := 0
	for _, evt := range emitter.eventTypes() {
		if evt == eventlog.EvtWorktreePoolExhausted {
			count++
		}
	}
	if count < 1 {
		t.Errorf("EvtWorktreePoolExhausted count = %d, want ≥ 1", count)
	}
	if count >= N {
		t.Errorf("EvtWorktreePoolExhausted count = %d, want ≪ %d (debounce ineffective)", count, N)
	}
}

func TestPool_Lease_SlowPath_ExhaustedEmitDebounced_Deterministic(t *testing.T) {
	fc := clock.NewFake(time.Unix(0, 0))
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		Clock:       fc,
	}
	exec := newRecordingExec()

	exec.setScenario("worktree add", nil, errors.New("exit status 1"))
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	if err := worktreepool.SeedWarmForTest(p, 1); err != nil {
		t.Fatalf("SeedWarmForTest: %v", err)
	}
	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease (saturate): %v", err)
	}
	if w == nil {
		t.Fatal("nil worktree from saturating lease")
	}

	saturate := func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := p.Lease(ctx)
		if !errors.Is(err, worktreepool.ErrPoolExhausted) {
			t.Fatalf("expected ErrPoolExhausted, got %v", err)
		}
	}

	countDegraded := func() int {
		n := 0
		for _, evt := range emitter.eventTypes() {
			if evt == eventlog.EvtWorktreePoolExhausted {
				n++
			}
		}
		return n
	}

	saturate()
	if got := countDegraded(); got != 1 {
		t.Fatalf("after T0 saturation: emissions=%d, want 1", got)
	}

	fc.Advance(50 * time.Millisecond)
	saturate()
	if got := countDegraded(); got != 1 {
		t.Fatalf("after T0+50ms (in window): emissions=%d, want 1", got)
	}

	fc.Advance(50 * time.Millisecond)
	saturate()
	if got := countDegraded(); got != 2 {
		t.Fatalf("after T0+100ms (window edge): emissions=%d, want 2", got)
	}

	fc.Advance(50 * time.Millisecond)
	saturate()
	if got := countDegraded(); got != 2 {
		t.Fatalf("after T0+150ms (in new window): emissions=%d, want 2", got)
	}

	fc.Advance(50 * time.Millisecond)
	saturate()
	if got := countDegraded(); got != 3 {
		t.Fatalf("after T0+200ms: emissions=%d, want 3", got)
	}
}

func TestPool_Lease_SlowPath_CtxCancelledMidBlock(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	if _, err := p.Lease(context.Background()); err != nil {
		t.Fatalf("Lease 1: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	leaseDone := make(chan error, 1)
	go func() {
		_, err := p.Lease(ctx)
		leaseDone <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-leaseDone:
		if !errors.Is(err, worktreepool.ErrPoolExhausted) {
			t.Errorf("err=%v, want errors.Is(ErrPoolExhausted)", err)
		}

		if !errors.Is(err, context.Canceled) {
			t.Errorf("err=%v, want errors.Is(context.Canceled) (IMP-3 double-%%w)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Lease did not return within 2s after ctx.Cancel")
	}
}

func TestPool_Lease_SlowPath_DegradedEmittedForAllPressureClasses(t *testing.T) {
	cases := []struct {
		name       string
		stderr     string
		wantReason string
	}{
		{"ENOSPC", "fatal: write error: No space left on device\n", "ENOSPC"},
		{"WorktreeLocked", "fatal: cannot lock ref 'refs/heads/zen-pool-p-1': File exists.\n", "WorktreeLocked"},
		{"Network", "fatal: unable to access 'https://github.com/foo/bar.git/': Failed to connect to github.com port 443\n", "Network"},
		{"Signal", "signal: killed\n", "Signal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := worktreepool.PoolConfig{
				RepoRoot:    t.TempDir(),
				WorktreeDir: t.TempDir(),
				BranchBase:  "main",
				Floor:       1,
				ElasticMax:  2,
				GCCadence:   1 * time.Second,
				Doctrine:    "max-scope",
				PoolID:      "myproj",
			}
			exec := newRecordingExec()
			exec.setScenario("worktree add",
				[]byte(tc.stderr),
				errors.New("exit status 128"),
			)
			emitter := &fakeEmitter{}
			p, err := worktreepool.NewPool(cfg, emitter, exec)
			if err != nil {
				t.Fatalf("NewPool: %v", err)
			}
			defer func() { _ = p.Close(context.Background()) }()

			_, err = p.Lease(context.Background())
			if err == nil {
				t.Fatal("want error from saturating-then-failing spawn")
			}
			if !errors.Is(err, worktreepool.ErrPoolDegraded) {
				t.Fatalf("err=%v, want errors.Is(ErrPoolDegraded)", err)
			}

			events := emitter.eventsSnapshot()
			var degraded *eventlog.Event
			for i, e := range events {
				if e.Type == eventlog.EvtWorktreePoolDegraded {
					degraded = &events[i]
					break
				}
			}
			if degraded == nil {
				t.Fatalf("EvtWorktreePoolDegraded not emitted; saw: %v", emitter.eventTypes())
			}
			if r, _ := degraded.Payload["reason"].(string); r != tc.wantReason {
				t.Errorf("payload reason = %v, want %s", degraded.Payload["reason"], tc.wantReason)
			}
			if d, _ := degraded.Payload["doctrine"].(string); d != "max-scope" {
				t.Errorf("payload doctrine = %v, want max-scope", degraded.Payload["doctrine"])
			}
			if pid, _ := degraded.Payload["pool_id"].(string); pid != "myproj" {
				t.Errorf("payload pool_id = %v, want myproj", degraded.Payload["pool_id"])
			}
		})
	}
}

// TestPool_Lease_SlowPath_NonPressureClassesNoDegradedEmit verifies the
// negative half of the IMP-1 contract: deterministic-bug / config-error
// classes (BranchExists, NotARepo, Panic, Other) MUST NOT emit
// EvtWorktreePoolDegraded. Otherwise HRA Phase I would needlessly
// downgrade voting strategy on a typo'd branch name or a programmer
// bug — both of which a backoff-and-retry policy cannot cure.
func TestPool_Lease_SlowPath_NonPressureClassesNoDegradedEmit(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
	}{
		{"BranchExists", "fatal: A branch named 'b' already exists.\n"},
		{"NotARepo", "fatal: not a git repository (or any of the parent directories): .git\n"},
		{"Panic", "runtime error: invalid memory address\n"},
		{"Other", "fatal: some other unexpected git failure\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := worktreepool.PoolConfig{
				RepoRoot:    t.TempDir(),
				WorktreeDir: t.TempDir(),
				BranchBase:  "main",
				Floor:       1,
				ElasticMax:  2,
				GCCadence:   1 * time.Second,
				Doctrine:    "default",
			}
			exec := newRecordingExec()
			exec.setScenario("worktree add",
				[]byte(tc.stderr),
				errors.New("exit status 128"),
			)
			emitter := &fakeEmitter{}
			p, err := worktreepool.NewPool(cfg, emitter, exec)
			if err != nil {
				t.Fatalf("NewPool: %v", err)
			}
			defer func() { _ = p.Close(context.Background()) }()

			_, err = p.Lease(context.Background())
			if err == nil {
				t.Fatal("want error")
			}
			if errors.Is(err, worktreepool.ErrPoolDegraded) {
				t.Fatalf("class %s must NOT wrap ErrPoolDegraded; got %v", tc.name, err)
			}
			for _, evt := range emitter.eventTypes() {
				if evt == eventlog.EvtWorktreePoolDegraded {
					t.Errorf("class %s must NOT emit EvtWorktreePoolDegraded; saw: %v",
						tc.name, emitter.eventTypes())
				}
			}
		})
	}
}

func TestPool_Lease_SlowPath_ENOSPC_DoesNotLeakSlot(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree add",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"),
	)
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	for i := 0; i < 3; i++ {
		_, err := p.Lease(context.Background())
		if err == nil {
			t.Fatalf("attempt %d: want ENOSPC error, got nil", i)
		}
		if !errors.Is(err, worktreepool.ErrPoolDegraded) {
			t.Fatalf("attempt %d: err=%v, want ErrPoolDegraded", i, err)
		}
		if errors.Is(err, worktreepool.ErrPoolExhausted) {
			t.Fatalf("attempt %d: phantom ErrPoolExhausted (slot leaked)", i)
		}
	}
}

func TestPool_Release_HappyPath_ResetsAndReturnsToWarm(t *testing.T) {
	// Floor==ElasticMax==1 pins the warm-reuse assertion against the B-6
	// prewarm goroutine: prewarm fills the single elastic slot, Lease
	// pops it, prewarm cannot spawn a replacement (total==ElasticMax)
	// so warm stays empty until Release. The re-leased worktree MUST
	// be the same id (warm-slice LIFO reuse). With ElasticMax>1 the
	// prewarm could race ahead and spawn a second elastic slot
	// between Release and the re-Lease, breaking the same-id assertion
	// non-deterministically. The contract being pinned here is "Release
	// returns the worktree to warm and subsequent Lease reuses it",
	// which the tightened ceiling exercises directly.
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if err := p.Release(context.Background(), w); err != nil {
		t.Fatalf("Release: %v", err)
	}

	calls := exec.callsSnapshot()
	sawReset, sawClean := false, false
	for _, c := range calls {
		j := strings.Join(c, " ")
		if strings.Contains(j, "reset --hard") && strings.Contains(j, "main") {
			sawReset = true
		}
		if strings.Contains(j, "clean -fdx") {
			sawClean = true
		}
	}
	if !sawReset || !sawClean {
		t.Errorf("expected reset+clean; saw reset=%v clean=%v calls=%v", sawReset, sawClean, calls)
	}

	addCallsBefore := 0
	for _, c := range calls {
		if strings.Contains(strings.Join(c, " "), "worktree add") {
			addCallsBefore++
		}
	}
	w2, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease 2: %v", err)
	}
	if w2.ID() != w.ID() {
		t.Errorf("expected re-leased worktree to have same ID; got %d, want %d", w2.ID(), w.ID())
	}
	addCallsAfter := 0
	for _, c := range exec.callsSnapshot() {
		if strings.Contains(strings.Join(c, " "), "worktree add") {
			addCallsAfter++
		}
	}
	if addCallsAfter != addCallsBefore {
		t.Errorf("re-Lease should reuse warm; got %d new worktree-add calls", addCallsAfter-addCallsBefore)
	}
}

func TestPool_Release_ResetFailure_DestroysAndDoesNotPanic(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	exec.setScenario("reset --hard", nil, errors.New("exit status 1"))

	if err := p.Release(context.Background(), w); err != nil {
		t.Fatalf("Release should not surface reset error: %v", err)
	}

	saw := false
	for _, c := range exec.callsSnapshot() {
		if strings.Contains(strings.Join(c, " "), "worktree remove --force") {
			saw = true
		}
	}
	if !saw {
		t.Error("expected `git worktree remove --force` after reset failure")
	}

	sawDegraded := false
	for _, e := range emitter.eventsSnapshot() {
		if e.Type != eventlog.EvtWorktreePoolDegraded {
			continue
		}
		if r, _ := e.Payload["reason"].(string); r == "release-destroyed" {
			sawDegraded = true
			break
		}
	}
	if !sawDegraded {
		t.Errorf("expected EvtWorktreePoolDegraded with reason=release-destroyed; saw %v", emitter.eventsSnapshot())
	}
}

func TestPool_Release_CleanFailure_DestroysAndDoesNotPanic(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exec.setScenario("clean -fdx", nil, errors.New("exit status 1"))

	if err := p.Release(context.Background(), w); err != nil {
		t.Fatalf("Release should not surface clean error: %v", err)
	}

	saw := false
	for _, c := range exec.callsSnapshot() {
		if strings.Contains(strings.Join(c, " "), "worktree remove --force") {
			saw = true
		}
	}
	if !saw {
		t.Error("expected `git worktree remove --force` after clean failure")
	}

	sawDegraded := false
	for _, e := range emitter.eventsSnapshot() {
		if e.Type != eventlog.EvtWorktreePoolDegraded {
			continue
		}
		if r, _ := e.Payload["reason"].(string); r == "release-destroyed" {
			sawDegraded = true
			break
		}
	}
	if !sawDegraded {
		t.Errorf("expected EvtWorktreePoolDegraded with reason=release-destroyed on clean failure; saw %v", emitter.eventsSnapshot())
	}
}

func TestPool_Release_DoubleRelease_ReturnsError(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, newRecordingExec())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Release(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if err := p.Release(context.Background(), w); err == nil {
		t.Fatal("want error on double-Release")
	}
}

func TestPool_Release_NilWorktree_ReturnsError(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, newRecordingExec())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	if err := p.Release(context.Background(), nil); err == nil {
		t.Fatal("want error on nil worktree")
	}
}

func TestPool_Release_AfterClose_ReturnsErrPoolClosed(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, newRecordingExec())
	if err != nil {
		t.Fatal(err)
	}
	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Release(context.Background(), w); !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Errorf("Release after Close: err=%v, want ErrPoolClosed", err)
	}
}

func TestPool_Release_WakesBlockedLease(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w1, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	type res struct {
		w   *worktreepool.Worktree
		err error
	}
	ch := make(chan res, 1)
	leaseCtx, leaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer leaseCancel()
	go func() {
		w2, err := p.Lease(leaseCtx)
		ch <- res{w: w2, err: err}
	}()

	time.Sleep(50 * time.Millisecond)

	if err := p.Release(context.Background(), w1); err != nil {
		t.Fatalf("Release: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Lease 2 unblocked with err=%v", r.err)
		}
		if r.w.ID() != w1.ID() {
			t.Errorf("Lease 2 should have popped released worktree (warm reuse); got id=%d, want %d",
				r.w.ID(), w1.ID())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Lease did not unblock within 2s after Release; signalSlot wakeup broken")
	}
}

func TestPool_Release_DestroyWakesBlockedLease(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "p",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	w1, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	type res struct {
		w   *worktreepool.Worktree
		err error
	}
	ch := make(chan res, 1)
	leaseCtx, leaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer leaseCancel()
	go func() {
		w2, err := p.Lease(leaseCtx)
		ch <- res{w: w2, err: err}
	}()

	time.Sleep(50 * time.Millisecond)

	exec.setScenario("reset --hard", nil, errors.New("exit status 1"))
	if err := p.Release(context.Background(), w1); err != nil {
		t.Fatalf("Release: %v", err)
	}

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Lease 2 unblocked with err=%v", r.err)
		}
		if r.w == nil {
			t.Fatal("Lease 2 returned nil worktree on success")
		}

		if r.w.ID() == w1.ID() {
			t.Errorf("Lease 2 should have spawned a fresh worktree after destroy; got same id=%d as destroyed", r.w.ID())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Lease did not unblock within 2s after destroy-Release; signalSlot wakeup broken")
	}
}
