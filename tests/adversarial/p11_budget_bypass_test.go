//go:build adversarial

package adversarial

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

type budgetStoreFake struct {
	mu         sync.Mutex
	rolledUSD  float64
	ledger     []augment.CostLedgerEntry
	storeErr   error
	insertSeen map[string]bool
}

func newBudgetStoreFake() *budgetStoreFake {
	return &budgetStoreFake{insertSeen: map[string]bool{}}
}

func (b *budgetStoreFake) RolledUSDByAxis(_ context.Context, _ string, _ string, _ int64) (float64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.storeErr != nil {
		return 0, b.storeErr
	}
	return b.rolledUSD, nil
}

func (b *budgetStoreFake) InsertCostLedgerEntry(_ context.Context, e augment.CostLedgerEntry) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.insertSeen[e.RequestID] {
		return errors.New("duplicate RequestID")
	}
	b.insertSeen[e.RequestID] = true
	b.ledger = append(b.ledger, e)
	return nil
}

func TestAdversarial_BudgetBypass_TokenBomb(t *testing.T) {
	store := newBudgetStoreFake()
	gate := augment.NewBudgetGate(store, nil)

	proceed, scope, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "p-attack",
		Doctrine:        "default",
		RequestedTokens: 10_000_000,
		CapUSD:          1.0,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if proceed {
		t.Error("budget bypass: proceed=true for token-bomb request, want false")
	}
	if scope == "" {
		t.Error("budget bypass: blockedScope=\"\", want non-empty")
	}
}

func TestAdversarial_BudgetBypass_ZeroTokensRejected(t *testing.T) {
	store := newBudgetStoreFake()
	gate := augment.NewBudgetGate(store, nil)

	proceed, _, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "p",
		Doctrine:        "default",
		RequestedTokens: 0,
		CapUSD:          100.0,
	})
	if err == nil {
		t.Error("zero-token bypass: expected validation error, got nil")
	}
	if proceed {
		t.Error("zero-token bypass: proceed=true with 0 tokens, want false")
	}
}

func TestAdversarial_BudgetBypass_NegativeCapRejected(t *testing.T) {
	store := newBudgetStoreFake()
	gate := augment.NewBudgetGate(store, nil)

	proceed, _, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "p",
		Doctrine:        "default",
		RequestedTokens: 100,
		CapUSD:          -1.0,
	})
	if err == nil {
		t.Error("negative-cap bypass: expected validation error, got nil")
	}
	if proceed {
		t.Error("negative-cap bypass: proceed=true with CapUSD<0, want false")
	}
}

func TestAdversarial_BudgetBypass_DuplicateCommitIdempotent(t *testing.T) {
	store := newBudgetStoreFake()
	gate := augment.NewBudgetGate(store, fixedClock{time.Unix(1700000000, 0)})

	commit := augment.BudgetCommitInput{
		RequestID: "rq-dup",
		ProjectID: "p",
		Doctrine:  "default",
		Tokens:    1000,
	}
	if err := gate.Commit(context.Background(), commit); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	if err := gate.Commit(context.Background(), commit); err == nil {
		t.Error("duplicate commit: succeeded, want duplicate-RequestID error")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.ledger) != 1 {
		t.Errorf("ledger len = %d, want 1 (idempotency broken)", len(store.ledger))
	}
}

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time { return f.t }
