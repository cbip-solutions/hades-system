package quotaadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/quotaadapter"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func openTestStore(t *testing.T) *testhelpers.TestStore {
	t.Helper()
	return testhelpers.OpenInMemoryStore(t)
}

func TestAdapterImplementsOverrideStoreInterface(t *testing.T) {

	var _ quota.OverrideStore = (*quotaadapter.Adapter)(nil)
}

func TestAdapterNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil store; got none")
		}
	}()
	_ = quotaadapter.New(nil)
}

func TestAdapterNewOverrideStoreReturnsInterface(t *testing.T) {

	ts := openTestStore(t)
	os := quotaadapter.NewOverrideStore(ts.Store)
	if os == nil {
		t.Fatal("NewOverrideStore returned nil")
	}

	if _, err := os.List(context.Background()); err != nil {
		t.Errorf("List via OverrideStore interface: %v", err)
	}
}

func TestAdapterSetGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)

	expiresAt := time.Now().Add(4 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "internal-platform-x", 3.0, expiresAt, "investigation"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := a.Get(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Alias != "internal-platform-x" {
		t.Errorf("Alias = %q, want internal-platform-x", got.Alias)
	}
	if got.Multiplier != 3.0 {
		t.Errorf("Multiplier = %v, want 3.0", got.Multiplier)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, expiresAt)
	}
	if got.Reason != "investigation" {
		t.Errorf("Reason = %q, want %q", got.Reason, "investigation")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero; expected wall-clock from Set()")
	}
}

func TestAdapterGetUnknownReturnsNilNoError(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	got, err := a.Get(ctx, "no-such-project")
	if err != nil {
		t.Errorf("Get(unknown) err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Get(unknown) = %+v, want nil", got)
	}
}

func TestAdapterSetUpsert(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "x", 2.0, expiry, "first"); err != nil {
		t.Fatalf("Set first: %v", err)
	}

	expiry2 := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "x", 5.0, expiry2, "second"); err != nil {
		t.Fatalf("Set upsert: %v", err)
	}
	got, _ := a.Get(ctx, "x")
	if got == nil {
		t.Fatal("Get returned nil after upsert")
	}
	if got.Multiplier != 5.0 {
		t.Errorf("Multiplier after upsert = %v, want 5.0", got.Multiplier)
	}
	if got.Reason != "second" {
		t.Errorf("Reason after upsert = %q, want %q", got.Reason, "second")
	}
	if !got.ExpiresAt.Equal(expiry2) {
		t.Errorf("ExpiresAt after upsert = %v, want %v", got.ExpiresAt, expiry2)
	}
}

func TestAdapterReset(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	_ = a.Set(ctx, "x", 3.0, expiry, "demo")
	if err := a.Reset(ctx, "x"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	got, _ := a.Get(ctx, "x")
	if got != nil {
		t.Errorf("Reset did not delete row; got %+v", got)
	}
}

func TestAdapterResetIdempotent(t *testing.T) {

	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if err := a.Reset(ctx, "no-such-alias"); err != nil {
		t.Errorf("Reset on unknown alias = %v, want nil (idempotent)", err)
	}
	resets, err := ts.Store.ListEventsByKind(context.Background(), "quota.priority_boost.reset")
	if err != nil {
		t.Fatalf("ListEventsByKind: %v", err)
	}
	if len(resets) != 1 {
		t.Errorf("idempotent reset event count = %d, want 1", len(resets))
	}
}

func TestAdapterResetEmptyAlias(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	err := a.Reset(ctx, "  ")
	if err == nil {
		t.Fatal("Reset(empty): want error, got nil")
	}
	if !errors.Is(err, quota.ErrInvalidOverride) {
		t.Errorf("Reset(empty) err = %v, want errors.Is ErrInvalidOverride", err)
	}
}

func TestAdapterList(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	_ = a.Set(ctx, "a", 2.0, expiry, "ra")
	_ = a.Set(ctx, "b", 3.0, expiry, "rb")
	_ = a.Set(ctx, "c", 4.0, expiry, "rc")
	all, err := a.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List len = %d, want 3", len(all))
	}
	aliases := map[string]bool{}
	for _, ov := range all {
		aliases[ov.Alias] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !aliases[want] {
			t.Errorf("List missing alias %q; got %v", want, aliases)
		}
	}
}

