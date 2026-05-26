package subprocess

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/testharness"
)

func fakeFactory(t *testing.T, scenario string) Factory {
	t.Helper()
	return func(ctx context.Context, spec WorkerSpecRef) (Session, error) {
		_ = ctx
		cf := func(name string, arg ...string) *exec.Cmd {
			return testharness.BuildFakeCmd("TestHelperOpenClaudeFakeSubprocess",
				scenario, string(spec.ThreadID), spec.Worktree)
		}
		sess, err := newOpenClaudeSession(openClaudeOptions{
			Binary:      "openclaude",
			ThreadID:    spec.ThreadID,
			Worktree:    spec.Worktree,
			commandFunc: cf,
		})
		if err == nil {
			sess.closeGrace = 200 * time.Millisecond
		}
		return sess, err
	}
}

func TestSpawnEphemeralRoundTrip(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	tid, err := NewThreadID()
	if err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sess, err := mgr.SpawnEphemeral(ctx, WorkerSpecRef{
		SpecID:       "ephemeral-1",
		Variant:      VariantWorker,
		ThreadID:     tid,
		Worktree:     wt,
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("SpawnEphemeral: %v", err)
	}

	if err := sess.Send(ctx, Message{
		Kind: MessageKindRequest, ID: "1", Method: "prompt", ThreadID: tid,
		Payload: json.RawMessage(`{"text":"x"}`),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := sess.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.Kind != MessageKindResult {
		t.Errorf("Kind = %v, want result", got.Kind)
	}

	if err := mgr.Release(ctx, sess); err != nil {
		t.Errorf("Release: %v", err)
	}
}

func TestSpawnEphemeralReleaseKillsProcess(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	tid, _ := NewThreadID()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sess, err := mgr.SpawnEphemeral(ctx, WorkerSpecRef{
		SpecID:       "ephemeral-2",
		Variant:      VariantWorker,
		ThreadID:     tid,
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("SpawnEphemeral: %v", err)
	}
	if err := mgr.Release(ctx, sess); err != nil {
		t.Errorf("Release: %v", err)
	}

	err = sess.Send(ctx, Message{Kind: MessageKindRequest, ID: "x"})
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send after Release: err = %v, want ErrSessionClosed", err)
	}
}

func TestSpawnEphemeralRejectsEmptyWorktree(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	tid, _ := NewThreadID()
	_, err = mgr.SpawnEphemeral(context.Background(), WorkerSpecRef{
		SpecID:       "bad",
		Variant:      VariantWorker,
		ThreadID:     tid,
		Worktree:     "",
		DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("SpawnEphemeral with empty worktree returned nil err")
	}
}

func TestSpawnEphemeralRejectsEmptySpecID(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.SpawnEphemeral(context.Background(), WorkerSpecRef{
		SpecID:       "",
		Variant:      VariantWorker,
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("SpawnEphemeral with empty SpecID returned nil err")
	}
}

func TestSpawnEphemeralRejectsEmptyDoctrine(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.SpawnEphemeral(context.Background(), WorkerSpecRef{
		SpecID:       "x",
		Variant:      VariantWorker,
		Worktree:     t.TempDir(),
		DoctrineName: "",
	})
	if err == nil {
		t.Fatal("SpawnEphemeral with empty doctrine returned nil err")
	}
}

func TestSpawnEphemeralAssignsThreadIDIfZero(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sess, err := mgr.SpawnEphemeral(ctx, WorkerSpecRef{
		SpecID:       "auto-tid",
		Variant:      VariantWorker,
		ThreadID:     "",
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("SpawnEphemeral: %v", err)
	}
	defer mgr.Release(ctx, sess)
	if sess.ThreadID().IsZero() {
		t.Fatal("Manager did not assign a ThreadID")
	}
}

func TestSpawnEphemeralParallelDoesNotShareSessions(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	const N = 8
	wg := sync.WaitGroup{}
	wg.Add(N)
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			tid, _ := NewThreadID()
			sess, err := mgr.SpawnEphemeral(ctx, WorkerSpecRef{
				SpecID:       "parallel",
				Variant:      VariantWorker,
				ThreadID:     tid,
				Worktree:     t.TempDir(),
				DoctrineName: "default",
			})
			if err != nil {
				errs <- err
				return
			}
			_ = mgr.Release(ctx, sess)
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("parallel spawn err: %v", e)
	}
}

func TestNewManagerRejectsNilFactory(t *testing.T) {
	_, err := NewManager(ManagerOptions{Factory: nil})
	if err == nil {
		t.Fatal("NewManager with nil Factory returned nil err")
	}
}

func TestNewManagerDefaults(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	if mgr.evictorIvl != 60*time.Second {
		t.Errorf("EvictorInterval default = %v, want 60s", mgr.evictorIvl)
	}
	if mgr.grace != 10*time.Second {
		t.Errorf("SigtermGrace default = %v, want 10s", mgr.grace)
	}
}

func TestReleaseRejectsCancelledContext(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mgr.Release(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Release(cancelled, nil) = %v, want context.Canceled", err)
	}
}

func TestShutdownRejectsCancelledContext(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	defer mgr.Shutdown(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mgr.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Shutdown(cancelled) = %v, want context.Canceled", err)
	}
}

func TestReleaseRejectsNil(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	if err := mgr.Release(context.Background(), nil); err == nil {
		t.Fatal("Release(nil) returned nil err")
	}
}

func TestReleasePersistentIsNoOp(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ms := newMemSession(ThreadID("not-tracked"))
	defer ms.Close()
	if err := mgr.Release(context.Background(), ms); err != nil {
		t.Errorf("Release of untracked session: %v", err)
	}

	if err := ms.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "x"}); err != nil {
		t.Errorf("memSession was unexpectedly closed by Release: %v", err)
	}
}

func TestShutdownIdempotent(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown #1: %v", err)
	}
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown #2: %v", err)
	}
}

func TestShutdownClosesActiveEphemeral(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	tid, _ := NewThreadID()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sess, err := mgr.SpawnEphemeral(ctx, WorkerSpecRef{
		SpecID:       "leaked",
		Variant:      VariantWorker,
		ThreadID:     tid,
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("SpawnEphemeral: %v", err)
	}
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if err := sess.Send(ctx, Message{Kind: MessageKindRequest, ID: "x"}); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send post-Shutdown: err = %v, want ErrSessionClosed", err)
	}
}

func TestSpawnEphemeralRandReadError(t *testing.T) {
	prev := randRead
	defer func() { randRead = prev }()
	randRead = func(_ []byte) (int, error) { return 0, errors.New("rand: synthetic") }
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.SpawnEphemeral(context.Background(), WorkerSpecRef{
		SpecID:       "x",
		Variant:      VariantWorker,
		ThreadID:     "",
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("rand error not surfaced")
	}
}

func TestShutdownClosesActivePersistent(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: fakeFactory(t, "happy-path"),
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ms := newMemSession(ThreadID("manual-pers"))
	mgr.mu.Lock()
	manualNow := time.Now()
	mgr.persistents[persistentKey{specID: "manual", doctrineName: "d"}] = &persistentEntry{
		sess:      ms,
		specRef:   WorkerSpecRef{SpecID: "manual", DoctrineName: "d"},
		startedAt: manualNow,
		lastUse:   manualNow,
		ttl:       time.Hour,
	}
	mgr.mu.Unlock()
	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if err := ms.Send(context.Background(), Message{Kind: MessageKindRequest, ID: "x"}); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("memSession not closed by Shutdown: %v", err)
	}
}

func TestSpawnEphemeralFactoryError(t *testing.T) {
	failingFactory := func(_ context.Context, _ WorkerSpecRef) (Session, error) {
		return nil, errors.New("factory: synthetic")
	}
	mgr, err := NewManager(ManagerOptions{
		Factory: failingFactory,
		Clock:   realClock{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	tid, _ := NewThreadID()
	_, err = mgr.SpawnEphemeral(context.Background(), WorkerSpecRef{
		SpecID:       "x",
		Variant:      VariantWorker,
		ThreadID:     tid,
		Worktree:     t.TempDir(),
		DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("factory error not surfaced")
	}
}

func TestVariantStringAndIsPersistent(t *testing.T) {
	cases := []struct {
		v          Variant
		s          string
		persistent bool
	}{
		{VariantWorker, "worker", false},
		{VariantTeamLead, "teamlead", true},
		{VariantReviewerL2, "reviewer-l2", false},
		{VariantReviewerL3, "reviewer-l3", true},
		{VariantReviewerL4, "reviewer-l4", true},
	}
	for _, c := range cases {
		if got := c.v.String(); got != c.s {
			t.Errorf("Variant(%d).String() = %q, want %q", c.v, got, c.s)
		}
		if got := c.v.IsPersistent(); got != c.persistent {
			t.Errorf("Variant(%d).IsPersistent() = %v, want %v", c.v, got, c.persistent)
		}
	}

	if Variant(99).String() == "" {
		t.Error("Unknown Variant stringified to empty")
	}
	if Variant(99).IsPersistent() {
		t.Error("Variant(99).IsPersistent() = true")
	}
}

func TestWorkerSpecRefValidate(t *testing.T) {

	ok := WorkerSpecRef{SpecID: "x", Worktree: "/tmp", DoctrineName: "d"}
	if err := ok.Validate(); err != nil {
		t.Errorf("Validate ok: %v", err)
	}

	if err := (WorkerSpecRef{Worktree: "/tmp", DoctrineName: "d"}).Validate(); err == nil {
		t.Error("Validate missing SpecID accepted")
	}

	if err := (WorkerSpecRef{SpecID: "x", DoctrineName: "d"}).Validate(); err == nil {
		t.Error("Validate missing Worktree accepted")
	}

	if err := (WorkerSpecRef{SpecID: "x", Worktree: "/tmp"}).Validate(); err == nil {
		t.Error("Validate missing DoctrineName accepted")
	}
}

func TestRealClockTickerFires(t *testing.T) {
	c := realClock{}
	tk := c.NewTicker(20 * time.Millisecond)
	defer tk.Stop()
	select {
	case <-tk.C():

	case <-time.After(time.Second):
		t.Fatal("realClock ticker did not fire")
	}

	if c.Now().IsZero() {
		t.Error("realClock Now() returned zero time")
	}
}

func TestStaticTTLLookup(t *testing.T) {
	r := staticTTL{"a": time.Second}
	if d, err := r.SubprocessTTL("a"); err != nil || d != time.Second {
		t.Errorf("SubprocessTTL(a) = %v, %v", d, err)
	}
	if _, err := r.SubprocessTTL("missing"); err == nil {
		t.Error("SubprocessTTL(missing) returned nil err")
	}
}

type memSessionStore struct {
	mu        sync.Mutex
	rows      map[persistentKey]PersistentRow
	upsertErr error
}

func newMemSessionStore() *memSessionStore {
	return &memSessionStore{rows: make(map[persistentKey]PersistentRow)}
}

func (s *memSessionStore) UpsertPersistent(_ context.Context, row PersistentRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.rows[persistentKey{row.SpecID, row.DoctrineName}] = row
	return nil
}

func (s *memSessionStore) DeletePersistent(_ context.Context, specID, doctrineName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, persistentKey{specID, doctrineName})
	return nil
}

func (s *memSessionStore) ListPersistent(_ context.Context) ([]PersistentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PersistentRow, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}

func TestAcquirePersistentIdempotent(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"max-scope": 8 * time.Hour, "default": 4 * time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID:       "skeptic",
		Variant:      VariantTeamLead,
		Worktree:     t.TempDir(),
		DoctrineName: "max-scope",
	}
	first, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	second, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire #2: %v", err)
	}
	if first != second {
		t.Errorf("idempotency violated: %p != %p", first, second)
	}
	if first.ThreadID() != second.ThreadID() {
		t.Errorf("ThreadID changed: %s != %s", first.ThreadID(), second.ThreadID())
	}
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 1 {
		t.Errorf("subprocess_sessions rows = %d, want 1", len(rows))
	}
}

func TestAcquirePersistentDifferentDoctrinesAreDistinct(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"max-scope": 8 * time.Hour, "default": 4 * time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, err := mgr.AcquirePersistent(ctx, WorkerSpecRef{
		SpecID: "shared", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "max-scope",
	})
	if err != nil {
		t.Fatalf("Acquire A: %v", err)
	}
	b, err := mgr.AcquirePersistent(ctx, WorkerSpecRef{
		SpecID: "shared", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	})
	if err != nil {
		t.Fatalf("Acquire B: %v", err)
	}
	if a == b {
		t.Errorf("doctrines collided: same Session for max-scope + default")
	}
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 2 {
		t.Errorf("subprocess_sessions rows = %d, want 2", len(rows))
	}
}

func TestAcquirePersistentRejectsEphemeralVariant(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": 4 * time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "x", Variant: VariantWorker,
		Worktree: t.TempDir(), DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("AcquirePersistent accepted VariantWorker; want error")
	}
}

func TestAcquirePersistentRequiresDoctrineTTLs(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "x", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("AcquirePersistent without DoctrineTTLs returned nil err")
	}
}

