package orchestrator

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePinStore struct {
	mu       sync.Mutex
	rows     map[string]PinRow
	purgeErr error
	purges   int32
	queryErr error
}

func newFakePinStore() *fakePinStore {
	return &fakePinStore{rows: map[string]PinRow{}}
}

func (f *fakePinStore) key(scope, id string) string { return scope + "\x00" + id }

func (f *fakePinStore) Insert(p PinRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[f.key(p.Scope, p.ScopeID)] = p
	return nil
}

func (f *fakePinStore) Delete(scope, scopeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, f.key(scope, scopeID))
	return nil
}

func (f *fakePinStore) Query(scope, scopeID string) (*PinRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	r, ok := f.rows[f.key(scope, scopeID)]
	if !ok {
		return nil, nil
	}
	cp := r
	return &cp, nil
}

func (f *fakePinStore) ListAll() ([]PinRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PinRow, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakePinStore) PurgeExpired(now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	atomic.AddInt32(&f.purges, 1)
	if f.purgeErr != nil {
		return 0, f.purgeErr
	}
	purged := 0
	for k, r := range f.rows {
		if r.ExpiresAt != nil && r.ExpiresAt.Before(now) {
			delete(f.rows, k)
			purged++
		}
	}
	return purged, nil
}

func TestPinOverridesSetPermanent(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	if err := p.Set("session", "sess-1", "tier_inhouse", "anthropic", 0, "ad hoc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := p.Resolve("sess-1", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil || got.Tier != "tier_inhouse" || got.ExpiresAt != nil {
		t.Errorf("got=%+v, want tier=tier_inhouse permanent", got)
	}
}

