package quota

import (
	"context"
	"testing"
	"time"
)

type fakeOverrideStore struct {
	rows         map[string]Override
	resetCalls   []string
	setCalls     []Override
	auditEntries []string
}

func newFakeOverrideStore() *fakeOverrideStore {
	return &fakeOverrideStore{rows: map[string]Override{}}
}

func (f *fakeOverrideStore) Get(_ context.Context, alias string) (*Override, error) {
	r, ok := f.rows[alias]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (f *fakeOverrideStore) Set(_ context.Context, alias string, mult float64, expiresAt time.Time, reason string) error {
	if err := validateOverrideArgs(alias, mult, expiresAt, reason, time.Now()); err != nil {
		return err
	}
	prior, hasPrior := f.rows[alias]
	ov := Override{
		Alias:      alias,
		Multiplier: mult,
		ExpiresAt:  expiresAt,
		Reason:     reason,
		CreatedAt:  time.Now(),
	}
	f.rows[alias] = ov
	f.setCalls = append(f.setCalls, ov)
	if hasPrior {
		f.auditEntries = append(f.auditEntries, "quota.priority_boost.replaced:"+prior.Alias)
	}
	f.auditEntries = append(f.auditEntries, "quota.priority_boost.set:"+alias)
	return nil
}

func (f *fakeOverrideStore) Reset(_ context.Context, alias string) error {
	delete(f.rows, alias)
	f.resetCalls = append(f.resetCalls, alias)
	f.auditEntries = append(f.auditEntries, "quota.priority_boost.reset:"+alias)
	return nil
}

func (f *fakeOverrideStore) List(_ context.Context) ([]Override, error) {
	out := make([]Override, 0, len(f.rows))
	for _, ov := range f.rows {
		out = append(out, ov)
	}
	return out, nil
}

func TestApplyOverrideNilReturnsBase(t *testing.T) {
	got := ApplyOverride(Weight(1.0), nil)
	if got != Weight(1.0) {
		t.Errorf("ApplyOverride(1.0, nil) = %v, want 1.0", got)
	}
}

func TestApplyOverrideActiveAppliesMultiplier(t *testing.T) {
	ov := &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  time.Now().Add(1 * time.Hour),
		Reason:     "urgent investigation",
		CreatedAt:  time.Now(),
	}
	got := ApplyOverride(Weight(1.0), ov)
	if got != Weight(3.0) {
		t.Errorf("ApplyOverride(1.0, x3) = %v, want 3.0", got)
	}
}

func TestApplyOverrideExpiredReturnsBase(t *testing.T) {
	ov := &Override{
		Alias:      "internal-platform-x",
		Multiplier: 3.0,
		ExpiresAt:  time.Now().Add(-1 * time.Hour),
		Reason:     "stale",
		CreatedAt:  time.Now().Add(-2 * time.Hour),
	}
	got := ApplyOverride(Weight(1.0), ov)
	if got != Weight(1.0) {
		t.Errorf("ApplyOverride(1.0, expired) = %v, want 1.0 (base preserved)", got)
	}
}

func TestApplyOverrideMaxScopeBaseTimes3(t *testing.T) {

	ov := &Override{
		Alias:      "x",
		Multiplier: 3.0,
		ExpiresAt:  time.Now().Add(4 * time.Hour),
		Reason:     "demo",
	}
	got := ApplyOverride(Weight(1.5), ov)
	if got != Weight(4.5) {
		t.Errorf("ApplyOverride(1.5, x3) = %v, want 4.5", got)
	}
}

func TestApplyOverrideZeroMultiplierIgnored(t *testing.T) {

	for _, mult := range []float64{0, -1, -100} {
		ov := &Override{
			Alias:      "x",
			Multiplier: mult,
			ExpiresAt:  time.Now().Add(1 * time.Hour),
			Reason:     "should-not-apply",
		}
		got := ApplyOverride(Weight(1.0), ov)
		if got != Weight(1.0) {
			t.Errorf("ApplyOverride(1.0, x%v) = %v, want 1.0 (base preserved)", mult, got)
		}
	}
}