func TestAcquirePersistentPreservesStartedAt(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": 4 * time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "started-at", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	rows1, _ := store.ListPersistent(ctx)
	if len(rows1) != 1 {
		t.Fatalf("rows after Acquire #1 = %d", len(rows1))
	}
	startedAt1 := rows1[0].StartedAt

	time.Sleep(30 * time.Millisecond)
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatalf("Acquire #2: %v", err)
	}
	rows2, _ := store.ListPersistent(ctx)
	if len(rows2) != 1 {
		t.Fatalf("rows after Acquire #2 = %d", len(rows2))
	}
	startedAt2 := rows2[0].StartedAt
	if !startedAt1.Equal(startedAt2) {
		t.Errorf("StartedAt drifted across refresh: first=%v second=%v (want equal)", startedAt1, startedAt2)
	}
	// LastUseAt MUST advance even though StartedAt does not.
	if !rows2[0].LastUseAt.After(rows1[0].LastUseAt) {
		t.Errorf("LastUseAt did not advance across refresh: first=%v second=%v",
			rows1[0].LastUseAt, rows2[0].LastUseAt)
	}
}

func TestAcquirePersistentTouchesLastUseOnRebind(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": 4 * time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "skeptic", Variant: VariantReviewerL3,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatal(err)
	}
	rows1, _ := store.ListPersistent(ctx)
	if len(rows1) != 1 {
		t.Fatalf("rows after #1 = %d", len(rows1))
	}
	first := rows1[0].LastUseAt
	time.Sleep(20 * time.Millisecond)
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatal(err)
	}
	rows2, _ := store.ListPersistent(ctx)
	second := rows2[0].LastUseAt
	if !second.After(first) {
		t.Errorf("LastUseAt did not advance: first=%v second=%v", first, second)
	}
}

