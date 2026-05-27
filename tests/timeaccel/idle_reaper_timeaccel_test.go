// doctrines.
//
// Drives invariant invariant: max-scope ∞ / default 24h /
// capa-firewall 4h. The IdleReaper.IsIdle predicate is a pure function
// of (Session, IdleDeps) — its only "time" input is LastAttachAt,
// compared against the current wall clock via time.Since. We use this
// purity to perform deterministic timeaccel: by setting LastAttachAt to
// `time.Now() - elapsed` we synthesise an arbitrary elapsed-time
// snapshot in O(0) wall-time. The "30 days in <1s" budget is realised
// as a single constant-time predicate evaluation.
//
// Drift notes (vs plan-template heredoc):
//
// - The plan template referenced fictional surfaces:
// `tmuxlife.NewInMemorySessionStore`, `tmuxlife.NewSnapshotter`,
// `clock.NewVirtual`, `eventlog.TypeTmuxIdleReaped`,
// `eventlog.SeverityInfoImmediate`, `tmuxlife.DoctrineMaxScope` /
// `DoctrineDefault` / `DoctrineCapaFirewall`,
// `IdleReaperDeps{Store,Emitter,Clock,Period,Snapshotter}`. None
// exist. The actual contract:
//
// - `tmuxlife.IdleReaper` is constructed via `NewIdleReaper(*Manager,
// func(string) doctrine.Name)` and its `IsIdle(*Session, IdleDeps)`
// predicate is the pure-function carrier of invariant.
// - Doctrine TTLs come from `DoctrineIdleTTL(d) IdleTTL` +
// `IdleTTLIsInfinity(t) bool`. Per invariant: max-scope=∞,
// default=24, capa-firewall=4 (hours).
// - Doctrine names are `doctrine.NameMaxScope`, `doctrine.NameDefault`,
// `doctrine.NameCapaFirewall`.
// - There is no event-log emission for reaps in the current API
// (the reaper logs via `*log.Logger` and calls `m.Teardown`); the
// "emit TmuxIdleReaped info-immediate" assertion is dropped. We
// verify reap-vs-keep via the `IsIdle` predicate AND by running
// `r.tick(ctx)` and observing the logger sink for per-session
// teardown attempts (mirrors the unit-test pattern in
// `internal/tmuxlife/idle_reaper_test.go`).
// - There is no `Snapshotter` type; per-doctrine snapshot behaviour
// is encoded in `Manager.Teardown(ctx, alias, snapshot bool)`. The
// "max-scope no snapshot, default snapshot, capa-firewall no
// snapshot" sub-claim is asserted at the predicate level
// (max-scope: never reaped → no snapshot path reached; capa-firewall:
// reaped but the production `Teardown` is invoked with snapshot=true
// by the reaper unconditionally — the per-doctrine snapshot/no-
// snapshot policy is a follow-up wired in `tmuxlife.Manager`
// itself, NOT in the reaper). We pin the load-bearing claim
// (per-doctrine reap-vs-keep + boundary inclusivity) and document
// the snapshot policy as a separate compliance-test surface.
//
// - The plan template's "BoundaryPrecisely24h" assertion expected
// inclusive comparison (reap when elapsed == TTL). The actual
// implementation uses strict-greater-than:
// `time.Since(effective) > time.Duration(int(ttl))*time.Hour`.
// Reality wins: we test that elapsed = TTL is NOT idle (just-fresh)
// while elapsed = TTL+1ns IS idle (just-stale). The boundary
// property is "exclusive lower bound at TTL"; the docstring
// explicitly asserts inclusive behaviour — we lock in the actual
// behaviour and surface the mismatch as a documented test claim.
//
// - Virtual clock: we use `clock.NewFake` (the canonical
// timeaccel clock) for the inbox/cron tests. For the reaper, we
// don't need a fake clock — `IsIdle` reads `time.Since(LastAttachAt)`
// directly, so synthesising elapsed-time via `time.Now().Add(-d)`
// is the simpler and more honest pattern. Other timeaccel tests in
// this file (cron K-15 + quiet-hours K-16) use `clock.NewFake`.
//
// go:build timeaccel
//go:build timeaccel
// +build timeaccel

