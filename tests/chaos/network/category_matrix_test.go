//go:build chaos

package network

import (
	"context"
	"testing"
	"time"
)

func TestToxiproxyCategorySubMatrices(t *testing.T) {
	reg, err := LoadRegistryForTest()
	if err != nil {
		t.Skipf("Toxiproxy not available (run scripts/setup_toxiproxy_dev.sh); skipping: %v", err)
	}
	all := GenerateScenarios(reg)
	if got := len(all); got != 80 {
		t.Fatalf("scenario count = %d, want 80 (10 toxics × 8 edges)", got)
	}
	wantPerCat := map[Category]int{
		CategoryClose:   3 * len(reg.Edges),
		CategoryDegrade: 4 * len(reg.Edges),
		CategoryCorrupt: 3 * len(reg.Edges),
	}
	runner := NewRunner(reg)
	for _, cat := range AllCategories() {
		cat := cat
		t.Run(cat.String(), func(t *testing.T) {
			sub := FilterByCategory(all, cat)
			if got, want := len(sub), wantPerCat[cat]; got != want {
				t.Fatalf("%s sub-matrix = %d scenarios, want %d", cat, got, want)
			}
			for _, s := range sub {
				s := s
				t.Run(s.String(), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()

					if err := r2Run(ctx, runner, s); err != nil {
						t.Errorf("scenario %s: %v", s, err)
					}
				})
			}
		})
	}
}

func r2Run(ctx context.Context, r *Runner, s Scenario) error {
	if err := r.applyToxic(ctx, s); err != nil {
		return err
	}
	defer func() { _ = r.clearToxics(ctx, s.Edge) }()
	return AssertEdgeInvariantV2(ctx, r.reg, s)
}
