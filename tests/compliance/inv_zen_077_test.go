package compliance

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/budget"
)

type complianceFakeStore struct {
	mu     sync.Mutex
	tags   map[int64]map[string]string
	losses map[int64][]string
}

func newComplianceFakeStore() *complianceFakeStore {
	return &complianceFakeStore{
		tags:   map[int64]map[string]string{},
		losses: map[int64][]string{},
	}
}

func (f *complianceFakeStore) InsertCostAxisTag(_ context.Context, id int64, n, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tags[id]; !ok {
		f.tags[id] = map[string]string{}
	}
	if _, exists := f.tags[id][n]; !exists {
		f.tags[id][n] = v
	}
	return nil
}

func (f *complianceFakeStore) EmitAxisTagLoss(_ context.Context, id int64, axis string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.losses[id] = append(f.losses[id], axis)
	return nil
}

func (f *complianceFakeStore) QueryAxisTags(_ context.Context, id int64) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.tags[id] {
		out[k] = v
	}
	return out, nil
}

func (f *complianceFakeStore) QueryCostIDsByAxis(_ context.Context, n, v string) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []int64
	for id, m := range f.tags {
		if m[n] == v {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *complianceFakeStore) QueryAxisTagLosses(_ context.Context, id int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.losses[id]...), nil
}

func (f *complianceFakeStore) PauseGet(context.Context, string, string) (bool, int64, error) {
	return false, 0, nil
}
func (f *complianceFakeStore) PauseSet(context.Context, string, string, string, int64, int64) error {
	return nil
}
func (f *complianceFakeStore) PauseClear(context.Context, string, string) error { return nil }
func (f *complianceFakeStore) PauseClearIfExpired(context.Context, string, string, int64) error {
	return nil
}
func (f *complianceFakeStore) PauseListActive(context.Context) ([]budget.PauseRow, error) {
	return nil, nil
}
func (f *complianceFakeStore) AnomalyAppend(context.Context, budget.AnomalyRow) error {
	return nil
}
func (f *complianceFakeStore) AnomalyWindow(context.Context, string, string, int) ([]float64, error) {
	return nil, nil
}
func (f *complianceFakeStore) RolledUSDByAxis(context.Context, string, string, int64) (float64, error) {
	return 0, nil
}

func TestInvZen077_AxisTagCompletenessAt1kSamples(t *testing.T) {
	store := newComplianceFakeStore()
	tagger := budget.NewAxisTagger(store)
	r := rand.New(rand.NewSource(42))
	required := budget.RequiredAxes()
	const N = 1000
	const stripPct = 0.05

	for i := int64(1); i <= N; i++ {
		axisTags := map[string]string{
			"project":  "internal-platform-x",
			"doctrine": "max-scope",
			"stage":    "design",
			"task":     "T-12",
		}

		if r.Float64() < stripPct {
			drop := required[r.Intn(len(required))]
			delete(axisTags, drop)
		}
		err := tagger.Tag(context.Background(), i, axisTags)
		if err != nil && !errors.Is(err, budget.ErrAxisIncomplete) {
			t.Fatalf("cost_id=%d unexpected err: %v", i, err)
		}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	for id := int64(1); id <= N; id++ {
		nTags := 0
		for _, axis := range required {
			if _, ok := store.tags[id][axis]; ok {
				nTags++
			}
		}
		nLoss := len(store.losses[id])
		if nTags+nLoss != 4 {
			t.Errorf("cost_id=%d violates inv-zen-077: tags=%d loss=%d (want sum=4)",
				id, nTags, nLoss)
		}
	}
}

func TestInvZen077_NoSilentDrop(t *testing.T) {
	store := newComplianceFakeStore()
	tagger := budget.NewAxisTagger(store)
	axisTags := map[string]string{
		"project": "internal-platform-x",

		"stage": "design",
		"task":  "T-12",
	}
	err := tagger.Tag(context.Background(), 7, axisTags)
	if !errors.Is(err, budget.ErrAxisIncomplete) {
		t.Fatalf("err = %v, want ErrAxisIncomplete", err)
	}
	if len(store.losses[7]) != 1 {
		t.Fatalf("losses = %d, want 1", len(store.losses[7]))
	}
	if store.losses[7][0] != "doctrine" {
		t.Errorf("losses[0] = %q, want doctrine", store.losses[7][0])
	}
}

func TestInvZen077_AllRequiredAxesAreNamedConsistently(t *testing.T) {
	got := budget.RequiredAxes()
	want := []string{"doctrine", "project", "stage", "task"}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
