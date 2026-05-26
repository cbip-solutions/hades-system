package compliance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/augment"
)

func TestInvZen167_SentinelInvokedFromNewPipeline(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "augment", "types.go"))
	if err != nil {
		t.Fatalf("read types.go: %v", err)
	}
	if !strings.Contains(string(src), "budgetGateRequired()") {
		t.Error("inv-zen-167 sentinel budgetGateRequired() not invoked in NewPipeline")
	}
}

func TestInvZen167_AugmentationRefusedAtCap(t *testing.T) {
	store := &p167CapStore{
		rolled: map[string]float64{
			"augmentation|internal-platform-x": 9.99,
		},
	}
	gate := augment.NewBudgetGate(store, augment.SystemClock{})

	proceed, scope, err := gate.Check(context.Background(), augment.BudgetCheckInput{
		ProjectID:       "internal-platform-x",
		Doctrine:        "max-scope",
		RequestedTokens: 100000,
		CapUSD:          10.0,
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if proceed {
		t.Error("inv-zen-167 violated: augmentation proceeded despite over-cap")
	}
	if !strings.HasPrefix(scope, "augmentation:") {
		t.Errorf("inv-zen-167: blockedScope format unexpected: %q", scope)
	}
}

func TestInvZen167_CommitIdempotentOnRequestID(t *testing.T) {
	store := &p167CapStore{rolled: map[string]float64{}}
	gate := augment.NewBudgetGate(store, augment.SystemClock{})

	for i := 0; i < 5; i++ {
		if err := gate.Commit(context.Background(), augment.BudgetCommitInput{
			RequestID: "dup-req",
			ProjectID: "internal-platform-x",
			Doctrine:  "max-scope",
			Tokens:    1000,
		}); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.entries) != 1 {
		t.Errorf("inv-zen-167 idempotency: want 1 ledger entry, got %d", len(store.entries))
	}
}

type p167CapStore struct {
	mu      sync.Mutex
	rolled  map[string]float64
	entries []augment.CostLedgerEntry
	seen    map[string]bool
}

func (c *p167CapStore) RolledUSDByAxis(_ context.Context, axis, value string, _ int64) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rolled[axis+"|"+value], nil
}
func (c *p167CapStore) InsertCostLedgerEntry(_ context.Context, e augment.CostLedgerEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if c.seen[e.RequestID] {
		return nil
	}
	c.seen[e.RequestID] = true
	c.entries = append(c.entries, e)
	return nil
}
