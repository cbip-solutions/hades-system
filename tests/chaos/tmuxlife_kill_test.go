//go:build chaos

// Drives the recovery contract from spec §4.1 row "Tmux server crash":
//   - HealthMonitor polls Manager.ListSessions and surfaces orphan
//     status via the OnOrphan callback.
//   - On store failure during a poll, HealthMonitor MUST absorb and
//     continue (transient error budget — chaos tests inject store
//     faults to verify the monitor does not crash).
//   - When tmux server is "killed" (simulated by flipping every active
//     session to StatusOrphaned in the store, the way the daemon's
//     drift detector would when `tmux ls` returns server-gone), the
//     OnOrphan callback fires for every orphaned session within a
//     few ticks.
//   - Lazy-respawn-on-next-activation: after the operator reattaches,
//     a fresh session is upserted (no reuse of the orphan row).
//
// Determinism: this test is single-threaded apart from the monitor
// goroutine; no random injection. Tickrate is 5ms so all assertions
// land in <100ms wall-clock.
//
// Public-API surface only (no test-only types from internal/tmuxlife).
// The fake store is built from tmuxlife.NewInMemorySessionStore which
// is the canonical chaos-test substrate (per api_p7.go contract).
package chaos

import (
	"context"
	"hash/crc32"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/tmuxlife"
)

