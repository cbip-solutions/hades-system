package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openPinStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "pin.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertPinPermanent(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	row := PinRow{
		Scope: "session", ScopeID: "sess-1",
		Tier: "opus-paygo", Provider: "anthropic",
		SetAt: now, ExpiresAt: nil, Reason: "investigating regression",
	}
	if err := s.InsertPin(row); err != nil {
		t.Fatalf("InsertPin: %v", err)
	}
	got, err := s.QueryPin("session", "sess-1")
	if err != nil {
		t.Fatalf("QueryPin: %v", err)
	}
	if got == nil {
		t.Fatal("QueryPin returned nil for inserted pin")
	}
	if got.Tier != "opus-paygo" || got.Provider != "anthropic" {
		t.Errorf("tier/provider mismatch: %+v", got)
	}
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt expected nil for permanent pin, got %v", got.ExpiresAt)
	}
}

func TestInsertPinWithTTL(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	exp := now.Add(1 * time.Hour)
	row := PinRow{
		Scope: "project", ScopeID: "internal-platform-x",
		Tier: "gemini-3-pro", SetAt: now, ExpiresAt: &exp,
	}
	if err := s.InsertPin(row); err != nil {
		t.Fatalf("InsertPin: %v", err)
	}
	got, err := s.QueryPin("project", "internal-platform-x")
	if err != nil {
		t.Fatalf("QueryPin: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt expected non-nil")
	}
	if !got.ExpiresAt.Equal(exp.UTC().Truncate(time.Second)) {
		t.Errorf("ExpiresAt = %v, want %v (truncated to seconds)", got.ExpiresAt, exp)
	}
}

func TestInsertPinUpsertsOnSameScope(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	first := PinRow{Scope: "session", ScopeID: "sess-X", Tier: "T1", SetAt: now}
	second := PinRow{Scope: "session", ScopeID: "sess-X", Tier: "T2", SetAt: now.Add(time.Second)}

	if err := s.InsertPin(first); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := s.InsertPin(second); err != nil {
		t.Fatalf("second insert (upsert): %v", err)
	}
	got, _ := s.QueryPin("session", "sess-X")
	if got == nil || got.Tier != "T2" {
		t.Errorf("upsert failed: got=%+v, want tier=T2", got)
	}
	all, _ := s.ListAllPins()
	if len(all) != 1 {
		t.Errorf("expected 1 row after upsert, got %d", len(all))
	}
}

func TestQueryPinReturnsNilWhenAbsent(t *testing.T) {
	s := openPinStore(t)
	got, err := s.QueryPin("session", "missing")
	if err != nil {
		t.Errorf("QueryPin returned err for missing: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestDeletePin(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	_ = s.InsertPin(PinRow{Scope: "global", ScopeID: "", Tier: "T1", SetAt: now})
	if err := s.DeletePin("global", ""); err != nil {
		t.Fatalf("DeletePin: %v", err)
	}
	got, _ := s.QueryPin("global", "")
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeletePinIdempotent(t *testing.T) {
	s := openPinStore(t)
	if err := s.DeletePin("session", "never-existed"); err != nil {
		t.Errorf("DeletePin on missing row should not error: %v", err)
	}
}

func TestListAllPinsOrdersBySetAtDesc(t *testing.T) {
	s := openPinStore(t)
	t0 := time.Unix(1_000_000, 0)
	for i, scope := range []struct{ s, id string }{
		{"session", "s1"}, {"project", "p1"}, {"global", ""},
	} {
		_ = s.InsertPin(PinRow{
			Scope: scope.s, ScopeID: scope.id, Tier: "T",
			SetAt: t0.Add(time.Duration(i) * time.Second),
		})
	}
	rows, err := s.ListAllPins()
	if err != nil {
		t.Fatalf("ListAllPins: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len=%d, want 3", len(rows))
	}

	if rows[0].Scope != "global" || rows[2].Scope != "session" {
		t.Errorf("unexpected order: %+v", rows)
	}
}

func TestPurgeExpiredPins(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	expPast := now.Add(-1 * time.Hour)
	expFuture := now.Add(1 * time.Hour)

	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "expired-1", Tier: "T", SetAt: now, ExpiresAt: &expPast})
	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "alive-1", Tier: "T", SetAt: now, ExpiresAt: &expFuture})
	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "permanent-1", Tier: "T", SetAt: now, ExpiresAt: nil})

	purged, err := s.PurgeExpiredPins(now)
	if err != nil {
		t.Fatalf("PurgeExpiredPins: %v", err)
	}
	if purged != 1 {
		t.Errorf("purged=%d, want 1", purged)
	}
	rows, _ := s.ListAllPins()
	if len(rows) != 2 {
		t.Errorf("after purge len=%d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.ScopeID == "expired-1" {
			t.Errorf("expired row not purged: %+v", r)
		}
	}
}

func TestPurgeExpiredPinsHandlesNoExpiredRows(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	_ = s.InsertPin(PinRow{Scope: "global", ScopeID: "", Tier: "T", SetAt: now})
	purged, err := s.PurgeExpiredPins(now)
	if err != nil {
		t.Fatalf("PurgeExpiredPins on empty-eligible: %v", err)
	}
	if purged != 0 {
		t.Errorf("purged=%d, want 0", purged)
	}
}

