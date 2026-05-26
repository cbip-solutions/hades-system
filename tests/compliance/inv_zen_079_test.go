package compliance

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/budget"
)

type prec79Store struct {
	rolled map[string]float64
	paused map[string]bool
}

func newPrec79Store() *prec79Store {
	return &prec79Store{rolled: map[string]float64{}, paused: map[string]bool{}}
}

func (f *prec79Store) InsertCostAxisTag(context.Context, int64, string, string) error { return nil }
func (f *prec79Store) EmitAxisTagLoss(context.Context, int64, string) error           { return nil }
func (f *prec79Store) QueryAxisTags(context.Context, int64) (map[string]string, error) {
	return nil, nil
}
func (f *prec79Store) QueryCostIDsByAxis(context.Context, string, string) ([]int64, error) {
	return nil, nil
}
func (f *prec79Store) QueryAxisTagLosses(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (f *prec79Store) PauseGet(_ context.Context, scope, val string) (bool, int64, error) {
	return f.paused[scope+":"+val], 0, nil
}
func (f *prec79Store) PauseSet(_ context.Context, scope, val, _ string, _, _ int64) error {
	f.paused[scope+":"+val] = true
	return nil
}
func (f *prec79Store) PauseClear(_ context.Context, scope, val string) error {
	delete(f.paused, scope+":"+val)
	return nil
}
func (f *prec79Store) PauseClearIfExpired(_ context.Context, scope, val string, _ int64) error {
	delete(f.paused, scope+":"+val)
	return nil
}
func (f *prec79Store) PauseListActive(context.Context) ([]budget.PauseRow, error) {
	return nil, nil
}
func (f *prec79Store) AnomalyAppend(context.Context, budget.AnomalyRow) error { return nil }
func (f *prec79Store) AnomalyWindow(context.Context, string, string, int) ([]float64, error) {
	return nil, nil
}
func (f *prec79Store) RolledUSDByAxis(_ context.Context, axis, val string, _ int64) (float64, error) {
	return f.rolled[axis+":"+val], nil
}

var defaultScopes79 = budget.Scopes{
	Project: "internal-platform-x", Doctrine: "max-scope", Stage: "design", Worker: "w-42",
}
var defaultCaps79 = budget.Caps{Project: 10, Doctrine: 25, Stage: 5, Worker: 2}

func TestInvZen079_AllFourBlockedSortedTightestFirst(t *testing.T) {
	s := newPrec79Store()
	s.rolled["project:internal-platform-x"] = 9.99
	s.rolled["doctrine:max-scope"] = 24.99
	s.rolled["stage:design"] = 4.99
	s.rolled["worker_id:w-42"] = 1.99
	g := budget.NewGate(s)
	d, err := g.Check(context.Background(), defaultScopes79, defaultCaps79, 0.05)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	want := []string{"worker_id", "stage", "doctrine", "project"}
	if len(d.BlockedScopes) != 4 {
		t.Fatalf("len(BlockedScopes) = %d, want 4", len(d.BlockedScopes))
	}
	for i, w := range want {
		if d.BlockedScopes[i] != w {
			t.Errorf("BlockedScopes[%d] = %q, want %q", i, d.BlockedScopes[i], w)
		}
	}
}

func TestInvZen079_PauseAndCapMixedScopes(t *testing.T) {
	s := newPrec79Store()

	s.paused["project:internal-platform-x"] = true
	s.rolled["stage:design"] = 4.99
	g := budget.NewGate(s)
	d, _ := g.Check(context.Background(), defaultScopes79, defaultCaps79, 0.05)
	if len(d.BlockedScopes) != 2 {
		t.Fatalf("len(BlockedScopes) = %d, want 2", len(d.BlockedScopes))
	}

	if d.BlockedScopes[0] != "stage" || d.BlockedScopes[1] != "project" {
		t.Errorf("BlockedScopes = %v, want [stage project]", d.BlockedScopes)
	}
}

func TestInvZen079_OnlyWorkerBlocked(t *testing.T) {
	s := newPrec79Store()
	s.rolled["worker_id:w-42"] = 1.99
	g := budget.NewGate(s)
	d, _ := g.Check(context.Background(), defaultScopes79, defaultCaps79, 0.05)
	if len(d.BlockedScopes) != 1 || d.BlockedScopes[0] != "worker_id" {
		t.Errorf("BlockedScopes = %v, want [worker_id]", d.BlockedScopes)
	}
}

func TestInvZen079_NoneBlockedAllowedTrue(t *testing.T) {
	s := newPrec79Store()
	g := budget.NewGate(s)
	d, _ := g.Check(context.Background(), defaultScopes79, defaultCaps79, 0.05)
	if !d.Allowed {
		t.Errorf("Allowed = false, want true")
	}
	if len(d.BlockedScopes) != 0 {
		t.Errorf("BlockedScopes = %v, want []", d.BlockedScopes)
	}
}