func TestPinOverridesSetWithTTL(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	if err := p.Set("session", "sess-2", "tier_openclaude", "", time.Hour, ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, _ := p.Resolve("sess-2", "")
	if got == nil {
		t.Fatal("expected pin")
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt expected non-nil for TTL pin")
	}
	if delta := time.Until(*got.ExpiresAt); delta < 30*time.Minute || delta > 90*time.Minute {
		t.Errorf("ExpiresAt = %v (delta %v), want ~1h", got.ExpiresAt, delta)
	}
}

func TestPinOverridesResolveHierarchySessionWins(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	_ = p.Set("global", "", "GLOBAL", "", 0, "")
	_ = p.Set("project", "internal-platform-x", "PROJECT", "", 0, "")
	_ = p.Set("session", "sess-A", "SESSION", "", 0, "")

	got, err := p.Resolve("sess-A", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil || got.Tier != "SESSION" {
		t.Errorf("got=%+v, want tier=SESSION (session > project > global)", got)
	}
}

func TestPinOverridesResolveHierarchyProjectFallback(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	_ = p.Set("global", "", "GLOBAL", "", 0, "")
	_ = p.Set("project", "internal-platform-x", "PROJECT", "", 0, "")

	got, _ := p.Resolve("sess-without-pin", "internal-platform-x")
	if got == nil || got.Tier != "PROJECT" {
		t.Errorf("got=%+v, want PROJECT fallback", got)
	}
}

func TestPinOverridesResolveHierarchyGlobalFallback(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	_ = p.Set("global", "", "GLOBAL", "", 0, "")

	got, _ := p.Resolve("anysess", "anyproject")
	if got == nil || got.Tier != "GLOBAL" {
		t.Errorf("got=%+v, want GLOBAL fallback", got)
	}
}

func TestPinOverridesResolveNoPinReturnsNil(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	got, err := p.Resolve("s", "p")
	if err != nil {
		t.Errorf("Resolve: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestPinOverridesResolveSkipsExpired(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)

	past := time.Now().Add(-time.Hour)
	_ = s.Insert(PinRow{
		Scope: "session", ScopeID: "stale", Tier: "tier_inhouse",
		SetAt: time.Now().Add(-2 * time.Hour), ExpiresAt: &past,
	})

	got, err := p.Resolve("stale", "")
	if err != nil {
		t.Errorf("Resolve: %v", err)
	}
	if got != nil {
		t.Errorf("expected expired pin filtered out, got %+v", got)
	}
}

func TestPinOverridesUnsetIdempotent(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	_ = p.Set("session", "sess-1", "tier_inhouse", "", 0, "")
	if err := p.Unset("session", "sess-1"); err != nil {
		t.Fatalf("Unset: %v", err)
	}
	if err := p.Unset("session", "sess-1"); err != nil {
		t.Errorf("second Unset should be idempotent, got %v", err)
	}
}

func TestPinOverridesListAllSkipsExpired(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	_ = s.Insert(PinRow{Scope: "session", ScopeID: "alive", Tier: "tier_inhouse", SetAt: time.Now(), ExpiresAt: &future})
	_ = s.Insert(PinRow{Scope: "session", ScopeID: "stale", Tier: "tier_inhouse", SetAt: time.Now(), ExpiresAt: &past})
	_ = s.Insert(PinRow{Scope: "global", ScopeID: "", Tier: "tier_inhouse", SetAt: time.Now()})

	out, err := p.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("len=%d, want 2 (expired filtered)", len(out))
	}
	for _, o := range out {
		if o.ScopeID == "stale" {
			t.Errorf("expired pin leaked in ListAll: %+v", o)
		}
	}
}

func TestPinOverridesUnpinAll(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	_ = p.Set("session", "s1", "tier_inhouse", "", 0, "")
	_ = p.Set("project", "p1", "tier_inhouse", "", 0, "")
	_ = p.Set("global", "", "tier_inhouse", "", 0, "")

	n, err := p.UnpinAll()
	if err != nil {
		t.Fatalf("UnpinAll: %v", err)
	}
	if n != 3 {
		t.Errorf("UnpinAll returned %d, want 3", n)
	}
	rows, _ := p.ListAll()
	if len(rows) != 0 {
		t.Errorf("ListAll after UnpinAll has %d rows, want 0", len(rows))
	}
}

func TestStartTTLSweepRunsOnInterval(t *testing.T) {
	s := newFakePinStore()
	past := time.Now().Add(-time.Hour)
	_ = s.Insert(PinRow{Scope: "session", ScopeID: "expired", Tier: "tier_inhouse", SetAt: past, ExpiresAt: &past})
	_ = s.Insert(PinRow{Scope: "session", ScopeID: "alive", Tier: "tier_inhouse", SetAt: time.Now()})

	p := NewPinOverrides(s)
	p.tickInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := p.StartTTLSweep(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, expiredStill := s.rows[s.key("session", "expired")]
		s.mu.Unlock()
		if !expiredStill {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	if _, ok := s.rows[s.key("session", "expired")]; ok {
		s.mu.Unlock()
		t.Error("expired pin not purged by sweep")
	} else {
		s.mu.Unlock()
	}
	if atomic.LoadInt32(&s.purges) < 1 {
		t.Error("PurgeExpired never invoked")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("sweep goroutine did not exit on ctx cancel")
	}
}

func TestStartTTLSweepSwallowsStoreErrors(t *testing.T) {
	s := newFakePinStore()
	s.purgeErr = errors.New("disk i/o")
	p := NewPinOverrides(s)
	p.tickInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := p.StartTTLSweep(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("sweep goroutine did not exit despite repeated errors")
	}

	if atomic.LoadInt32(&s.purges) < 1 {
		t.Error("expected at least one PurgeExpired call")
	}
}

func TestPinOverridesNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewPinOverrides(nil) did not panic")
		}
	}()
	_ = NewPinOverrides(nil)
}

func TestPinOverridesResolveStoreErrorPropagates(t *testing.T) {
	s := newFakePinStore()
	s.queryErr = errors.New("disk corrupted")
	p := NewPinOverrides(s)

	got, err := p.Resolve("sess", "proj")
	if err == nil {
		t.Fatal("expected error to propagate, got nil")
	}
	if got != nil {
		t.Errorf("expected nil pin on error, got %+v", got)
	}
	if !errors.Is(err, s.queryErr) {
		t.Errorf("error chain broken: want wrap of %v, got %v", s.queryErr, err)
	}
}

func TestPinOverridesStartTTLSweepDefaultInterval(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)

	ctx, cancel := context.WithCancel(context.Background())
	done := p.StartTTLSweep(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("sweep with default interval did not exit on cancel")
	}
}

func TestPinOverridesStartTTLSweepGracefulShutdown(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	p.tickInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := p.StartTTLSweep(ctx)
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("sweep did not honour ctx cancel within 1s")
	}
}