func TestPurgeExpiredPinsKeepsEqualInstant(t *testing.T) {
	// Boundary SQL predicate is `expires_at < ?` (strict). A pin with
	// expires_at == now MUST survive a purge invoked at exactly `now`.
	// Pins the documented contract on PurgeExpiredPins ("strictly less
	// than now") since I-2's 5-min sweep ticker may fire at the exact
	// expiry second.
	s := openPinStore(t)
	now := time.Unix(1_700_000_000, 0)
	if err := s.InsertPin(PinRow{
		Scope: "session", ScopeID: "edge", Tier: "T",
		SetAt: now, ExpiresAt: &now,
	}); err != nil {
		t.Fatalf("InsertPin: %v", err)
	}
	purged, err := s.PurgeExpiredPins(now)
	if err != nil {
		t.Fatalf("PurgeExpiredPins: %v", err)
	}
	if purged != 0 {
		t.Errorf("equal-instant should survive (strict <), got purged=%d", purged)
	}

	got, _ := s.QueryPin("session", "edge")
	if got == nil {
		t.Error("equal-instant pin was purged but should have survived")
	}
}

func TestInsertPinRejectsInvalidScope(t *testing.T) {
	s := openPinStore(t)
	err := s.InsertPin(PinRow{Scope: "bogus", ScopeID: "x", Tier: "T", SetAt: time.Now()})
	if err == nil {
		t.Error("expected CHECK constraint failure for scope='bogus'")
	}
}

func TestInsertPinInvalidScopeRejected(t *testing.T) {
	s := openPinStore(t)
	for _, bad := range []string{"invalid", "GLOBAL", "Session", "payg", ""} {
		err := s.InsertPin(PinRow{Scope: bad, ScopeID: "x", Tier: "T", SetAt: time.Now()})
		if err == nil {
			t.Errorf("InsertPin with scope=%q should have returned error", bad)
		}
	}
}

func TestInsertPinNullExpiresAtRoundtrip(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "perm-rt", Tier: "T", SetAt: now, ExpiresAt: nil})
	got, err := s.QueryPin("session", "perm-rt")
	if err != nil {
		t.Fatalf("QueryPin: %v", err)
	}
	if got == nil {
		t.Fatal("QueryPin returned nil")
	}
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt expected nil after roundtrip, got %v", got.ExpiresAt)
	}
}

func TestInsertPinExpiresAtTruncatesToSeconds(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()

	exp := now.Add(1*time.Hour + 500*time.Millisecond)
	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "sub-sec", Tier: "T", SetAt: now, ExpiresAt: &exp})
	got, err := s.QueryPin("session", "sub-sec")
	if err != nil {
		t.Fatalf("QueryPin: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt should be non-nil")
	}
	want := exp.UTC().Truncate(time.Second)
	if !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v (sub-second truncated)", got.ExpiresAt, want)
	}
}

func TestQueryPinDoesNotMatchWrongScopeID(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "sess-A", Tier: "T", SetAt: now})
	got, err := s.QueryPin("session", "sess-B")
	if err != nil {
		t.Fatalf("QueryPin: %v", err)
	}
	if got != nil {
		t.Errorf("QueryPin('session','sess-B') should return nil, got %+v", got)
	}
}

func TestListAllPinsEmptyReturnsConsistent(t *testing.T) {
	s := openPinStore(t)
	rows, err := s.ListAllPins()
	if err != nil {
		t.Fatalf("ListAllPins on empty table: %v", err)
	}

	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d: %+v", len(rows), rows)
	}
}

func TestDeletePinAffectsOnlyExactScope(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	_ = s.InsertPin(PinRow{Scope: "session", ScopeID: "shared-id", Tier: "T1", SetAt: now})
	_ = s.InsertPin(PinRow{Scope: "project", ScopeID: "shared-id", Tier: "T2", SetAt: now})
	if err := s.DeletePin("session", "shared-id"); err != nil {
		t.Fatalf("DeletePin: %v", err)
	}

	if got, _ := s.QueryPin("session", "shared-id"); got != nil {
		t.Errorf("session pin should be gone, got %+v", got)
	}

	if got, _ := s.QueryPin("project", "shared-id"); got == nil {
		t.Error("project pin should still exist after deleting session pin")
	}
}

func TestInsertPinGlobalScopeEmptyScopeID(t *testing.T) {
	s := openPinStore(t)
	now := time.Now()
	if err := s.InsertPin(PinRow{Scope: "global", ScopeID: "", Tier: "default", SetAt: now}); err != nil {
		t.Fatalf("InsertPin global empty scopeID: %v", err)
	}
	got, err := s.QueryPin("global", "")
	if err != nil {
		t.Fatalf("QueryPin: %v", err)
	}
	if got == nil {
		t.Fatal("QueryPin returned nil for global pin")
	}
	if got.Tier != "default" {
		t.Errorf("tier mismatch: %+v", got)
	}
}
