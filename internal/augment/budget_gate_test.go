package augment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type fakeBudgetStore struct {
	mu         sync.Mutex
	rolled     map[string]float64
	ledger     []augment.CostLedgerEntry
	seenReqIDs map[string]bool
	rolledErr  error
	insertErr  error
}

func newFakeBudgetStore() *fakeBudgetStore {
	return &fakeBudgetStore{
		rolled:     make(map[string]float64),
		seenReqIDs: make(map[string]bool),
	}
}

func (f *fakeBudgetStore) RolledUSDByAxis(_ context.Context, axisName, axisValue string, _ int64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rolledErr != nil {
		return 0, f.rolledErr
	}
	return f.rolled[axisName+"|"+axisValue], nil
}

func (f *fakeBudgetStore) InsertCostLedgerEntry(_ context.Context, entry augment.CostLedgerEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return f.insertErr
	}
	if f.seenReqIDs[entry.RequestID] {
		return nil
	}
	f.seenReqIDs[entry.RequestID] = true
	f.ledger = append(f.ledger, entry)
	return nil
}

func (f *fakeBudgetStore) setRolled(axisName, axisValue string, usd float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rolled[axisName+"|"+axisValue] = usd
}

func (f *fakeBudgetStore) ledgerSnapshot() []augment.CostLedgerEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]augment.CostLedgerEntry, len(f.ledger))
	copy(out, f.ledger)
	return out
}

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func TestBudgetGate_ProceedsUnderCap(t *testing.T) {
	store := newFakeBudgetStore()
	store.setRolled("augmentation", "internal-platform-x", 1.50)
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	proceed, blockedScope, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "internal-platform-x",
		Doctrine:        "max-scope",
		RequestedTokens: 5000,
		CapUSD:          10.0,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !proceed {
		t.Fatalf("expected proceed=true; blockedScope=%q", blockedScope)
	}
	if blockedScope != "" {
		t.Fatalf("blockedScope: want empty, got %q", blockedScope)
	}
}

func TestBudgetGate_BlocksOverCap(t *testing.T) {
	store := newFakeBudgetStore()
	store.setRolled("augmentation", "internal-platform-x", 9.95)
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	proceed, blockedScope, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "internal-platform-x",
		Doctrine:        "max-scope",
		RequestedTokens: 50000,
		CapUSD:          10.0,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if proceed {
		t.Fatal("expected proceed=false (over cap)")
	}
	if blockedScope != "augmentation:internal-platform-x" {
		t.Fatalf("blockedScope: want augmentation:internal-platform-x, got %q", blockedScope)
	}
}

func TestBudgetGate_CommitWritesCostLedgerEntry(t *testing.T) {
	store := newFakeBudgetStore()
	clk := fixedClock{t: time.Unix(1715000000, 0)}
	gate := augment.NewBudgetGate(store, clk)

	err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "req-abc-123",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		Tokens:    5000,
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	entries := store.ledgerSnapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(entries))
	}
	got := entries[0]
	if got.RequestID != "req-abc-123" {
		t.Errorf("RequestID: want req-abc-123, got %q", got.RequestID)
	}
	if got.ProjectID != "internal-platform-x" {
		t.Errorf("ProjectID: want internal-platform-x, got %q", got.ProjectID)
	}
	if got.Doctrine != "max-scope" {
		t.Errorf("Doctrine: want max-scope, got %q", got.Doctrine)
	}
	if got.Tokens != 5000 {
		t.Errorf("Tokens: want 5000, got %d", got.Tokens)
	}
	wantUSD := float64(5000) * augment.PerTokenUSDDefault
	if got.USD != wantUSD {
		t.Errorf("USD: want %.20f, got %.20f", wantUSD, got.USD)
	}
	if got.EmittedAt != clk.t.UnixMilli() {
		t.Errorf("EmittedAt: want %d, got %d", clk.t.UnixMilli(), got.EmittedAt)
	}
}

func TestBudgetGate_CommitIdempotentOnRequestID(t *testing.T) {
	store := newFakeBudgetStore()
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	if err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "req-dup",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		Tokens:    5000,
	}); err != nil {
		t.Fatalf("Commit 1: %v", err)
	}

	if err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "req-dup",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		Tokens:    5000,
	}); err != nil {
		t.Fatalf("Commit 2 (idempotent): %v", err)
	}

	entries := store.ledgerSnapshot()
	if len(entries) != 1 {
		t.Fatalf("expected idempotency: 1 ledger entry, got %d", len(entries))
	}
}

func TestBudgetGate_CheckHandlesRolledError(t *testing.T) {
	store := newFakeBudgetStore()
	store.rolledErr = errors.New("db down")
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	_, _, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "internal-platform-x",
		Doctrine:        "max-scope",
		RequestedTokens: 5000,
		CapUSD:          10.0,
	})
	if err == nil || !contains(err.Error(), "db down") {
		t.Fatalf("expected store error to propagate, got %v", err)
	}
}

func TestBudgetGate_CommitHandlesInsertError(t *testing.T) {
	store := newFakeBudgetStore()
	store.insertErr = errors.New("disk full")
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "req-1",
		ProjectID: "internal-platform-x",
		Doctrine:  "max-scope",
		Tokens:    5000,
	})
	if err == nil || !contains(err.Error(), "disk full") {
		t.Fatalf("expected insert error to propagate, got %v", err)
	}
}

func TestBudgetGate_CheckRejectsZeroTokens(t *testing.T) {
	store := newFakeBudgetStore()
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	_, _, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "internal-platform-x",
		Doctrine:        "max-scope",
		RequestedTokens: 0,
		CapUSD:          10.0,
	})
	if err == nil || !contains(err.Error(), "RequestedTokens") {
		t.Fatalf("expected error for RequestedTokens=0, got %v", err)
	}
}

func TestBudgetGate_CheckRejectsNegativeCap(t *testing.T) {
	store := newFakeBudgetStore()
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})

	_, _, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "internal-platform-x",
		Doctrine:        "max-scope",
		RequestedTokens: 5000,
		CapUSD:          -1.0,
	})
	if err == nil || !contains(err.Error(), "CapUSD") {
		t.Fatalf("expected error for CapUSD<0, got %v", err)
	}
}

func TestBudgetGate_CommitMissingRequestID(t *testing.T) {
	store := newFakeBudgetStore()
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})
	err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "",
		ProjectID: "p",
		Doctrine:  "d",
		Tokens:    100,
	})
	if err == nil || !contains(err.Error(), "RequestID required") {
		t.Fatalf("expected RequestID error, got %v", err)
	}
}

func TestBudgetGate_CommitZeroTokens(t *testing.T) {
	store := newFakeBudgetStore()
	gate := augment.NewBudgetGate(store, fixedClock{t: time.Unix(1715000000, 0)})
	err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "x",
		ProjectID: "p",
		Doctrine:  "d",
		Tokens:    0,
	})
	if err == nil || !contains(err.Error(), "Tokens must be > 0") {
		t.Fatalf("expected Tokens=0 error, got %v", err)
	}
}

func TestNewBudgetGate_NilClockDefaultsToSystemClock(t *testing.T) {
	store := newFakeBudgetStore()
	gate := augment.NewBudgetGate(store, nil)
	if gate == nil {
		t.Fatal("nil gate")
	}

	if err := gate.Commit(context.Background(), augment.BudgetCommitInput{
		RequestID: "x",
		ProjectID: "p",
		Doctrine:  "d",
		Tokens:    100,
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