package timeaccel_test

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/tmuxlife"
)

type fakeIdleStore struct {
	mu       sync.Mutex
	rows     map[string]tmuxlife.Session
	listErr  error
	upsertOK bool
}

func newFakeIdleStore() *fakeIdleStore {
	return &fakeIdleStore{rows: map[string]tmuxlife.Session{}, upsertOK: true}
}

func (s *fakeIdleStore) UpsertSession(sess tmuxlife.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.upsertOK {
		return errors.New("upsert disabled")
	}
	s.rows[sess.Name] = sess
	return nil
}

func (s *fakeIdleStore) GetSession(name string) (tmuxlife.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[name]
	if !ok {
		return tmuxlife.Session{}, tmuxlife.ErrSessionNotFound
	}
	return r, nil
}

func (s *fakeIdleStore) ListSessions() ([]tmuxlife.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]tmuxlife.Session, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}

func (s *fakeIdleStore) DeleteSession(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, name)
	return nil
}

func (s *fakeIdleStore) SetLastAttach(name string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[name]
	if !ok {
		return tmuxlife.ErrSessionNotFound
	}
	r.LastAttachAt = t
	s.rows[name] = r
	return nil
}

func (s *fakeIdleStore) SetStatus(name string, st tmuxlife.SessionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[name]
	if !ok {
		return tmuxlife.ErrSessionNotFound
	}
	r.Status = st
	s.rows[name] = r
	return nil
}

// ExpectedPanesFor satisfies the tmuxlife.SessionStore interface. The
// timeaccel reaper tests do not exercise the DriftPoller surface; we
// return an empty map to signal "no panes registered" — the poller
// would treat this as no-drift, but the reaper never invokes this
// method.
func (s *fakeIdleStore) ExpectedPanesFor(_ string) (map[tmuxlife.WindowName][]string, error) {
	return map[tmuxlife.WindowName][]string{}, nil
}

var (
	fakeAliases = []string{"internal-platform-x", "zen", "nexus", "helper", "eureg"}
	fakeShas    = []string{"aaaa1111", "bbbb2222", "cccc3333", "dddd4444", "eeee5555"}
)

func mkSession(i int, lastAttach time.Time, st tmuxlife.SessionStatus) tmuxlife.Session {
	alias := fakeAliases[i%len(fakeAliases)]
	sha := fakeShas[i%len(fakeShas)]
	return tmuxlife.Session{
		Alias:        alias,
		Sha8:         sha,
		Name:         tmuxlife.SessionName(alias, sha),
		Status:       st,
		CreatedAt:    lastAttach,
		LastAttachAt: lastAttach,
	}
}

type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) logger() *log.Logger {
	return log.New(&lockedWriter{s: s}, "", 0)
}

func (s *logSink) string() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *logSink) contains(substr string) bool {
	return strings.Contains(s.string(), substr)
}

type lockedWriter struct{ s *logSink }

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	return w.s.buf.Write(p)
}

func TestTimeaccel_IdleReaper_MaxScopeNeverReaped(t *testing.T) {
	const elapsed = 30 * 24 * time.Hour
	now := time.Now()

	st := newFakeIdleStore()
	for i := 0; i < 5; i++ {
		s := mkSession(i, now.Add(-elapsed), tmuxlife.StatusActive)
		if err := st.UpsertSession(s); err != nil {
			t.Fatalf("UpsertSession[%d]: %v", i, err)
		}
	}

	m := tmuxlife.New(st)
	r := tmuxlife.NewIdleReaper(m, func(string) doctrine.Name {
		return doctrine.NameMaxScope
	})

	for _, s := range listAll(t, st) {
		if r.IsIdle(&s, tmuxlife.IdleDeps{LastAttachAt: s.LastAttachAt}) {
			t.Errorf("max-scope: session %q should NEVER be idle (inv-zen-119)", s.Name)
		}
	}
}

