package tmuxlife

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

func TestSessionStateAlias(t *testing.T) {
	var s SessionState = Session{Alias: "internal-platform-x", Status: StatusActive}
	if s.Alias != "internal-platform-x" {
		t.Errorf("SessionState alias broken: got %q", s.Alias)
	}

	var sessions []Session = []SessionState{s}
	var states []SessionState = []Session{s}
	if len(sessions) != len(states) {
		t.Errorf("slice length mismatch %d vs %d", len(sessions), len(states))
	}
}

func TestStatusAliases(t *testing.T) {
	if StatusRunning != StatusActive {
		t.Errorf("StatusRunning=%v, want StatusActive=%v", StatusRunning, StatusActive)
	}
	if StatusOrphan != StatusOrphaned {
		t.Errorf("StatusOrphan=%v, want StatusOrphaned=%v", StatusOrphan, StatusOrphaned)
	}

	got := StatusRunning
	if got != StatusActive {
		t.Errorf("StatusRunning roundtrip drifted to %v", got)
	}
}

func TestNewManagerAlias(t *testing.T) {
	store := NewInMemorySessionStore()
	m1 := New(store)
	m2 := NewManager(store)
	if m1.store != m2.store {
		t.Errorf("NewManager and New should produce Managers with the same store reference")
	}
}