func TestAdapterListEmpty(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	all, err := a.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("List on empty = %d rows, want 0", len(all))
	}
}

func TestAdapterSetEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "internal-platform-x", 3.0, expiry, "demo"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	events, err := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.set")
	if err != nil {
		t.Fatalf("ListEventsByKind: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}

	if !strings.Contains(events[0].PayloadJSON, "internal-platform-x") {
		t.Errorf("event payload missing alias; got %q", events[0].PayloadJSON)
	}
	if !strings.Contains(events[0].PayloadJSON, "set") {
		t.Errorf("event payload missing action 'set'; got %q", events[0].PayloadJSON)
	}
}

func TestAdapterSetUpsertEmitsReplacedEvent(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	_ = a.Set(ctx, "x", 2.0, expiry, "first")
	_ = a.Set(ctx, "x", 5.0, expiry, "second")
	replaced, _ := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.replaced")
	if len(replaced) != 1 {
		t.Errorf("replaced event count = %d, want 1", len(replaced))
	}
	sets, _ := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.set")
	if len(sets) != 2 {
		t.Errorf("set event count = %d, want 2 (initial + upsert)", len(sets))
	}
}

func TestAdapterResetEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	_ = a.Set(ctx, "x", 3.0, expiry, "demo")
	_ = a.Reset(ctx, "x")
	resets, err := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.reset")
	if err != nil {
		t.Fatalf("ListEventsByKind: %v", err)
	}
	if len(resets) != 1 {
		t.Errorf("reset event count = %d, want 1", len(resets))
	}
}

func TestAdapterTransactionRollsBackOnAuditFailure(t *testing.T) {
	// If the audit INSERT fails, the priority_overrides INSERT MUST also
	// roll back — atomicity is load-bearing for invariant /
	// audit chain integrity.
	//
	// We simulate audit failure by dropping the events table after a
	// pre-insert; the subsequent Set must fail AND the prior row must
	// remain unchanged (verified via Get after re-creating events).
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "x", 2.0, expiry, "pre"); err != nil {
		t.Fatalf("pre-insert Set: %v", err)
	}
	if _, err := ts.Store.ExecRaw(ctx, "DROP TABLE events"); err != nil {
		t.Fatalf("setup: drop events: %v", err)
	}
	err := a.Set(ctx, "x", 5.0, expiry, "should-rollback")
	if err == nil {
		t.Fatal("Set with broken audit table: want error, got nil")
	}

	if _, err := ts.Store.ExecRaw(ctx,
		`CREATE TABLE events (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			ts           INTEGER NOT NULL,
			project      TEXT,
			session_id   TEXT,
			swarm_id     TEXT,
			task_id      TEXT,
			type         TEXT NOT NULL,
			payload_json TEXT
		)`); err != nil {
		t.Fatalf("setup: recreate events: %v", err)
	}
	got, _ := a.Get(ctx, "x")
	if got == nil {
		t.Fatal("rollback failed; Get returned nil (row was deleted)")
	}
	if got.Multiplier != 2.0 || got.Reason != "pre" {
		t.Errorf("rollback failed; got %+v, want pre-insert preserved", got)
	}
}

func TestAdapterValidationRejectsInvalidArgs(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	cases := []struct {
		name      string
		alias     string
		mult      float64
		expiresAt time.Time
		reason    string
	}{
		{"empty alias", "", 3.0, time.Now().Add(1 * time.Hour), "demo"},
		{"whitespace alias", "   ", 3.0, time.Now().Add(1 * time.Hour), "demo"},
		{"zero mult", "x", 0, time.Now().Add(1 * time.Hour), "demo"},
		{"negative mult", "x", -1, time.Now().Add(1 * time.Hour), "demo"},
		{"excess mult", "x", 1000, time.Now().Add(1 * time.Hour), "demo"},
		{"past expiry", "x", 3.0, time.Now().Add(-1 * time.Hour), "stale"},
		{"empty reason", "x", 3.0, time.Now().Add(1 * time.Hour), ""},
		{"whitespace reason", "x", 3.0, time.Now().Add(1 * time.Hour), "   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := a.Set(ctx, c.alias, c.mult, c.expiresAt, c.reason)
			if err == nil {
				t.Errorf("Set(%+v): want error, got nil", c)
			}
			if !errors.Is(err, quota.ErrInvalidOverride) {
				t.Errorf("Set(%s): err = %v, want errors.Is ErrInvalidOverride", c.name, err)
			}
		})
	}

	rows, err := a.List(ctx)
	if err != nil {
		t.Fatalf("List after invalid Set: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("invalid Sets leaked rows: %d", len(rows))
	}
	for _, kind := range []string{
		"quota.priority_boost.set",
		"quota.priority_boost.replaced",
		"quota.priority_boost.reset",
	} {
		evs, err := ts.Store.ListEventsByKind(ctx, kind)
		if err != nil {
			t.Fatalf("ListEventsByKind(%s): %v", kind, err)
		}
		if len(evs) != 0 {
			t.Errorf("invalid Sets leaked %s events: %d", kind, len(evs))
		}
	}
}