func TestOverrideStoreSetGet(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(4 * time.Hour)
	if err := s.Set(ctx, "internal-platform-x", 3.0, expiry, "investigation"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(ctx, "internal-platform-x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Multiplier != 3.0 {
		t.Errorf("Multiplier = %v, want 3.0", got.Multiplier)
	}
	if got.Reason != "investigation" {
		t.Errorf("Reason = %q, want %q", got.Reason, "investigation")
	}
}

func TestOverrideStoreSetEmitsAuditEvent(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	_ = s.Set(ctx, "internal-platform-x", 3.0, expiry, "demo")
	found := false
	for _, e := range s.auditEntries {
		if e == "quota.priority_boost.set:internal-platform-x" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("audit event not emitted; got %v", s.auditEntries)
	}
}

func TestOverrideStoreSetRejectsZeroMultiplier(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	cases := []float64{0, -1, -100}
	for _, mult := range cases {
		err := s.Set(ctx, "x", mult, expiry, "bad")
		if err == nil {
			t.Errorf("Set with mult=%v: want error, got nil", mult)
		}
	}
}

func TestOverrideStoreSetRejectsExcessMultiplier(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	err := s.Set(ctx, "x", 1000.0, expiry, "absurd")
	if err == nil {
		t.Error("Set with mult=1000 should error (sanity bound)")
	}
}

func TestOverrideStoreSetRejectsPastExpiry(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	past := time.Now().Add(-1 * time.Hour)
	err := s.Set(ctx, "x", 3.0, past, "stale")
	if err == nil {
		t.Error("Set with past expiry should error")
	}
}

func TestOverrideStoreSetRejectsEmptyReason(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	err := s.Set(ctx, "x", 3.0, expiry, "")
	if err == nil {
		t.Error("Set with empty reason should error (audit trail demands intent)")
	}
}

func TestOverrideStoreSetUpsertEmitsReplacedEvent(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	_ = s.Set(ctx, "x", 3.0, expiry, "first")

	s.auditEntries = s.auditEntries[:0]
	_ = s.Set(ctx, "x", 5.0, expiry, "second")
	foundReplace := false
	foundSet := false
	for _, e := range s.auditEntries {
		if e == "quota.priority_boost.replaced:x" {
			foundReplace = true
		}
		if e == "quota.priority_boost.set:x" {
			foundSet = true
		}
	}
	if !foundReplace {
		t.Errorf("missing replaced event; got %v", s.auditEntries)
	}
	if !foundSet {
		t.Errorf("missing set event; got %v", s.auditEntries)
	}
}

func TestOverrideStoreReset(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	_ = s.Set(ctx, "x", 3.0, expiry, "demo")
	if err := s.Reset(ctx, "x"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	got, _ := s.Get(ctx, "x")
	if got != nil {
		t.Error("Reset did not remove override")
	}
}

func TestOverrideStoreList(t *testing.T) {
	ctx := context.Background()
	s := newFakeOverrideStore()
	expiry := time.Now().Add(1 * time.Hour)
	_ = s.Set(ctx, "a", 2.0, expiry, "ra")
	_ = s.Set(ctx, "b", 3.0, expiry, "rb")
	_ = s.Set(ctx, "c", 4.0, expiry, "rc")
	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List len = %d, want 3", len(all))
	}
}

func TestValidateOverrideArgsAllChecks(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	expiryOK := now.Add(1 * time.Hour)
	cases := []struct {
		name      string
		alias     string
		mult      float64
		expiresAt time.Time
		reason    string
		wantErr   bool
	}{
		{"valid", "x", 3.0, expiryOK, "demo", false},
		{"empty alias", "", 3.0, expiryOK, "demo", true},
		{"zero mult", "x", 0, expiryOK, "demo", true},
		{"negative mult", "x", -1, expiryOK, "demo", true},
		{"excess mult", "x", 1001, expiryOK, "demo", true},
		{"max valid mult", "x", 100, expiryOK, "demo", false},
		{"past expiry", "x", 3.0, now.Add(-1 * time.Second), "demo", true},
		{"now expiry", "x", 3.0, now, "demo", true},
		{"empty reason", "x", 3.0, expiryOK, "", true},
		{"whitespace reason", "x", 3.0, expiryOK, "   ", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateOverrideArgs(c.alias, c.mult, c.expiresAt, c.reason, now)
			if (err != nil) != c.wantErr {
				t.Errorf("validateOverrideArgs(...) err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestApplyOverrideSetNowFuncDeterministic(t *testing.T) {
	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	SetNowFunc(func() time.Time { return fixed })
	t.Cleanup(func() { SetNowFunc(time.Now) })

	active := &Override{
		Alias:      "x",
		Multiplier: 2.0,
		ExpiresAt:  fixed.Add(1 * time.Hour),
		Reason:     "demo",
	}
	if got := ApplyOverride(Weight(1.0), active); got != Weight(2.0) {
		t.Errorf("active under fixed clock: got %v, want 2.0", got)
	}

	expired := &Override{
		Alias:      "x",
		Multiplier: 2.0,
		ExpiresAt:  fixed.Add(-1 * time.Hour),
		Reason:     "stale",
	}
	if got := ApplyOverride(Weight(1.0), expired); got != Weight(1.0) {
		t.Errorf("expired under fixed clock: got %v, want 1.0", got)
	}
}

func TestOverrideIsActiveNilReceiver(t *testing.T) {
	var ov *Override
	if ov.IsActive(time.Now()) {
		t.Error("nil Override.IsActive should return false")
	}
}

func TestOverrideIsActiveAtExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ov := &Override{
		Alias:      "x",
		Multiplier: 2.0,
		ExpiresAt:  now,
		Reason:     "edge",
	}
	if ov.IsActive(now) {
		t.Error("Override with ExpiresAt == now should be inactive (strict Before)")
	}
}