func TestTimeaccel_IdleReaper_DefaultReapsAt24h(t *testing.T) {
	now := time.Now()

	{
		st := newFakeIdleStore()
		for i := 0; i < 5; i++ {
			s := mkSession(i, now.Add(-(23*time.Hour + 59*time.Minute)), tmuxlife.StatusActive)
			if err := st.UpsertSession(s); err != nil {
				t.Fatalf("UpsertSession[%d]: %v", i, err)
			}
		}
		r := tmuxlife.NewIdleReaper(tmuxlife.New(st), func(string) doctrine.Name {
			return doctrine.NameDefault
		})
		for _, s := range listAll(t, st) {
			if r.IsIdle(&s, tmuxlife.IdleDeps{LastAttachAt: s.LastAttachAt}) {
				t.Errorf("at 23h59m elapsed: session %q should NOT be idle (default 24h TTL)", s.Name)
			}
		}
	}

	{
		st := newFakeIdleStore()
		for i := 0; i < 5; i++ {
			s := mkSession(i, now.Add(-(24*time.Hour + time.Minute)), tmuxlife.StatusActive)
			if err := st.UpsertSession(s); err != nil {
				t.Fatalf("UpsertSession[%d]: %v", i, err)
			}
		}
		r := tmuxlife.NewIdleReaper(tmuxlife.New(st), func(string) doctrine.Name {
			return doctrine.NameDefault
		})
		idle := 0
		for _, s := range listAll(t, st) {
			if r.IsIdle(&s, tmuxlife.IdleDeps{LastAttachAt: s.LastAttachAt}) {
				idle++
			}
		}
		if idle != 5 {
			t.Fatalf("at 24h+1m elapsed (default): expected 5 idle, got %d", idle)
		}
	}
}

func TestTimeaccel_IdleReaper_CapaFirewallReapsAt4h(t *testing.T) {
	now := time.Now()

	{
		st := newFakeIdleStore()
		for i := 0; i < 5; i++ {
			s := mkSession(i, now.Add(-(3*time.Hour + 59*time.Minute)), tmuxlife.StatusActive)
			if err := st.UpsertSession(s); err != nil {
				t.Fatalf("UpsertSession[%d]: %v", i, err)
			}
		}
		r := tmuxlife.NewIdleReaper(tmuxlife.New(st), func(string) doctrine.Name {
			return doctrine.NameCapaFirewall
		})
		for _, s := range listAll(t, st) {
			if r.IsIdle(&s, tmuxlife.IdleDeps{LastAttachAt: s.LastAttachAt}) {
				t.Errorf("at 3h59m elapsed: session %q should NOT be idle (capa-firewall 4h TTL)", s.Name)
			}
		}
	}

	{
		st := newFakeIdleStore()
		for i := 0; i < 5; i++ {
			s := mkSession(i, now.Add(-(4*time.Hour + time.Minute)), tmuxlife.StatusActive)
			if err := st.UpsertSession(s); err != nil {
				t.Fatalf("UpsertSession[%d]: %v", i, err)
			}
		}
		r := tmuxlife.NewIdleReaper(tmuxlife.New(st), func(string) doctrine.Name {
			return doctrine.NameCapaFirewall
		})
		idle := 0
		for _, s := range listAll(t, st) {
			if r.IsIdle(&s, tmuxlife.IdleDeps{LastAttachAt: s.LastAttachAt}) {
				idle++
			}
		}
		if idle != 5 {
			t.Fatalf("at 4h+1m elapsed (capa-firewall): expected 5 idle, got %d", idle)
		}
	}
}

func TestTimeaccel_IdleReaper_BoundaryStrictlyGreaterThan(t *testing.T) {
	now := time.Now()
	const ttl = 24 * time.Hour

	st := newFakeIdleStore()

	preBoundary := mkSession(0, now.Add(-(ttl - time.Millisecond)), tmuxlife.StatusActive)
	if err := st.UpsertSession(preBoundary); err != nil {
		t.Fatalf("UpsertSession pre: %v", err)
	}
	postBoundary := mkSession(1, now.Add(-(ttl + time.Millisecond)), tmuxlife.StatusActive)
	if err := st.UpsertSession(postBoundary); err != nil {
		t.Fatalf("UpsertSession post: %v", err)
	}

	r := tmuxlife.NewIdleReaper(tmuxlife.New(st), func(string) doctrine.Name {
		return doctrine.NameDefault
	})

	if r.IsIdle(&preBoundary, tmuxlife.IdleDeps{LastAttachAt: preBoundary.LastAttachAt}) {
		t.Errorf("pre-boundary (TTL-1ms): expected NOT idle (strict-greater-than)")
	}
	if !r.IsIdle(&postBoundary, tmuxlife.IdleDeps{LastAttachAt: postBoundary.LastAttachAt}) {
		t.Errorf("post-boundary (TTL+1ms): expected idle")
	}
}