func TestAcquirePersistentRejectsBadSpec(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": time.Hour},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "",
	})
	if err == nil {
		t.Fatal("missing SpecID accepted")
	}
}

func TestAcquirePersistentTTLResolverError(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "x", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "missing",
	})
	if err == nil {
		t.Fatal("missing TTL accepted")
	}
}

func TestAcquirePersistentFactoryError(t *testing.T) {
	mgr, err := NewManager(ManagerOptions{
		Factory: func(_ context.Context, _ WorkerSpecRef) (Session, error) {
			return nil, errors.New("factory: synthetic")
		},
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": time.Hour},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "x", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("factory error not surfaced")
	}
}

func TestAcquirePersistentRandError(t *testing.T) {
	prev := randRead
	defer func() { randRead = prev }()
	randRead = func(_ []byte) (int, error) { return 0, errors.New("rand: synthetic") }
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": time.Hour},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "x", Variant: VariantTeamLead,
		ThreadID: "",
		Worktree: t.TempDir(), DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("rand error not surfaced")
	}
}

func TestAcquirePersistentStoreUpsertErrorTearsDown(t *testing.T) {
	store := newMemSessionStore()
	store.upsertErr = errors.New("store: synthetic")
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	_, err = mgr.AcquirePersistent(context.Background(), WorkerSpecRef{
		SpecID: "x", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	})
	if err == nil {
		t.Fatal("store error not surfaced")
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.persistents) != 0 {
		t.Errorf("persistents = %d, want 0 (entry must be torn down)", len(mgr.persistents))
	}
}