func TestNewInMemorySessionStoreRoundTrip(t *testing.T) {
	store := NewInMemorySessionStore()
	if err := store.UpsertSession(Session{
		Alias:  "x",
		Sha8:   "12345678",
		Name:   "zen-x-12345678",
		Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession("zen-x-12345678")
	if err != nil {
		t.Fatal(err)
	}
	if got.Alias != "x" {
		t.Errorf("GetSession alias=%q, want x", got.Alias)
	}
}

func TestNewInMemorySessionStoreFullSurface(t *testing.T) {
	store := NewInMemorySessionStore()
	s := Session{Alias: "a", Sha8: "11111111", Name: "zen-a-11111111", Status: StatusActive}
	if err := store.UpsertSession(s); err != nil {
		t.Fatal(err)
	}

	all, err := store.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Name != "zen-a-11111111" {
		t.Errorf("ListSessions=%v", all)
	}

	now := time.Now().UTC()
	if err := store.SetLastAttach("zen-a-11111111", now); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetSession("zen-a-11111111")
	if !got.LastAttachAt.Equal(now) {
		t.Errorf("SetLastAttach did not persist; got %v want %v", got.LastAttachAt, now)
	}

	if err := store.SetStatus("zen-a-11111111", StatusOrphaned); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetSession("zen-a-11111111")
	if got.Status != StatusOrphaned {
		t.Errorf("SetStatus did not persist; got %v want StatusOrphaned", got.Status)
	}

	panes, err := store.ExpectedPanesFor("zen-a-11111111")
	if err != nil {
		t.Fatalf("ExpectedPanesFor=%v", err)
	}
	if len(panes) != 0 {
		t.Errorf("ExpectedPanesFor for fresh session=%v, want empty", panes)
	}

	if err := store.DeleteSession("zen-a-11111111"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSession("zen-a-11111111"); err == nil {
		t.Errorf("expected ErrSessionNotFound after delete")
	}
}

func TestNewInMemorySessionStoreNotFoundSentinels(t *testing.T) {
	store := NewInMemorySessionStore()
	if _, err := store.GetSession("zen-missing-00000000"); err != ErrSessionNotFound {
		t.Errorf("GetSession for missing name=%v, want ErrSessionNotFound", err)
	}
	if err := store.SetLastAttach("zen-missing-00000000", time.Now().UTC()); err != ErrSessionNotFound {
		t.Errorf("SetLastAttach for missing name=%v, want ErrSessionNotFound", err)
	}
	if err := store.SetStatus("zen-missing-00000000", StatusActive); err != ErrSessionNotFound {
		t.Errorf("SetStatus for missing name=%v, want ErrSessionNotFound", err)
	}
}

func TestRepaintLayoutResultShape(t *testing.T) {
	store := NewInMemorySessionStore()
	if err := store.UpsertSession(Session{
		Alias:  "x",
		Sha8:   "12345678",
		Name:   "zen-x-12345678",
		Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	m.exec = func(ctx context.Context, args ...string) ([]byte, error) {

		return []byte("orch\nleads\nworkers\nhra\nlogs\nscratch\n"), nil
	}
	res, err := m.RepaintLayout(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if !res.ScratchPreserved {
		t.Errorf("ScratchPreserved=false, want true (inv-zen-118)")
	}
	if res.SessionName != "zen-x-12345678" {
		t.Errorf("SessionName=%q, want zen-x-12345678", res.SessionName)
	}
}

func TestRepaintLayoutMissingAlias(t *testing.T) {
	store := NewInMemorySessionStore()
	m := New(store)
	_, err := m.RepaintLayout(context.Background(), "ghost")
	if err != ErrSessionNotFound {
		t.Errorf("RepaintLayout for unknown alias=%v, want ErrSessionNotFound", err)
	}
}

func TestListSessionsAlias(t *testing.T) {
	store := NewInMemorySessionStore()
	if err := store.UpsertSession(Session{
		Alias:  "a",
		Sha8:   "11111111",
		Name:   "zen-a-11111111",
		Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	got1, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got2, err := m.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got1) != len(got2) {
		t.Errorf("ListSessions=%d, List=%d (must match)", len(got2), len(got1))
	}
	if got1[0].Name != got2[0].Name {
		t.Errorf("ListSessions[0].Name=%q, List[0].Name=%q", got2[0].Name, got1[0].Name)
	}
}

func TestActivateConvenienceWrapper(t *testing.T) {
	store := NewInMemorySessionStore()
	deps := ActivateDeps{
		Store:    store,
		Doctrine: doctrine.NameDefault,
		Now:      func() time.Time { return time.Now() },
	}
	_, err := Activate(deps, "x", "12345678")

	_ = err
}

func TestActivateFromSpec(t *testing.T) {
	store := NewInMemorySessionStore()
	m := New(store)
	calls := 0
	m.exec = func(ctx context.Context, args ...string) ([]byte, error) {
		calls++

		for _, a := range args {
			if a == "has-session" {
				return nil, errCantFindSession{}
			}
		}
		return []byte{}, nil
	}
	spec := SessionSpec{
		Alias:   "y",
		Sha8:    "22222222",
		Windows: DaemonOwnedWindows,
	}
	s, err := m.ActivateFromSpec(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Alias != "y" {
		t.Errorf("ActivateFromSpec returned %v, want session with alias=y", s)
	}

	if calls < 2 {
		t.Errorf("expected at least 2 exec calls (Spawn + CreateWindows), got %d", calls)
	}
}

type errCantFindSession struct{}

func (errCantFindSession) Error() string { return "can't find session" }

func TestNewHealthMonitorDefaultsTick(t *testing.T) {
	h := NewHealthMonitor(HealthMonitorDeps{
		Manager: New(NewInMemorySessionStore()),
	})
	if h.deps.Tick != 5*time.Second {
		t.Errorf("default Tick=%v, want 5s", h.deps.Tick)
	}
}

func TestHealthMonitorRunCancellation(t *testing.T) {
	store := NewInMemorySessionStore()
	m := New(store)
	h := NewHealthMonitor(HealthMonitorDeps{
		Manager: m,
		Tick:    1 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Errorf("Run returned nil, want context.Canceled")
		}
	case <-time.After(30 * time.Second):
		t.Errorf("Run did not return within 30s after cancellation")
	}
}

func TestHealthMonitorOrphanCallback(t *testing.T) {
	store := NewInMemorySessionStore()
	if err := store.UpsertSession(Session{
		Alias:  "ghost",
		Sha8:   "deadbeef",
		Name:   "zen-ghost-deadbeef",
		Status: StatusOrphan,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	gotOrphan := make(chan Session, 1)
	h := NewHealthMonitor(HealthMonitorDeps{
		Manager: m,
		Tick:    5 * time.Millisecond,
		OnOrphan: func(s Session) {
			select {
			case gotOrphan <- s:
			default:
			}
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = h.Run(ctx) }()
	select {
	case s := <-gotOrphan:
		if s.Name != "zen-ghost-deadbeef" {
			t.Errorf("OnOrphan got Name=%q, want zen-ghost-deadbeef", s.Name)
		}
	case <-time.After(30 * time.Second):
		t.Errorf("OnOrphan callback did not fire within 30s")
	}
}

func TestRepaintResultShape(t *testing.T) {
	r := RepaintResult{
		SessionName:      "zen-a-11111111",
		WindowsRepainted: []string{"orch"},
		ScratchPreserved: true,
		Duration:         42 * time.Millisecond,
	}
	if r.SessionName == "" || r.Duration == 0 {
		t.Errorf("RepaintResult missing required fields")
	}
}

func TestActivateFromSpecSpawnError(t *testing.T) {
	store := NewInMemorySessionStore()
	m := New(store)
	m.exec = func(ctx context.Context, args ...string) ([]byte, error) {

		for _, a := range args {
			if a == "has-session" {
				return nil, errCantFindSession{}
			}
			if a == "new-session" {
				return nil, errSpawnFailed{}
			}
		}
		return []byte{}, nil
	}
	_, err := m.ActivateFromSpec(context.Background(), SessionSpec{Alias: "z", Sha8: "33333333"})
	if err == nil {
		t.Errorf("expected error from ActivateFromSpec when Spawn fails")
	}
}

type errSpawnFailed struct{}

func (errSpawnFailed) Error() string { return "out of file descriptors" }

func TestActivateFromSpecCreateWindowsError(t *testing.T) {
	store := NewInMemorySessionStore()
	m := New(store)
	m.exec = func(ctx context.Context, args ...string) ([]byte, error) {
		for _, a := range args {
			if a == "has-session" {
				return nil, errCantFindSession{}
			}
			if a == "rename-window" {
				return nil, errSpawnFailed{}
			}
		}
		return []byte{}, nil
	}
	_, err := m.ActivateFromSpec(context.Background(), SessionSpec{Alias: "w", Sha8: "44444444"})
	if err == nil {
		t.Errorf("expected error from ActivateFromSpec when CreateWindows fails")
	}
}

func TestRepaintLayoutCreateWindowsError(t *testing.T) {
	store := NewInMemorySessionStore()
	if err := store.UpsertSession(Session{
		Alias:  "v",
		Sha8:   "55555555",
		Name:   "zen-v-55555555",
		Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(store)
	m.exec = func(ctx context.Context, args ...string) ([]byte, error) {

		for _, a := range args {
			if a == "rename-window" {
				return nil, errSpawnFailed{}
			}
		}
		return []byte("orch\n"), nil
	}
	_, err := m.RepaintLayout(context.Background(), "v")
	if err == nil {
		t.Errorf("expected RepaintLayout to surface CreateWindows error")
	}
}

func TestHealthMonitorRunListSessionsError(t *testing.T) {
	failingStore := &failingListStore{}
	m := New(failingStore)
	h := NewHealthMonitor(HealthMonitorDeps{
		Manager: m,
		Tick:    1 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:

	case <-time.After(30 * time.Second):
		t.Errorf("Run did not return within 30s after cancellation")
	}
}

type failingListStore struct{}

func (failingListStore) UpsertSession(Session) error { return nil }
func (failingListStore) GetSession(string) (Session, error) {
	return Session{}, ErrSessionNotFound
}
func (failingListStore) ListSessions() ([]Session, error) { return nil, errSpawnFailed{} }
func (failingListStore) DeleteSession(string) error       { return nil }
func (failingListStore) SetLastAttach(string, time.Time) error {
	return ErrSessionNotFound
}
func (failingListStore) SetStatus(string, SessionStatus) error { return ErrSessionNotFound }
func (failingListStore) ExpectedPanesFor(string) (map[WindowName][]string, error) {
	return nil, nil
}

func TestSplitWindowsEmpty(t *testing.T) {
	got := splitWindows([]byte{})
	if got == nil {
		t.Errorf("splitWindows returned nil; want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("splitWindows([]byte{})=%v, want []string{}", got)
	}
}

func TestSplitWindowsTrailingNewline(t *testing.T) {
	got := splitWindows([]byte("orch\nleads\n"))
	if len(got) != 2 || got[0] != "orch" || got[1] != "leads" {
		t.Errorf("splitWindows=%v, want [orch leads]", got)
	}
}

func TestWindowDiffNewWindows(t *testing.T) {
	pre := []string{"orch"}
	post := []string{"orch", "leads", "workers"}
	got := windowDiff(pre, post)
	if len(got) != 2 || got[0] != "leads" || got[1] != "workers" {
		t.Errorf("windowDiff=%v, want [leads workers]", got)
	}
}

func TestWindowDiffNoNew(t *testing.T) {
	pre := []string{"orch", "leads", "workers", "hra", "logs", "scratch"}
	post := []string{"orch", "leads", "workers", "hra", "logs", "scratch"}
	got := windowDiff(pre, post)
	if len(got) != 0 {
		t.Errorf("windowDiff with identical pre/post=%v, want []", got)
	}
}
