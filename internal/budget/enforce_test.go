package budget

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type fakeEnforceStore struct {
	mu          sync.Mutex
	rolled      map[string]float64
	paused      map[string]int64
	rolledErrOn string
	pauseErrOn  string
}

func newFakeEnforceStore() *fakeEnforceStore {
	return &fakeEnforceStore{rolled: map[string]float64{}, paused: map[string]int64{}}
}

func (f *fakeEnforceStore) InsertCostAxisTag(context.Context, int64, string, string) error {
	return nil
}
func (f *fakeEnforceStore) EmitAxisTagLoss(context.Context, int64, string) error { return nil }
func (f *fakeEnforceStore) QueryAxisTags(context.Context, int64) (map[string]string, error) {
	return nil, nil
}
func (f *fakeEnforceStore) QueryCostIDsByAxis(context.Context, string, string) ([]int64, error) {
	return nil, nil
}
func (f *fakeEnforceStore) QueryAxisTagLosses(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (f *fakeEnforceStore) PauseGet(_ context.Context, scope, val string) (bool, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pauseErrOn == scope {
		return false, 0, errors.New("injected pause-get error")
	}
	v, ok := f.paused[scope+":"+val]
	return ok, v, nil
}
func (f *fakeEnforceStore) PauseSet(_ context.Context, scope, val, _ string, _, autoResumeAt int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused[scope+":"+val] = autoResumeAt
	return nil
}
func (f *fakeEnforceStore) PauseClear(_ context.Context, scope, val string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.paused, scope+":"+val)
	return nil
}
func (f *fakeEnforceStore) PauseClearIfExpired(_ context.Context, scope, val string, beforeMs int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	auto, ok := f.paused[scope+":"+val]
	if !ok {
		return nil
	}
	if auto > 0 && auto <= beforeMs {
		delete(f.paused, scope+":"+val)
	}
	return nil
}
func (f *fakeEnforceStore) PauseListActive(context.Context) ([]PauseRow, error) {
	return nil, nil
}
func (f *fakeEnforceStore) AnomalyAppend(context.Context, AnomalyRow) error { return nil }
func (f *fakeEnforceStore) AnomalyWindow(context.Context, string, string, int) ([]float64, error) {
	return nil, nil
}
func (f *fakeEnforceStore) RolledUSDByAxis(_ context.Context, axis, val string, _ int64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rolledErrOn == axis {
		return 0, errors.New("injected rolled-error")
	}
	return f.rolled[axis+":"+val], nil
}

func sampleCaps() Caps {
	return Caps{
		Project:  10.00,
		Doctrine: 25.00,
		Stage:    5.00,
		Worker:   2.00,
	}
}

func sampleScopes() Scopes {
	return Scopes{
		Project:  "internal-platform-x",
		Doctrine: "max-scope",
		Stage:    "design",
		Worker:   "w-42",
	}
}

func TestCheckAllAllowedReturnsRemaining(t *testing.T) {
	store := newFakeEnforceStore()
	store.rolled["project:internal-platform-x"] = 1.00
	store.rolled["doctrine:max-scope"] = 2.00
	store.rolled["stage:design"] = 0.50
	store.rolled["worker_id:w-42"] = 0.10
	g := NewGate(store)
	d, err := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !d.Allowed {
		t.Errorf("Allowed = false, want true; BlockedScopes=%v", d.BlockedScopes)
	}
	if d.RemainingPerScope["project"] < 8.94 || d.RemainingPerScope["project"] > 8.96 {
		t.Errorf("project remaining = %f, want ~8.95", d.RemainingPerScope["project"])
	}
	if d.RemainingPerScope["worker_id"] < 1.84 || d.RemainingPerScope["worker_id"] > 1.86 {
		t.Errorf("worker_id remaining = %f, want ~1.85", d.RemainingPerScope["worker_id"])
	}
}

func TestCheckSingleScopeBlockedReturnsThatScope(t *testing.T) {
	store := newFakeEnforceStore()
	store.rolled["worker_id:w-42"] = 1.99
	g := NewGate(store)
	d, _ := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if d.Allowed {
		t.Errorf("Allowed = true, want false")
	}
	if len(d.BlockedScopes) != 1 || d.BlockedScopes[0] != "worker_id" {
		t.Errorf("BlockedScopes = %v, want [worker_id]", d.BlockedScopes)
	}
}

func TestCheckMultipleScopesBlocked_SortedMostRestrictiveFirst(t *testing.T) {
	store := newFakeEnforceStore()

	store.rolled["project:internal-platform-x"] = 9.99
	store.rolled["doctrine:max-scope"] = 24.99
	store.rolled["stage:design"] = 4.99
	store.rolled["worker_id:w-42"] = 1.99
	g := NewGate(store)
	d, _ := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if d.Allowed {
		t.Errorf("Allowed = true, want false")
	}
	want := []string{"worker_id", "stage", "doctrine", "project"}
	if len(d.BlockedScopes) != 4 {
		t.Fatalf("len(BlockedScopes) = %d, want 4", len(d.BlockedScopes))
	}
	for i, scope := range want {
		if d.BlockedScopes[i] != scope {
			t.Errorf("BlockedScopes[%d] = %q, want %q (most-restrictive-first)",
				i, d.BlockedScopes[i], scope)
		}
	}
}

