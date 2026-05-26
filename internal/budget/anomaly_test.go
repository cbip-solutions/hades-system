package budget

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
)

type fakeAnomalyStore struct {
	mu      sync.Mutex
	windows map[string][]float64
	rows    []AnomalyRow
	winErr  bool
	appErr  bool
}

func newFakeAnomalyStore() *fakeAnomalyStore {
	return &fakeAnomalyStore{windows: map[string][]float64{}}
}

func (f *fakeAnomalyStore) InsertCostAxisTag(context.Context, int64, string, string) error {
	return nil
}
func (f *fakeAnomalyStore) EmitAxisTagLoss(context.Context, int64, string) error { return nil }
func (f *fakeAnomalyStore) QueryAxisTags(context.Context, int64) (map[string]string, error) {
	return nil, nil
}
func (f *fakeAnomalyStore) QueryCostIDsByAxis(context.Context, string, string) ([]int64, error) {
	return nil, nil
}
func (f *fakeAnomalyStore) QueryAxisTagLosses(context.Context, int64) ([]string, error) {
	return nil, nil
}
func (f *fakeAnomalyStore) PauseGet(context.Context, string, string) (bool, int64, error) {
	return false, 0, nil
}
func (f *fakeAnomalyStore) PauseSet(context.Context, string, string, string, int64, int64) error {
	return nil
}
func (f *fakeAnomalyStore) PauseClear(context.Context, string, string) error { return nil }
func (f *fakeAnomalyStore) PauseClearIfExpired(context.Context, string, string, int64) error {
	return nil
}
func (f *fakeAnomalyStore) PauseListActive(context.Context) ([]PauseRow, error) {
	return nil, nil
}
func (f *fakeAnomalyStore) AnomalyAppend(_ context.Context, r AnomalyRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appErr {
		return errors.New("injected anomaly-append error")
	}
	f.rows = append(f.rows, r)
	return nil
}
func (f *fakeAnomalyStore) AnomalyWindow(_ context.Context, scope, val string, limit int) ([]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.winErr {
		return nil, errors.New("injected anomaly-window error")
	}
	w := f.windows[scope+":"+val]
	if limit > 0 && len(w) > limit {
		return append([]float64{}, w[len(w)-limit:]...), nil
	}
	return append([]float64{}, w...), nil
}
func (f *fakeAnomalyStore) RolledUSDByAxis(context.Context, string, string, int64) (float64, error) {
	return 0, nil
}

func (f *fakeAnomalyStore) seed(scope, val string, samples ...float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows[scope+":"+val] = append(f.windows[scope+":"+val], samples...)
}

func TestComputeZScoreEmptyWindowReturnsTooSmall(t *testing.T) {
	score, err := ComputeZScore([]float64{}, 5.0)
	if !errors.Is(err, ErrWindowTooSmall) {
		t.Errorf("err = %v, want ErrWindowTooSmall", err)
	}
	if score != 0 {
		t.Errorf("score = %f, want 0 (empty window)", score)
	}
}

func TestComputeZScoreSingleSampleReturnsTooSmall(t *testing.T) {
	score, err := ComputeZScore([]float64{1.0}, 1.5)
	if !errors.Is(err, ErrWindowTooSmall) {
		t.Errorf("err = %v, want ErrWindowTooSmall", err)
	}
	if score != 0 {
		t.Errorf("score = %f, want 0 (cannot compute std with N=1)", score)
	}
}

func TestComputeZScoreUniformSamplesZeroStdReturnsZeroVariance(t *testing.T) {
	score, err := ComputeZScore([]float64{1, 1, 1, 1, 1}, 1.0)
	if !errors.Is(err, ErrWindowZeroVariance) {
		t.Errorf("err = %v, want ErrWindowZeroVariance", err)
	}
	if score != 0 {
		t.Errorf("score = %f, want 0 (zero variance)", score)
	}
}

func TestComputeZScoreKnownDistribution(t *testing.T) {

	window := []float64{1, 2, 3, 4, 5}
	got, err := ComputeZScore(window, 10.0)
	if err != nil {
		t.Fatalf("ComputeZScore: %v", err)
	}
	want := (10.0 - 3.0) / math.Sqrt(2.5)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("got %.6f, want %.6f", got, want)
	}
}