func TestAdapterSetCtxCancelled(t *testing.T) {
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	err := a.Set(ctx, "x", 3.0, expiry, "demo")
	if err == nil {
		t.Fatal("Set with cancelled ctx: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Set ctx.Canceled: err = %v, want errors.Is context.Canceled", err)
	}
}

func TestAdapterGetCtxCancelled(t *testing.T) {
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Get(ctx, "x")
	if err == nil {
		t.Fatal("Get with cancelled ctx: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Get ctx.Canceled: err = %v, want errors.Is context.Canceled", err)
	}
}

func TestAdapterResetCtxCancelled(t *testing.T) {
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Reset(ctx, "x")
	if err == nil {
		t.Fatal("Reset with cancelled ctx: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Reset ctx.Canceled: err = %v, want errors.Is context.Canceled", err)
	}
}

func TestAdapterListCtxCancelled(t *testing.T) {
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.List(ctx)
	if err == nil {
		t.Fatal("List with cancelled ctx: want error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("List ctx.Canceled: err = %v, want errors.Is context.Canceled", err)
	}
}

func TestAdapterListOrdering(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	if err := a.Set(ctx, "first", 2.0, expiry, "r1"); err != nil {
		t.Fatalf("Set 1: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := a.Set(ctx, "second", 3.0, expiry, "r2"); err != nil {
		t.Fatalf("Set 2: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := a.Set(ctx, "third", 4.0, expiry, "r3"); err != nil {
		t.Fatalf("Set 3: %v", err)
	}
	all, err := a.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List len = %d, want 3", len(all))
	}

	wantOrder := []string{"third", "second", "first"}
	for i, want := range wantOrder {
		if all[i].Alias != want {
			t.Errorf("List[%d].Alias = %q, want %q (full: %v)", i, all[i].Alias, want, all)
		}
	}
}

func TestAdapterPayloadIncludesAllFields(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "alpha", 7.5, expiry, "release-investigation"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	events, err := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.set")
	if err != nil {
		t.Fatalf("ListEventsByKind: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	p := events[0].PayloadJSON
	for _, want := range []string{
		"alpha",
		"7.5",
		"release-investigation",
		"set",
		"expires_at",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("payload missing %q; got %q", want, p)
		}
	}
}

func TestAdapterResetPayloadCarriesAlias(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	_ = a.Set(ctx, "beta", 2.0, expiry, "demo")
	if err := a.Reset(ctx, "beta"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	resets, _ := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.reset")
	if len(resets) != 1 {
		t.Fatalf("reset event count = %d, want 1", len(resets))
	}
	if !strings.Contains(resets[0].PayloadJSON, "beta") {
		t.Errorf("reset payload missing alias; got %q", resets[0].PayloadJSON)
	}
	if !strings.Contains(resets[0].PayloadJSON, "reset") {
		t.Errorf("reset payload missing action; got %q", resets[0].PayloadJSON)
	}
}

func TestAdapterTimeUTCNormalisation(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("LoadLocation: %v", err)
	}
	expiry := time.Now().In(loc).Add(1 * time.Hour).Truncate(time.Second)
	if err := a.Set(ctx, "tz", 2.0, expiry, "demo"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, _ := a.Get(ctx, "tz")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.ExpiresAt.Location() != time.UTC {
		t.Errorf("ExpiresAt timezone = %v, want UTC", got.ExpiresAt.Location())
	}

	if !got.ExpiresAt.Equal(expiry) {
		t.Errorf("ExpiresAt round-trip = %v, want %v (equiv)", got.ExpiresAt, expiry)
	}
}

func TestAdapterFileBackedStoreSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "p7.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	a := quotaadapter.New(s)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "persisted", 3.0, expiry, "survives"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_ = s.Close()

	s2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer s2.Close()
	if err := s2.Migrate(); err != nil {
		t.Fatalf("re-Migrate: %v", err)
	}
	a2 := quotaadapter.New(s2)
	got, err := a2.Get(ctx, "persisted")
	if err != nil {
		t.Fatalf("Get post-reopen: %v", err)
	}
	if got == nil {
		t.Fatal("row did not survive reopen")
	}
	if got.Multiplier != 3.0 {
		t.Errorf("Multiplier post-reopen = %v, want 3.0", got.Multiplier)
	}
	events, err := s2.ListEventsByKind(ctx, "quota.priority_boost.set")
	if err != nil {
		t.Fatalf("ListEventsByKind post-reopen: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("event count post-reopen = %d, want 1", len(events))
	}
}

func TestAdapterGetEmptyAlias(t *testing.T) {
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	cases := []string{"", "  ", "\t\n"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := a.Get(context.Background(), c)
			if err == nil {
				t.Fatal("Get(empty): want error, got nil")
			}
			if !errors.Is(err, quota.ErrInvalidOverride) {
				t.Errorf("Get(empty) err = %v, want errors.Is ErrInvalidOverride", err)
			}
		})
	}
}

func TestAdapterSetOnClosedStoreSurfacesError(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if err := ts.Store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	err := a.Set(ctx, "x", 3.0, expiry, "demo")
	if err == nil {
		t.Fatal("Set on closed store: want error, got nil")
	}
	if !strings.Contains(err.Error(), "quotaadapter.Set") {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestAdapterResetOnClosedStoreSurfacesError(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if err := ts.Store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := a.Reset(ctx, "x")
	if err == nil {
		t.Fatal("Reset on closed store: want error, got nil")
	}
	if !strings.Contains(err.Error(), "quotaadapter.Reset") {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestAdapterGetOnClosedStoreSurfacesError(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if err := ts.Store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := a.Get(ctx, "x")
	if err == nil {
		t.Fatal("Get on closed store: want error, got nil")
	}
	if !strings.Contains(err.Error(), "quotaadapter.Get") {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestAdapterListOnClosedStoreSurfacesError(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if err := ts.Store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := a.List(ctx)
	if err == nil {
		t.Fatal("List on closed store: want error, got nil")
	}
	if !strings.Contains(err.Error(), "quotaadapter.List") {
		t.Errorf("error not wrapped: %v", err)
	}
}

func TestAdapterSetUpsertFailureOnDroppedTable(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if _, err := ts.Store.ExecRaw(ctx, "DROP TABLE priority_overrides"); err != nil {
		t.Fatalf("setup: drop priority_overrides: %v", err)
	}
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	err := a.Set(ctx, "x", 3.0, expiry, "demo")
	if err == nil {
		t.Fatal("Set with dropped table: want error, got nil")
	}

	if _, err := ts.Store.ExecRaw(ctx,
		`CREATE TABLE priority_overrides (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_alias TEXT NOT NULL UNIQUE,
			multiplier REAL NOT NULL CHECK(multiplier > 0),
			expires_at TIMESTAMP NOT NULL,
			reason TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		t.Fatalf("setup: recreate priority_overrides: %v", err)
	}
	events, err := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.set")
	if err != nil {
		t.Fatalf("ListEventsByKind: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("rollback failed; %d set events leaked", len(events))
	}
}

func TestAdapterResetDeleteFailureOnDroppedTable(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if _, err := ts.Store.ExecRaw(ctx, "DROP TABLE priority_overrides"); err != nil {
		t.Fatalf("setup: drop priority_overrides: %v", err)
	}
	err := a.Reset(ctx, "x")
	if err == nil {
		t.Fatal("Reset with dropped priority_overrides: want error, got nil")
	}
}

func TestAdapterSetReplacedAuditFailure(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "x", 2.0, expiry, "first"); err != nil {
		t.Fatalf("first Set: %v", err)
	}

	if _, err := ts.Store.ExecRaw(ctx, "DROP TABLE events"); err != nil {
		t.Fatalf("setup: drop events: %v", err)
	}
	err := a.Set(ctx, "x", 5.0, expiry, "second")
	if err == nil {
		t.Fatal("Set with broken replaced-event audit: want error, got nil")
	}
	if !strings.Contains(err.Error(), "replaced event") &&
		!strings.Contains(err.Error(), "set event") {
		t.Errorf("error doesn't reference event-emit path: %v", err)
	}

	if _, err := ts.Store.ExecRaw(ctx,
		`CREATE TABLE events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			project TEXT,
			session_id TEXT,
			swarm_id TEXT,
			task_id TEXT,
			type TEXT NOT NULL,
			payload_json TEXT
		)`); err != nil {
		t.Fatalf("setup: recreate events: %v", err)
	}
	got, _ := a.Get(ctx, "x")
	if got == nil || got.Multiplier != 2.0 || got.Reason != "first" {
		t.Errorf("rollback failed; got %+v, want first preserved", got)
	}
}

func TestAdapterResetAuditFailure(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "x", 3.0, expiry, "demo"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := ts.Store.ExecRaw(ctx, "DROP TABLE events"); err != nil {
		t.Fatalf("setup: drop events: %v", err)
	}
	err := a.Reset(ctx, "x")
	if err == nil {
		t.Fatal("Reset with broken audit table: want error, got nil")
	}
	if _, err := ts.Store.ExecRaw(ctx,
		`CREATE TABLE events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			project TEXT,
			session_id TEXT,
			swarm_id TEXT,
			task_id TEXT,
			type TEXT NOT NULL,
			payload_json TEXT
		)`); err != nil {
		t.Fatalf("setup: recreate events: %v", err)
	}
	got, _ := a.Get(ctx, "x")
	if got == nil {
		t.Error("rollback failed; row was deleted despite audit failure")
	}
}

func TestAdapterSetFreshInsertAuditFailure(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	if _, err := ts.Store.ExecRaw(ctx, "DROP TABLE events"); err != nil {
		t.Fatalf("setup: drop events: %v", err)
	}
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	err := a.Set(ctx, "fresh", 3.0, expiry, "demo")
	if err == nil {
		t.Fatal("Set on fresh insert with broken audit: want error, got nil")
	}
	if !strings.Contains(err.Error(), "set event") {
		t.Errorf("error doesn't reference set-event path: %v", err)
	}
	if _, err := ts.Store.ExecRaw(ctx,
		`CREATE TABLE events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			project TEXT,
			session_id TEXT,
			swarm_id TEXT,
			task_id TEXT,
			type TEXT NOT NULL,
			payload_json TEXT
		)`); err != nil {
		t.Fatalf("setup: recreate events: %v", err)
	}

	got, _ := a.Get(ctx, "fresh")
	if got != nil {
		t.Errorf("rollback failed; row %+v leaked despite audit failure", got)
	}
}

func TestBuildPayloadShape(t *testing.T) {
	ctx := context.Background()
	ts := openTestStore(t)
	a := quotaadapter.New(ts.Store)
	expiry := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)
	if err := a.Set(ctx, "shape", 1.5, expiry, "test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	events, err := ts.Store.ListEventsByKind(ctx, "quota.priority_boost.set")
	if err != nil {
		t.Fatalf("ListEventsByKind: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	p := events[0].PayloadJSON
	if !strings.HasPrefix(p, "{") || !strings.HasSuffix(p, "}") {
		t.Errorf("payload not a JSON object: %q", p)
	}

	type roundtrip struct {
		Alias      string  `json:"alias"`
		Multiplier float64 `json:"multiplier"`
		Action     string  `json:"action"`
		Reason     string  `json:"reason"`
	}
	var rt roundtrip
	if err := json.Unmarshal([]byte(p), &rt); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if rt.Alias != "shape" || rt.Multiplier != 1.5 || rt.Action != "set" || rt.Reason != "test" {
		t.Errorf("roundtrip = %+v, want shape/1.5/set/test", rt)
	}
}