func TestCheckPauseTakesPrecedenceOverCap(t *testing.T) {
	store := newFakeEnforceStore()

	store.paused["project:internal-platform-x"] = 9999999999
	g := NewGate(store)
	d, _ := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if d.Allowed {
		t.Errorf("Allowed = true, want false (project paused)")
	}
	if len(d.BlockedScopes) != 1 || d.BlockedScopes[0] != "project" {
		t.Errorf("BlockedScopes = %v, want [project]", d.BlockedScopes)
	}
}

func TestCheckPauseAndCapBlockSameScope_DedupBlocked(t *testing.T) {
	store := newFakeEnforceStore()

	store.paused["worker_id:w-42"] = 9999999999
	store.rolled["worker_id:w-42"] = 1.99
	g := NewGate(store)
	d, _ := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if d.Allowed {
		t.Errorf("Allowed = true, want false")
	}

	if len(d.BlockedScopes) != 1 || d.BlockedScopes[0] != "worker_id" {
		t.Errorf("BlockedScopes = %v, want [worker_id] (deduped)", d.BlockedScopes)
	}
}

func TestCheckEstimatedCostZero_AllowedEvenAtCapBoundary(t *testing.T) {
	store := newFakeEnforceStore()

	store.rolled["worker_id:w-42"] = 2.00
	g := NewGate(store)
	d, _ := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.0)
	if !d.Allowed {
		t.Errorf("Allowed = false, want true (at-cap with estimated=0)")
	}
}

func TestCheckEstimatedCostNegativeRejected(t *testing.T) {
	g := NewGate(newFakeEnforceStore())
	_, err := g.Check(context.Background(), sampleScopes(), sampleCaps(), -0.01)
	if !errors.Is(err, ErrInvalidEstimate) {
		t.Errorf("err = %v, want ErrInvalidEstimate", err)
	}
}

func TestCheckScopesValidationEmptyValuesRejected(t *testing.T) {
	g := NewGate(newFakeEnforceStore())
	cases := []Scopes{
		{Project: "", Doctrine: "max-scope", Stage: "design", Worker: "w-42"},
		{Project: "p", Doctrine: "", Stage: "design", Worker: "w-42"},
		{Project: "p", Doctrine: "d", Stage: "", Worker: "w-42"},
		{Project: "p", Doctrine: "d", Stage: "s", Worker: ""},
	}
	for i, bad := range cases {
		_, err := g.Check(context.Background(), bad, sampleCaps(), 0.05)
		if !errors.Is(err, ErrInvalidScopes) {
			t.Errorf("case %d: err = %v, want ErrInvalidScopes", i, err)
		}
	}
}

func TestCheckRolledUSDErrorPropagated(t *testing.T) {
	store := newFakeEnforceStore()
	store.rolledErrOn = "stage"
	g := NewGate(store)
	_, err := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if err == nil {
		t.Error("err = nil, want injected rolled-error wrap")
	}
}

func TestCheckPauseGetErrorPropagated(t *testing.T) {
	store := newFakeEnforceStore()
	store.pauseErrOn = "doctrine"
	g := NewGate(store)
	_, err := g.Check(context.Background(), sampleScopes(), sampleCaps(), 0.05)
	if err == nil {
		t.Error("err = nil, want injected pause-error wrap")
	}
}

func TestScopePrecedenceMostRestrictiveOrdering(t *testing.T) {

	pairs := []struct {
		a, b   string
		expect bool
	}{
		{"worker_id", "stage", true},
		{"stage", "doctrine", true},
		{"doctrine", "project", true},
		{"project", "worker_id", false},
		{"worker_id", "project", true},
		{"unknown", "worker_id", false},
	}
	for _, p := range pairs {
		got := scopePrecedence(p.a) < scopePrecedence(p.b)
		if got != p.expect {
			t.Errorf("scopePrecedence(%q) < scopePrecedence(%q) = %v, want %v",
				p.a, p.b, got, p.expect)
		}
	}
}

func TestHierarchicalPrecedenceAnchorPresent(t *testing.T) {
	if v := hierarchicalPrecedence(); v != 4 {
		t.Errorf("hierarchicalPrecedence = %d, want 4 (4 scopes)", v)
	}
}

func TestPreCallEnforcedBeforeUpstreamAnchorPresent(t *testing.T) {
	if !preCallEnforcedBeforeUpstream() {
		t.Error("preCallEnforcedBeforeUpstream must return true")
	}
}

func TestNewGateNilStorePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewGate(nil)
}

func TestSetRollupWindow(t *testing.T) {
	g := NewGate(newFakeEnforceStore())
	g.SetRollupWindow(7 * 24 * 3600 * 1e9)

}

func TestSetRollupWindowZeroOrNegativePanics(t *testing.T) {
	g := NewGate(newFakeEnforceStore())
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	g.SetRollupWindow(0)
}

func TestSetRollupWindowNegativePanics(t *testing.T) {
	g := NewGate(newFakeEnforceStore())
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	g.SetRollupWindow(-1)
}