func TestComputeZScoreDeterministicAcrossPermutations(t *testing.T) {

	a := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	b := []float64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	c := []float64{5, 1, 9, 2, 8, 3, 7, 4, 6, 10}
	za, _ := ComputeZScore(a, 20.0)
	zb, _ := ComputeZScore(b, 20.0)
	zc, _ := ComputeZScore(c, 20.0)
	if math.Abs(za-zb) > 1e-9 || math.Abs(za-zc) > 1e-9 {
		t.Errorf("permutations diverge: a=%.9f b=%.9f c=%.9f", za, zb, zc)
	}
}

func TestUpdateAndCheckBelowThresholdNoTrigger(t *testing.T) {
	store := newFakeAnomalyStore()
	store.seed("stage", "design", 1, 1.05, 0.95, 1.02, 0.98, 1.01)
	det := NewAnomalyDetector(store, 4.0, 60)
	res, err := det.Update(context.Background(), "stage", "design", 1.03)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if res.Triggered {
		t.Errorf("Triggered = true, want false (sample is in-distribution)")
	}
	if res.ZScore > 1.0 || res.ZScore < -1.0 {
		t.Errorf("ZScore = %f, want |z| < 1", res.ZScore)
	}
}

func TestUpdateAndCheckAboveThresholdTriggers(t *testing.T) {
	store := newFakeAnomalyStore()

	store.seed("worker_id", "w-42", 1, 1.01, 0.99, 1.0, 0.98, 1.02, 0.97, 1.03, 0.98, 1.02)
	det := NewAnomalyDetector(store, 4.0, 60)

	res, _ := det.Update(context.Background(), "worker_id", "w-42", 50.0)
	if !res.Triggered {
		t.Errorf("Triggered = false, want true (z=%f > 4.0)", res.ZScore)
	}
	if res.ZScore < 4.0 {
		t.Errorf("ZScore = %f, want > 4.0", res.ZScore)
	}
}

func TestUpdateAppendsAnomalyRowOnTrigger(t *testing.T) {
	store := newFakeAnomalyStore()
	store.seed("stage", "design", 1, 1.01, 0.99, 1.0, 0.98, 1.02, 0.97, 1.03, 0.98, 1.02)
	det := NewAnomalyDetector(store, 4.0, 60)
	_, _ = det.Update(context.Background(), "stage", "design", 50.0)
	if len(store.rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(store.rows))
	}
	if store.rows[0].Scope != "stage" || store.rows[0].ScopeValue != "design" {
		t.Errorf("row scope = %q:%q, want stage:design",
			store.rows[0].Scope, store.rows[0].ScopeValue)
	}
}

func TestUpdateNoTriggerNoAnomalyRow(t *testing.T) {
	store := newFakeAnomalyStore()
	store.seed("stage", "design", 1, 1.05, 0.95, 1.02, 0.98)
	det := NewAnomalyDetector(store, 4.0, 60)
	_, _ = det.Update(context.Background(), "stage", "design", 1.03)
	if len(store.rows) != 0 {
		t.Errorf("len(rows) = %d, want 0", len(store.rows))
	}
}

func TestUpdateRejectsEmptyScope(t *testing.T) {
	det := NewAnomalyDetector(newFakeAnomalyStore(), 4.0, 60)
	if _, err := det.Update(context.Background(), "", "design", 1.0); err == nil {
		t.Error("err = nil, want error on empty scope")
	}
	if _, err := det.Update(context.Background(), "stage", "", 1.0); err == nil {
		t.Error("err = nil, want error on empty scope_value")
	}
}

func TestUpdateRejectsNaNSample(t *testing.T) {
	det := NewAnomalyDetector(newFakeAnomalyStore(), 4.0, 60)
	_, err := det.Update(context.Background(), "stage", "design", math.NaN())
	if !errors.Is(err, ErrAnomalyNaN) {
		t.Errorf("err = %v, want ErrAnomalyNaN", err)
	}
}

func TestUpdateWindowErrorPropagated(t *testing.T) {
	store := newFakeAnomalyStore()
	store.winErr = true
	det := NewAnomalyDetector(store, 4.0, 60)
	_, err := det.Update(context.Background(), "stage", "design", 1.0)
	if err == nil {
		t.Error("err = nil, want injected window error")
	}
}

func TestUpdateAppendErrorPropagated(t *testing.T) {
	store := newFakeAnomalyStore()
	store.seed("stage", "design", 1, 1.01, 0.99, 1.0, 0.98)
	store.appErr = true
	det := NewAnomalyDetector(store, 4.0, 60)
	_, err := det.Update(context.Background(), "stage", "design", 50.0)
	if err == nil {
		t.Error("err = nil, want injected anomaly-append error")
	}
}