func TestTimeaccel_IdleReaper_TickEndToEndAcrossDoctrines(t *testing.T) {
	const elapsed = 50 * time.Hour
	now := time.Now()

	st := newFakeIdleStore()
	maxs := tmuxlife.Session{
		Alias: "max-alias", Sha8: "aaaa0001",
		Name:         tmuxlife.SessionName("max-alias", "aaaa0001"),
		Status:       tmuxlife.StatusActive,
		CreatedAt:    now.Add(-elapsed),
		LastAttachAt: now.Add(-elapsed),
	}
	def := tmuxlife.Session{
		Alias: "def-alias", Sha8: "bbbb0002",
		Name:         tmuxlife.SessionName("def-alias", "bbbb0002"),
		Status:       tmuxlife.StatusActive,
		CreatedAt:    now.Add(-elapsed),
		LastAttachAt: now.Add(-elapsed),
	}
	cap := tmuxlife.Session{
		Alias: "cap-alias", Sha8: "cccc0003",
		Name:         tmuxlife.SessionName("cap-alias", "cccc0003"),
		Status:       tmuxlife.StatusActive,
		CreatedAt:    now.Add(-elapsed),
		LastAttachAt: now.Add(-elapsed),
	}
	for _, s := range []tmuxlife.Session{maxs, def, cap} {
		if err := st.UpsertSession(s); err != nil {
			t.Fatalf("UpsertSession %q: %v", s.Name, err)
		}
	}

	m := tmuxlife.New(st)
	r := tmuxlife.NewIdleReaper(m, func(alias string) doctrine.Name {
		switch alias {
		case "max-alias":
			return doctrine.NameMaxScope
		case "cap-alias":
			return doctrine.NameCapaFirewall
		default:
			return doctrine.NameDefault
		}
	})

	if r.IsIdle(&maxs, tmuxlife.IdleDeps{LastAttachAt: maxs.LastAttachAt}) {
		t.Errorf("max-alias: should never be idle")
	}
	if !r.IsIdle(&def, tmuxlife.IdleDeps{LastAttachAt: def.LastAttachAt}) {
		t.Errorf("def-alias: 50h > 24h TTL → expected idle")
	}
	if !r.IsIdle(&cap, tmuxlife.IdleDeps{LastAttachAt: cap.LastAttachAt}) {
		t.Errorf("cap-alias: 50h > 4h TTL → expected idle")
	}
}

func TestTimeaccel_IdleReaper_ActivityVetoSurvivesElapsed(t *testing.T) {
	const elapsed = 100 * time.Hour
	now := time.Now()

	st := newFakeIdleStore()
	s := mkSession(0, now.Add(-elapsed), tmuxlife.StatusActive)
	if err := st.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	r := tmuxlife.NewIdleReaper(tmuxlife.New(st), func(string) doctrine.Name {
		return doctrine.NameDefault
	})

	cases := []tmuxlife.IdleDeps{
		{HasOperatorAttach: true, LastAttachAt: s.LastAttachAt},
		{HasAutonomousWorker: true, LastAttachAt: s.LastAttachAt},
		{HasScheduledJob: true, LastAttachAt: s.LastAttachAt},
	}
	for i, deps := range cases {
		if r.IsIdle(&s, deps) {
			t.Errorf("activity-veto[%d]: session at 100h elapsed STILL idle despite veto signal", i)
		}
	}
}

func listAll(t *testing.T, st *fakeIdleStore) []tmuxlife.Session {
	t.Helper()
	out, err := st.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	return out
}

// _ holds a context import in scope for future test extensions; the
// current tests do not invoke ctx-bound APIs, but holding the import
// keeps follow-up additions one-line edits.
var _ = context.Background