func TestAcquirePersistentRefreshUpsertError(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	spec := WorkerSpecRef{
		SpecID: "x", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(context.Background(), spec); err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	store.mu.Lock()
	store.upsertErr = errors.New("refresh: synthetic")
	store.mu.Unlock()
	if _, err := mgr.AcquirePersistent(context.Background(), spec); err == nil {
		t.Fatal("refresh error not surfaced")
	}
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTicker{c: make(chan time.Time, 4), d: d}
	c.tickers = append(c.tickers, t)
	return t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	tickers := append([]*fakeTicker(nil), c.tickers...)
	now := c.now
	c.mu.Unlock()
	for _, t := range tickers {
		select {
		case t.c <- now:
		default:
		}
	}
}

type fakeTicker struct {
	c chan time.Time
	d time.Duration
}

func (t *fakeTicker) C() <-chan time.Time { return t.c }
func (t *fakeTicker) Stop()               {}

func TestEvictorEvictsPastTTLPersistent(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "happy-path"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 100 * time.Millisecond},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "skeptic", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	sess, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	clk.Advance(150 * time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := store.ListPersistent(ctx)
		if len(rows) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 0 {
		t.Errorf("rows after eviction = %d, want 0", len(rows))
	}

	err = sess.Send(ctx, Message{Kind: MessageKindRequest, ID: "x"})
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send after eviction: err = %v, want ErrSessionClosed", err)
	}
}