func TestNewAnomalyDetectorRejectsInvalidThreshold(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewAnomalyDetector(newFakeAnomalyStore(), -1.0, 60)
}

func TestNewAnomalyDetectorRejectsZeroThreshold(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewAnomalyDetector(newFakeAnomalyStore(), 0, 60)
}

func TestNewAnomalyDetectorRejectsZeroWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewAnomalyDetector(newFakeAnomalyStore(), 4.0, 0)
}

func TestNewAnomalyDetectorNilStorePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewAnomalyDetector(nil, 4.0, 60)
}

func TestComputeZScoreRejectsNaNInput(t *testing.T) {
	_, err := ComputeZScore([]float64{1, math.NaN(), 3}, 5.0)
	if !errors.Is(err, ErrAnomalyNaN) {
		t.Errorf("err = %v, want ErrAnomalyNaN", err)
	}
}

func TestComputeZScoreRejectsNaNSample(t *testing.T) {
	_, err := ComputeZScore([]float64{1, 2, 3}, math.NaN())
	if !errors.Is(err, ErrAnomalyNaN) {
		t.Errorf("err = %v, want ErrAnomalyNaN", err)
	}
}

func TestAnomalyDeterministicAnchorPresent(t *testing.T) {
	if !anomalyDeterministic() {
		t.Error("anomalyDeterministic must return true")
	}
}

func TestThresholdAndWindowMaxAccessors(t *testing.T) {
	det := NewAnomalyDetector(newFakeAnomalyStore(), 3.5, 120)
	if det.Threshold() != 3.5 {
		t.Errorf("Threshold = %f, want 3.5", det.Threshold())
	}
	if det.WindowMax() != 120 {
		t.Errorf("WindowMax = %d, want 120", det.WindowMax())
	}
}

func TestWelfordEmptyAndSingle(t *testing.T) {
	m, s := welford([]float64{})
	if m != 0 || s != 0 {
		t.Errorf("welford([]) = (%f,%f), want (0,0)", m, s)
	}
	m, s = welford([]float64{42})
	if m != 42 || s != 0 {
		t.Errorf("welford([42]) = (%f,%f), want (42,0)", m, s)
	}
}

func TestUpdateSameSampleDedupePostC3(t *testing.T) {
	store := newFakeAnomalyStore()
	store.seed("stage", "design", 1, 1.01, 0.99, 1.0, 0.98, 1.02, 0.97, 1.03, 0.98, 1.02)
	det := NewAnomalyDetector(store, 4.0, 60)

	r1, err := det.Update(context.Background(), "stage", "design", 50.0)
	if err != nil {
		t.Fatalf("Update 1: %v", err)
	}
	if !r1.Triggered {
		t.Fatalf("first Update Triggered = false; expected true")
	}

	// Second + third triggers with the SAME sample value MUST dedupe.
	for i := 0; i < 5; i++ {
		r, err := det.Update(context.Background(), "stage", "design", 50.0)
		if err != nil {
			t.Fatalf("Update repeat %d: %v", i, err)
		}
		if !r.Triggered {
			t.Errorf("repeat Update Triggered = false; expected true (Result still reports the observation)")
		}
	}

	if len(store.rows) != 1 {
		t.Errorf("store.rows = %d, want 1 (same-sample dedupe collapses N retries to 1 row)", len(store.rows))
	}
}

func TestUpdateDistinctSamplesEachAppendedPostC3(t *testing.T) {
	store := newFakeAnomalyStore()
	store.seed("stage", "design", 1, 1.01, 0.99, 1.0, 0.98, 1.02, 0.97, 1.03, 0.98, 1.02)
	det := NewAnomalyDetector(store, 4.0, 60)

	for _, sample := range []float64{50.0, 60.0, 70.0} {
		_, err := det.Update(context.Background(), "stage", "design", sample)
		if err != nil {
			t.Fatalf("Update %f: %v", sample, err)
		}
	}

	if len(store.rows) != 3 {
		t.Errorf("store.rows = %d, want 3 (distinct samples each appended)", len(store.rows))
	}
}

func TestStateForScopeReturnsSameInstance(t *testing.T) {
	det := NewAnomalyDetector(newFakeAnomalyStore(), 4.0, 60)
	a := det.stateForScope("stage", "design")
	b := det.stateForScope("stage", "design")
	if a != b {
		t.Error("stateForScope returned different instances for same scope")
	}
	c := det.stateForScope("stage", "other")
	if a == c {
		t.Error("stateForScope returned same instance for different scopes")
	}
}