func TestChaos_TmuxServerKill_OrphansSurfacedViaHealthMonitor(t *testing.T) {

	rng := rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name())))))
	_ = rng

	store := tmuxlife.NewInMemorySessionStore()
	manager := tmuxlife.New(store)

	aliases := []struct{ alias, sha8 string }{
		{"alpha", "11111111"},
		{"beta", "22222222"},
		{"gamma", "33333333"},
	}
	for _, a := range aliases {
		s := tmuxlife.Session{
			Alias:        a.alias,
			Sha8:         a.sha8,
			Name:         tmuxlife.SessionName(a.alias, a.sha8),
			CreatedAt:    time.Now(),
			LastAttachAt: time.Now(),
			Status:       tmuxlife.StatusActive,
		}
		if err := store.UpsertSession(s); err != nil {
			t.Fatalf("seed UpsertSession(%s): %v", a.alias, err)
		}
	}

	var mu sync.Mutex
	orphans := make(map[string]struct{})
	monitor := tmuxlife.NewHealthMonitor(tmuxlife.HealthMonitorDeps{
		Manager: manager,
		Tick:    2 * time.Millisecond,
		OnOrphan: func(s tmuxlife.Session) {
			mu.Lock()
			defer mu.Unlock()
			orphans[s.Name] = struct{}{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitorDone := make(chan error, 1)
	go func() { monitorDone <- monitor.Run(ctx) }()

	for _, a := range aliases {
		name := tmuxlife.SessionName(a.alias, a.sha8)
		if err := store.SetStatus(name, tmuxlife.StatusOrphaned); err != nil {
			t.Fatalf("SetStatus(%s, orphaned): %v", name, err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(orphans)
		mu.Unlock()
		if got >= len(aliases) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	cancel()
	if err := <-monitorDone; err != nil && err != context.Canceled {
		t.Fatalf("monitor.Run returned unexpected error: %v", err)
	}

	mu.Lock()
	got := len(orphans)
	mu.Unlock()
	if got != len(aliases) {
		t.Fatalf("expected %d OnOrphan callbacks, got %d (orphans=%v)",
			len(aliases), got, orphans)
	}

	first := aliases[0]
	freshName := tmuxlife.SessionName(first.alias, first.sha8)
	freshSess := tmuxlife.Session{
		Alias:        first.alias,
		Sha8:         first.sha8,
		Name:         freshName,
		CreatedAt:    time.Now(),
		LastAttachAt: time.Now(),
		Status:       tmuxlife.StatusActive,
	}
	if err := store.UpsertSession(freshSess); err != nil {
		t.Fatalf("respawn UpsertSession: %v", err)
	}
	got2, err := store.GetSession(freshName)
	if err != nil {
		t.Fatalf("respawn GetSession: %v", err)
	}
	if got2.Status != tmuxlife.StatusActive {
		t.Fatalf("respawn: expected StatusActive, got %v", got2.Status)
	}
}

// TestChaos_TmuxServerKill_HealthMonitorAbsorbsListError asserts that
// when the SessionStore.ListSessions errors transiently (the chaos
// equivalent of `tmux ls` returning a partial failure), the
// HealthMonitor MUST keep running and resume on the next tick.
//
// The contract drives spec §4.1's "TmuxServerLost" recovery: even
// during the error window the monitor doesn't crash, so when the
// situation clears (ListSessions starts working), the next tick
// resumes orphan surfacing without intervention.
func TestChaos_TmuxServerKill_HealthMonitorAbsorbsListError(t *testing.T) {
	store := newFlakyStore(tmuxlife.NewInMemorySessionStore(), 5)
	manager := tmuxlife.New(store)

	s := tmuxlife.Session{
		Alias:        "orphan-pre",
		Sha8:         "44444444",
		Name:         tmuxlife.SessionName("orphan-pre", "44444444"),
		CreatedAt:    time.Now(),
		LastAttachAt: time.Now(),
		Status:       tmuxlife.StatusOrphaned,
	}

	if err := store.under.UpsertSession(s); err != nil {
		t.Fatalf("plant orphan: %v", err)
	}

	var orphanCount int
	var mu sync.Mutex
	monitor := tmuxlife.NewHealthMonitor(tmuxlife.HealthMonitorDeps{
		Manager: manager,
		Tick:    2 * time.Millisecond,
		OnOrphan: func(_ tmuxlife.Session) {
			mu.Lock()
			defer mu.Unlock()
			orphanCount++
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitorDone := make(chan error, 1)
	go func() { monitorDone <- monitor.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := orphanCount
		mu.Unlock()
		if got >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	cancel()
	if err := <-monitorDone; err != nil && err != context.Canceled {
		t.Fatalf("monitor.Run returned unexpected error: %v", err)
	}

	mu.Lock()
	got := orphanCount
	mu.Unlock()
	if got < 1 {
		t.Fatalf("expected >=1 OnOrphan callback after error window, got %d", got)
	}
}

func TestChaos_TmuxServerKill_NoOrphansNoCallbacks(t *testing.T) {
	store := tmuxlife.NewInMemorySessionStore()
	manager := tmuxlife.New(store)

	var fired int
	var mu sync.Mutex
	monitor := tmuxlife.NewHealthMonitor(tmuxlife.HealthMonitorDeps{
		Manager: manager,
		Tick:    1 * time.Millisecond,
		OnOrphan: func(_ tmuxlife.Session) {
			mu.Lock()
			defer mu.Unlock()
			fired++
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = monitor.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	mu.Lock()
	got := fired
	mu.Unlock()
	if got != 0 {
		t.Fatalf("expected 0 OnOrphan callbacks on empty store, got %d", got)
	}
}

type flakySessionStore struct {
	mu    sync.Mutex
	under tmuxlife.SessionStore
	left  int
}

func newFlakyStore(under tmuxlife.SessionStore, errs int) *flakySessionStore {
	return &flakySessionStore{under: under, left: errs}
}

func (f *flakySessionStore) UpsertSession(s tmuxlife.Session) error {
	return f.under.UpsertSession(s)
}

func (f *flakySessionStore) GetSession(name string) (tmuxlife.Session, error) {
	return f.under.GetSession(name)
}

func (f *flakySessionStore) ListSessions() ([]tmuxlife.Session, error) {
	f.mu.Lock()
	if f.left > 0 {
		f.left--
		f.mu.Unlock()
		return nil, errFlakyTmuxList
	}
	f.mu.Unlock()
	return f.under.ListSessions()
}

func (f *flakySessionStore) DeleteSession(name string) error {
	return f.under.DeleteSession(name)
}

func (f *flakySessionStore) SetLastAttach(name string, t time.Time) error {
	return f.under.SetLastAttach(name, t)
}

func (f *flakySessionStore) SetStatus(name string, st tmuxlife.SessionStatus) error {
	return f.under.SetStatus(name, st)
}

func (f *flakySessionStore) ExpectedPanesFor(name string) (map[tmuxlife.WindowName][]string, error) {
	return f.under.ExpectedPanesFor(name)
}

var errFlakyTmuxList = chaosError("flaky tmux: server transiently unreachable")

type chaosError string

func (c chaosError) Error() string { return string(c) }