func TestEvictorEscalatesToSigkill(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "hang"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 100 * time.Millisecond},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "hang-tl", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	sess, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	readyCtx, readyCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readyCancel()
	ready, err := sess.Receive(readyCtx)
	if err != nil {
		t.Fatalf("ready Receive: %v", err)
	}
	if ready.Method != "ready" {
		t.Errorf("first frame method = %q, want ready", ready.Method)
	}
	clk.Advance(200 * time.Millisecond)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := store.ListPersistent(ctx)
		if len(rows) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 0 {
		t.Errorf("rows after SIGKILL escalation = %d, want 0", len(rows))
	}
}

func TestEvictorPreservesActivePersistent(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "happy-path"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 10 * time.Second},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "alive", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	sess, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	clk.Advance(1 * time.Second)
	time.Sleep(150 * time.Millisecond)
	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 1 {
		t.Errorf("rows for active session = %d, want 1", len(rows))
	}

	again, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if again != sess {
		t.Errorf("active session was replaced")
	}
}

func TestEvictorWithMemSession(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory: func(_ context.Context, spec WorkerSpecRef) (Session, error) {
			return newMemSession(spec.ThreadID), nil
		},
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 50 * time.Millisecond},
		SessionStore:    store,
		EvictorInterval: 25 * time.Millisecond,
		SigtermGrace:    25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "memsess", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := store.ListPersistent(ctx)
		if len(rows) == 0 {
			return
		}
		clk.Advance(100 * time.Millisecond)
		time.Sleep(20 * time.Millisecond)
	}
	rows, _ := store.ListPersistent(ctx)
	t.Errorf("memSession not evicted: rows = %d", len(rows))
}