func TestPinOverridesStartTTLSweepCallsPurge(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	p.tickInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := p.StartTTLSweep(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if got := atomic.LoadInt32(&s.purges); got < 1 {
		t.Errorf("expected ≥1 PurgeExpired call, got %d", got)
	}
}

func TestPinOverridesResolveEmptyProjectIDSkipsProjectScope(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	_ = p.Set("global", "", "GLOBAL", "", 0, "")

	got, err := p.Resolve("", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil || got.Tier != "GLOBAL" {
		t.Errorf("got=%+v, want GLOBAL (project scope skipped due to empty id)", got)
	}
}

func TestPinOverridesUnpinAllOnEmptyStoreReturnsZero(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)

	n, err := p.UnpinAll()
	if err != nil {
		t.Fatalf("UnpinAll on empty store: %v", err)
	}
	if n != 0 {
		t.Errorf("UnpinAll on empty store returned %d, want 0", n)
	}
}

func TestPinOverridesSetValidationRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name                           string
		scope, scopeID, tier, provider string
		ttl                            time.Duration
		wantSubstr                     string
	}{
		{"empty scope", "", "id", "tier_inhouse", "", 0, "scope and tier are required"},
		{"empty tier", "session", "id", "", "", 0, "scope and tier are required"},
		{"unknown scope", "user", "id", "tier_inhouse", "", 0, "scope must be session|project|global"},
		{"global with scopeID", "global", "id", "tier_inhouse", "", 0, "scope=global requires empty scopeID"},
		{"session without scopeID", "session", "", "tier_inhouse", "", 0, "requires non-empty scopeID"},
		{"project without scopeID", "project", "", "tier_inhouse", "", 0, "requires non-empty scopeID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakePinStore()
			p := NewPinOverrides(s)
			err := p.Set(tc.scope, tc.scopeID, tc.tier, tc.provider, tc.ttl, "")
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSubstr)
			}
			if !contains(err.Error(), tc.wantSubstr) {
				t.Errorf("err=%q, want substring %q", err.Error(), tc.wantSubstr)
			}

			s.mu.Lock()
			n := len(s.rows)
			s.mu.Unlock()
			if n != 0 {
				t.Errorf("validation failure leaked into store: %d rows", n)
			}
		})
	}
}

func TestPinOverridesUnsetEmptyScopeRejected(t *testing.T) {
	s := newFakePinStore()
	p := NewPinOverrides(s)
	err := p.Unset("", "anything")
	if err == nil {
		t.Fatal("expected error for empty scope, got nil")
	}
	if !contains(err.Error(), "scope is required") {
		t.Errorf("err=%q, want substring 'scope is required'", err.Error())
	}
}

type errPinStore struct {
	insertErr  error
	deleteErr  error
	listErr    error
	listResult []PinRow
}

func (e *errPinStore) Insert(p PinRow) error                        { return e.insertErr }
func (e *errPinStore) Delete(scope, scopeID string) error           { return e.deleteErr }
func (e *errPinStore) Query(scope, scopeID string) (*PinRow, error) { return nil, nil }
func (e *errPinStore) ListAll() ([]PinRow, error)                   { return e.listResult, e.listErr }
func (e *errPinStore) PurgeExpired(now time.Time) (int, error)      { return 0, nil }

func TestPinOverridesSetWrapsStoreError(t *testing.T) {
	sentinel := errors.New("disk full")
	p := NewPinOverrides(&errPinStore{insertErr: sentinel})
	err := p.Set("session", "s1", "tier_inhouse", "", 0, "")
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: want wrap of %v, got %v", sentinel, err)
	}
}

func TestPinOverridesUnsetWrapsStoreError(t *testing.T) {
	sentinel := errors.New("locked")
	p := NewPinOverrides(&errPinStore{deleteErr: sentinel})
	err := p.Unset("session", "s1")
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: want wrap of %v, got %v", sentinel, err)
	}
}

func TestPinOverridesListAllWrapsStoreError(t *testing.T) {
	sentinel := errors.New("io")
	p := NewPinOverrides(&errPinStore{listErr: sentinel})
	rows, err := p.ListAll()
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if rows != nil {
		t.Errorf("expected nil rows on error, got %v", rows)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: want wrap of %v, got %v", sentinel, err)
	}
}

func TestPinOverridesUnpinAllWrapsListError(t *testing.T) {
	sentinel := errors.New("io")
	p := NewPinOverrides(&errPinStore{listErr: sentinel})
	n, err := p.UnpinAll()
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if n != 0 {
		t.Errorf("expected 0 deletions on list error, got %d", n)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: want wrap of %v, got %v", sentinel, err)
	}
}

func TestPinOverridesUnpinAllWrapsDeleteError(t *testing.T) {
	sentinel := errors.New("locked")
	p := NewPinOverrides(&errPinStore{
		deleteErr:  sentinel,
		listResult: []PinRow{{Scope: "session", ScopeID: "s1", Tier: "tier_inhouse", SetAt: time.Now()}},
	})
	n, err := p.UnpinAll()
	if err == nil {
		t.Fatal("expected wrapped error, got nil")
	}
	if n != 0 {
		t.Errorf("expected 0 successful deletions before failure, got %d", n)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error chain broken: want wrap of %v, got %v", sentinel, err)
	}
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
