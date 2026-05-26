package orchestrator

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type pinStoreAdapter struct {
	s *store.Store
}

func newPinStoreAdapter(s *store.Store) *pinStoreAdapter {
	return &pinStoreAdapter{s: s}
}

func (a *pinStoreAdapter) Insert(p PinRow) error {
	return a.s.InsertPin(store.PinRow{
		ID:        p.ID,
		Scope:     p.Scope,
		ScopeID:   p.ScopeID,
		Tier:      p.Tier,
		Provider:  p.Provider,
		SetAt:     p.SetAt,
		ExpiresAt: p.ExpiresAt,
		Reason:    p.Reason,
	})
}

func (a *pinStoreAdapter) Delete(scope, scopeID string) error {
	return a.s.DeletePin(scope, scopeID)
}

func (a *pinStoreAdapter) Query(scope, scopeID string) (*PinRow, error) {
	storeRow, err := a.s.QueryPin(scope, scopeID)
	if err != nil || storeRow == nil {
		return nil, err
	}
	o := PinRow{
		ID:        storeRow.ID,
		Scope:     storeRow.Scope,
		ScopeID:   storeRow.ScopeID,
		Tier:      storeRow.Tier,
		Provider:  storeRow.Provider,
		SetAt:     storeRow.SetAt,
		ExpiresAt: storeRow.ExpiresAt,
		Reason:    storeRow.Reason,
	}
	return &o, nil
}

func (a *pinStoreAdapter) ListAll() ([]PinRow, error) {
	storeRows, err := a.s.ListAllPins()
	if err != nil {
		return nil, err
	}
	if len(storeRows) == 0 {
		return nil, nil
	}
	out := make([]PinRow, len(storeRows))
	for i, r := range storeRows {
		out[i] = PinRow{
			ID:        r.ID,
			Scope:     r.Scope,
			ScopeID:   r.ScopeID,
			Tier:      r.Tier,
			Provider:  r.Provider,
			SetAt:     r.SetAt,
			ExpiresAt: r.ExpiresAt,
			Reason:    r.Reason,
		}
	}
	return out, nil
}

func (a *pinStoreAdapter) PurgeExpired(now time.Time) (int, error) {
	return a.s.PurgeExpiredPins(now)
}

type fixedCounters struct {
	sessionVal map[string]float64
	pftVal     map[string]float64
}

func newFixedCounters() *fixedCounters {
	return &fixedCounters{
		sessionVal: map[string]float64{},
		pftVal:     map[string]float64{},
	}
}

func (f *fixedCounters) SessionTotal(s string) float64 { return f.sessionVal[s] }

func (f *fixedCounters) ProjectProfileTierTotal(p, pr, t string, win time.Duration) float64 {
	var winName string
	switch win {
	case 24 * time.Hour:
		winName = "24h"
	case 30 * 24 * time.Hour:
		winName = "30d"
	default:
		return 0
	}
	return f.pftVal[p+"|"+pr+"|"+t+"|"+winName]
}

func (f *fixedCounters) setPFT(p, pr, t, win string, v float64) {
	f.pftVal[p+"|"+pr+"|"+t+"|"+win] = v
}

type captureNotifier struct {
	info     []string
	warn     []string
	critical []string
}

func (c *captureNotifier) NotifyINFO(title, body, source string) {
	c.info = append(c.info, title+"|"+body+"|"+source)
}

func (c *captureNotifier) NotifyWARN(title, body, source string) {
	c.warn = append(c.warn, title+"|"+body+"|"+source)
}

func (c *captureNotifier) NotifyCRITICAL(title, body, source string) {
	c.critical = append(c.critical, title+"|"+body+"|"+source)
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestInvZen063_CapWinsOverPin(t *testing.T) {

	s := openTestStore(t)
	adapter := newPinStoreAdapter(s)

	pins := NewPinOverrides(adapter)
	if err := pins.Set("project", "internal-platform-x", "tier_openclaude", "", time.Hour, "integration-test pin"); err != nil {
		t.Fatalf("PinOverrides.Set: %v", err)
	}

	row, err := pins.Resolve("", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if row == nil {
		t.Fatal("Resolve returned nil; expected pinned tier_openclaude")
	}
	if row.Tier != "tier_openclaude" {
		t.Errorf("Resolve.Tier = %q, want %q", row.Tier, "tier_openclaude")
	}

	counters := newFixedCounters()
	counters.setPFT("internal-platform-x", "orchestrator", "tier_openclaude", "30d", 99.0)

	notifier := &captureNotifier{}
	safety := NewPaygSafety(PaygSafetyOptions{
		Counters: counters,
		Notifier: notifier,
	})

	effective := ProfileEffective{
		PerMonthUSD:  100.0,
		OnCapReached: ModePauseDescriptive,
	}

	capErr := safety.CheckCap("internal-platform-x", "orchestrator", "tier_openclaude", "", 5.0, effective)
	if !errors.Is(capErr, ErrCapWillExceed) {
		t.Fatalf("CheckCap: got %v, want ErrCapWillExceed", capErr)
	}

	handleErr := safety.HandleCapReached("internal-platform-x", "orchestrator", "tier_openclaude", ModePauseDescriptive)
	if !errors.Is(handleErr, ErrTierPausedDescriptive) {
		t.Fatalf("HandleCapReached: got %v, want ErrTierPausedDescriptive", handleErr)
	}

	rowAfter, err := pins.Resolve("", "internal-platform-x")
	if err != nil {
		t.Fatalf("Resolve (post cap): %v", err)
	}
	if rowAfter == nil {
		t.Fatal("pin was destroyed by cap policy (must be non-destructive)")
	}
	if rowAfter.Tier != "tier_openclaude" {
		t.Errorf("post-cap Resolve.Tier = %q, want %q", rowAfter.Tier, "tier_openclaude")
	}

	if len(notifier.critical) < 1 {
		t.Errorf("critical notifications = %d, want ≥1 (HandleCapReached must notify)", len(notifier.critical))
	}
}