func TestEvictorShutdownDuringEviction(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "hang"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 50 * time.Millisecond},
		SessionStore:    store,
		EvictorInterval: 25 * time.Millisecond,

		SigtermGrace: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "shutdown-during-evict", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	sess, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	readyCtx, readyCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readyCancel()
	if _, err := sess.Receive(readyCtx); err != nil {
		t.Fatalf("ready Receive: %v", err)
	}

	clk.Advance(100 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestEvictorSkipsAlreadyEvictedEntries(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory: func(_ context.Context, spec WorkerSpecRef) (Session, error) {
			return newMemSession(spec.ThreadID), nil
		},
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 100 * time.Millisecond},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "preset", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	if _, err := mgr.AcquirePersistent(ctx, spec); err != nil {
		t.Fatal(err)
	}

	mgr.mu.Lock()
	for _, e := range mgr.persistents {
		e.evicted = true
	}
	mgr.mu.Unlock()
	clk.Advance(200 * time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 1 {
		t.Errorf("rows = %d, want 1 (already-evicted should be skipped)", len(rows))
	}
}

func TestAcquireDuringEviction_PreservesReplacement(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "hang"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 50 * time.Millisecond},
		SessionStore:    store,
		EvictorInterval: 25 * time.Millisecond,

		SigtermGrace: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	spec := WorkerSpecRef{
		SpecID: "race-c1", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	e1, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire E1: %v", err)
	}

	readyCtx, readyCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readyCancel()
	if _, err := e1.Receive(readyCtx); err != nil {
		t.Fatalf("ready Receive: %v", err)
	}

	clk.Advance(100 * time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		entry, ok := mgr.persistents[persistentKey{spec.SpecID, spec.DoctrineName}]
		marked := ok && entry.evicted
		mgr.mu.Unlock()
		if marked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	e2, err := mgr.AcquirePersistent(ctx, spec)
	if err != nil {
		t.Fatalf("Acquire E2: %v", err)
	}
	if e1 == e2 {
		t.Fatal("Acquire returned same Session for evicted entry; want fresh spawn")
	}

	readyCtx2, readyCancel2 := context.WithTimeout(ctx, 2*time.Second)
	defer readyCancel2()
	if _, err := e2.Receive(readyCtx2); err != nil {
		t.Fatalf("E2 ready Receive: %v", err)
	}

	mgr.mu.Lock()
	entryE2, ok := mgr.persistents[persistentKey{spec.SpecID, spec.DoctrineName}]
	mgr.mu.Unlock()
	if !ok || entryE2.sess != e2 {
		t.Fatalf("registry does not map to E2 right after Acquire; ok=%v", ok)
	}

	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 1 {
		t.Fatalf("after Shutdown: rows = %d, want 1 (E2 must NOT have been erased by stale evictOne)", len(rows))
	}
	if rows[0].ThreadID != e2.ThreadID() {
		t.Errorf("retained row ThreadID = %q, want E2 = %q", rows[0].ThreadID, e2.ThreadID())
	}
}

func TestAcquireDuringScanCrashes_PreservesReplacement(t *testing.T) {
	store := newMemSessionStore()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))

	mgr, err := NewManager(ManagerOptions{
		Factory:         fakeFactory(t, "happy-path"),
		Clock:           clk,
		DoctrineTTLs:    staticTTL{"default": 10 * time.Second},
		SessionStore:    store,
		EvictorInterval: 50 * time.Millisecond,
		SigtermGrace:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	releaseClose := make(chan struct{})
	defer func() {

		select {
		case <-releaseClose:
		default:
			close(releaseClose)
		}
		_ = mgr.Shutdown(context.Background())
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	key := persistentKey{specID: "race-c1-scan", doctrineName: "default"}
	spec := WorkerSpecRef{
		SpecID: key.specID, Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: key.doctrineName,
		ThreadID: ThreadID("tid-c1-scan-e1"),
	}

	concreteE1 := &openClaudeSession{
		id:         spec.ThreadID,
		worktree:   spec.Worktree,
		sendCh:     make(chan Message, 1),
		recvCh:     make(chan Message, 1),
		closed:     make(chan struct{}),
		exitCh:     make(chan struct{}),
		closeGrace: 50 * time.Millisecond,
	}
	close(concreteE1.exitCh)
	e1Wrapped := &slowCloseSession{inner: concreteE1, gate: releaseClose}

	mgr.mu.Lock()
	mgr.persistents[key] = &persistentEntry{
		sess:      e1Wrapped,
		concrete:  concreteE1,
		specRef:   spec,
		startedAt: clk.Now(),
		lastUse:   clk.Now(),
		ttl:       10 * time.Second,
	}
	mgr.mu.Unlock()

	if err := store.UpsertPersistent(ctx, PersistentRow{
		SpecID: key.specID, DoctrineName: key.doctrineName,
		ThreadID: spec.ThreadID, Worktree: spec.Worktree,
		StartedAt: clk.Now(), LastUseAt: clk.Now(),
		TTLSeconds: 10,
	}); err != nil {
		t.Fatalf("seed UpsertPersistent: %v", err)
	}

	clk.Advance(60 * time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		entry, ok := mgr.persistents[key]
		marked := ok && entry.evicted
		mgr.mu.Unlock()
		if marked {
			break
		}
		time.Sleep(5 * time.Millisecond)
		clk.Advance(60 * time.Millisecond)
	}

	specE2 := spec
	specE2.ThreadID = ThreadID("tid-c1-scan-e2")
	e2, err := mgr.AcquirePersistent(ctx, specE2)
	if err != nil {
		t.Fatalf("Acquire E2: %v", err)
	}
	if e2 == e1Wrapped {
		t.Fatal("Acquire returned same crashed wrapper; want fresh E2")
	}

	mgr.mu.Lock()
	entryAfterAcquire, ok := mgr.persistents[key]
	mgr.mu.Unlock()
	if !ok || entryAfterAcquire.sess != e2 {
		t.Fatalf("registry does not map to E2 right after Acquire; ok=%v", ok)
	}

	close(releaseClose)

	time.Sleep(200 * time.Millisecond)

	mgr.mu.Lock()
	stillEntry, ok := mgr.persistents[key]
	mgr.mu.Unlock()
	if !ok || stillEntry.sess != e2 {
		t.Fatalf("after scanForCrashes trailing delete: registry erased E2 (ok=%v)", ok)
	}

	rows, _ := store.ListPersistent(ctx)
	if len(rows) != 1 {
		t.Fatalf("after scanForCrashes: rows = %d, want 1 (E2 row preserved)", len(rows))
	}
	if rows[0].ThreadID != e2.ThreadID() {
		t.Errorf("retained row ThreadID = %q, want E2 = %q", rows[0].ThreadID, e2.ThreadID())
	}
}

type slowCloseSession struct {
	inner *openClaudeSession
	gate  chan struct{}
}

func (s *slowCloseSession) ThreadID() ThreadID { return s.inner.ThreadID() }
func (s *slowCloseSession) Send(ctx context.Context, m Message) error {
	return s.inner.Send(ctx, m)
}
func (s *slowCloseSession) Receive(ctx context.Context) (Message, error) {
	return s.inner.Receive(ctx)
}
func (s *slowCloseSession) Close() error {
	<-s.gate

	s.inner.closeOnce.Do(func() { close(s.inner.closed) })
	return nil
}

func TestAcquirePersistentRebindCloseRace(t *testing.T) {
	store := newMemSessionStore()
	mgr, err := NewManager(ManagerOptions{
		Factory:      fakeFactory(t, "happy-path"),
		Clock:        realClock{},
		DoctrineTTLs: staticTTL{"default": time.Hour},
		SessionStore: store,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown(context.Background())
	spec := WorkerSpecRef{
		SpecID: "rcr", Variant: VariantTeamLead,
		Worktree: t.TempDir(), DoctrineName: "default",
	}
	const N = 8
	var wg sync.WaitGroup
	wg.Add(N)
	results := make(chan Session, N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			s, err := mgr.AcquirePersistent(context.Background(), spec)
			if err != nil {
				t.Errorf("AcquirePersistent: %v", err)
				return
			}
			results <- s
		}()
	}
	wg.Wait()
	close(results)
	first := <-results
	for s := range results {
		if s != first {
			t.Errorf("concurrent Acquires returned different Sessions; want all equal")
		}
	}
}
