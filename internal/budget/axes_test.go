package budget

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeBudgetStore struct {
	mu     sync.Mutex
	tags   map[int64]map[string]string
	losses map[int64][]string
	failOn map[string]error
}

func newFakeBudgetStore() *fakeBudgetStore {
	return &fakeBudgetStore{
		tags:   map[int64]map[string]string{},
		losses: map[int64][]string{},
		failOn: map[string]error{},
	}
}

func (f *fakeBudgetStore) InsertCostAxisTag(ctx context.Context, costID int64, name, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failOn[name]; err != nil {
		return err
	}
	if _, ok := f.tags[costID]; !ok {
		f.tags[costID] = map[string]string{}
	}
	if _, exists := f.tags[costID][name]; exists {
		return nil
	}
	f.tags[costID][name] = value
	return nil
}

func (f *fakeBudgetStore) EmitAxisTagLoss(ctx context.Context, costID int64, missingAxis string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failOn["loss:"+missingAxis]; err != nil {
		return err
	}
	f.losses[costID] = append(f.losses[costID], missingAxis)
	return nil
}

func (f *fakeBudgetStore) QueryAxisTags(ctx context.Context, costID int64) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.tags[costID] {
		out[k] = v
	}
	return out, nil
}

func (f *fakeBudgetStore) QueryCostIDsByAxis(ctx context.Context, name, value string) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []int64
	for id, m := range f.tags {
		if m[name] == value {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeBudgetStore) QueryAxisTagLosses(ctx context.Context, costID int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.losses[costID]...), nil
}

func (f *fakeBudgetStore) PauseGet(context.Context, string, string) (bool, int64, error) {
	return false, 0, nil
}
func (f *fakeBudgetStore) PauseSet(context.Context, string, string, string, int64, int64) error {
	return nil
}
func (f *fakeBudgetStore) PauseClear(context.Context, string, string) error { return nil }
func (f *fakeBudgetStore) PauseClearIfExpired(context.Context, string, string, int64) error {
	return nil
}
func (f *fakeBudgetStore) PauseListActive(context.Context) ([]PauseRow, error) {
	return nil, nil
}
func (f *fakeBudgetStore) AnomalyAppend(context.Context, AnomalyRow) error { return nil }
func (f *fakeBudgetStore) AnomalyWindow(context.Context, string, string, int) ([]float64, error) {
	return nil, nil
}
func (f *fakeBudgetStore) RolledUSDByAxis(context.Context, string, string, int64) (float64, error) {
	return 0, nil
}

func TestTagWritesAllRequiredAxes(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-12",
	}
	if err := tagger.Tag(context.Background(), 100, axisTags); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	got, _ := store.QueryAxisTags(context.Background(), 100)
	for k, v := range axisTags {
		if got[k] != v {
			t.Errorf("axis %q = %q, want %q", k, got[k], v)
		}
	}
	if len(store.losses[100]) != 0 {
		t.Errorf("losses = %v, want []", store.losses[100])
	}
}

func TestTagOptionalAxesPersistedWhenPresent(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":   "internal-platform-x",
		"doctrine":  "max-scope",
		"stage":     "design",
		"task":      "T-12",
		"operation": "research_dispatch",
		"worker_id": "w-42",
	}
	if err := tagger.Tag(context.Background(), 200, axisTags); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	got, _ := store.QueryAxisTags(context.Background(), 200)
	if len(got) != 6 {
		t.Errorf("len(got) = %d, want 6 (4 required + 2 optional)", len(got))
	}
	if got["operation"] != "research_dispatch" {
		t.Errorf("operation = %q, want research_dispatch", got["operation"])
	}
}

func TestTagIsIdempotentUnderRetry(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-12",
	}
	for i := 0; i < 5; i++ {
		if err := tagger.Tag(context.Background(), 300, axisTags); err != nil {
			t.Fatalf("Tag iter %d: %v", i, err)
		}
	}
	got, _ := store.QueryAxisTags(context.Background(), 300)
	if len(got) != 4 {
		t.Errorf("len(got) = %d, want 4 (idempotent)", len(got))
	}
}

func TestTagMissingRequiredAxisEmitsLossEvent(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",

		"task": "T-12",
	}
	err := tagger.Tag(context.Background(), 400, axisTags)
	if !errors.Is(err, ErrAxisIncomplete) {
		t.Errorf("err = %v, want ErrAxisIncomplete", err)
	}
	if len(store.losses[400]) != 1 || store.losses[400][0] != "stage" {
		t.Errorf("losses = %v, want [stage]", store.losses[400])
	}

	got, _ := store.QueryAxisTags(context.Background(), 400)
	if len(got) != 3 {
		t.Errorf("len(got) = %d, want 3 (best-effort partial write)", len(got))
	}
}

func TestTagMultipleMissingAxesAllEventsEmitted(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project": "internal-platform-x",
	}
	err := tagger.Tag(context.Background(), 500, axisTags)
	if !errors.Is(err, ErrAxisIncomplete) {
		t.Fatalf("err = %v, want ErrAxisIncomplete", err)
	}
	if len(store.losses[500]) != 3 {
		t.Errorf("losses = %d events, want 3 (doctrine+stage+task missing)", len(store.losses[500]))
	}
}

func TestTagInsertErrorPropagated(t *testing.T) {
	store := newFakeBudgetStore()
	store.failOn["project"] = errors.New("disk full")
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-12",
	}
	err := tagger.Tag(context.Background(), 600, axisTags)
	if err == nil || !errors.Is(err, ErrAxisInsertFailed) {
		t.Errorf("err = %v, want ErrAxisInsertFailed wrap", err)
	}
}

func TestTagInsertErrorPreservesUnderlyingViaDoubleWrap(t *testing.T) {
	rootCause := errors.New("disk full simulating sql.ErrConnDone")
	store := newFakeBudgetStore()
	store.failOn["project"] = rootCause
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-12",
	}
	err := tagger.Tag(context.Background(), 601, axisTags)
	if !errors.Is(err, ErrAxisInsertFailed) {
		t.Errorf("errors.Is sentinel = false, want true")
	}
	if !errors.Is(err, rootCause) {
		t.Errorf("errors.Is rootCause = false, want true (double-%%w preserves cause)")
	}
}

func TestTagLossEmitErrorPreservesUnderlyingViaDoubleWrap(t *testing.T) {
	rootCause := errors.New("loss-table corrupt simulating sql.ErrConnDone")
	store := newFakeBudgetStore()
	store.failOn["loss:stage"] = rootCause
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",

		"task": "T-12",
	}
	err := tagger.Tag(context.Background(), 651, axisTags)
	if !errors.Is(err, ErrAxisIncomplete) {
		t.Errorf("errors.Is sentinel = false, want true")
	}
	if !errors.Is(err, rootCause) {
		t.Errorf("errors.Is rootCause = false, want true (double-%%w preserves cause)")
	}
}

func TestTagLossEmitErrorWrapsIncomplete(t *testing.T) {
	store := newFakeBudgetStore()
	store.failOn["loss:stage"] = errors.New("loss-table corrupt")
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",

		"task": "T-12",
	}
	err := tagger.Tag(context.Background(), 650, axisTags)
	if !errors.Is(err, ErrAxisIncomplete) {
		t.Errorf("err = %v, want ErrAxisIncomplete (with emit error wrapped)", err)
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "emit failed") {
		t.Errorf("err = %v; expected to mention emit failure", err)
	}
}

func TestTagRejectsNegativeCostID(t *testing.T) {
	tagger := NewAxisTagger(newFakeBudgetStore())
	if err := tagger.Tag(context.Background(), 0, nil); err == nil {
		t.Error("err = nil, want error on cost_id=0")
	}
	if err := tagger.Tag(context.Background(), -1, nil); err == nil {
		t.Error("err = nil, want error on cost_id=-1")
	}
}

func TestTagAcceptsNilMapAsAllMissing(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	err := tagger.Tag(context.Background(), 700, nil)
	if !errors.Is(err, ErrAxisIncomplete) {
		t.Errorf("err = %v, want ErrAxisIncomplete (nil map = all missing)", err)
	}
	if len(store.losses[700]) != 4 {
		t.Errorf("losses = %d, want 4 (all required missing)", len(store.losses[700]))
	}
}

func TestNewAxisTaggerNilStorePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic, got none")
		}
	}()
	_ = NewAxisTagger(nil)
}

func TestRequiredAxesIsExactly4(t *testing.T) {
	required := RequiredAxes()
	if len(required) != 4 {
		t.Errorf("len(RequiredAxes) = %d, want 4", len(required))
	}
	want := map[string]bool{"project": true, "doctrine": true, "stage": true, "task": true}
	for _, a := range required {
		if !want[a] {
			t.Errorf("RequiredAxes contains unexpected %q", a)
		}
		delete(want, a)
	}
	if len(want) != 0 {
		t.Errorf("RequiredAxes missing %v", want)
	}
}

func TestRequiredAxesIsCopy(t *testing.T) {
	a := RequiredAxes()
	b := RequiredAxes()
	a[0] = "tampered"
	if b[0] == "tampered" {
		t.Error("RequiredAxes returned shared slice; must be a copy")
	}
}

func TestAxisTagCompletenessAnchorPresent(t *testing.T) {
	if err := axisTagCompleteness(); !errors.Is(err, ErrAxisIncomplete) {
		t.Fatalf("axisTagCompleteness must return ErrAxisIncomplete, got %v", err)
	}
}

func TestAugmentationAxisIsOptional5thAxis(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":      "internal-platform-x",
		"doctrine":     "max-scope",
		"stage":        "augmentation",
		"task":         "T-augment-1",
		"augmentation": "internal-platform-x",
	}
	if err := tagger.Tag(context.Background(), 700, axisTags); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	got, _ := store.QueryAxisTags(context.Background(), 700)
	if got["augmentation"] != "internal-platform-x" {
		t.Errorf("augmentation axis = %q; want internal-platform-x", got["augmentation"])
	}
	if got["project"] != "internal-platform-x" {
		t.Errorf("project axis = %q; want internal-platform-x", got["project"])
	}

	if len(got) != 5 {
		t.Errorf("len(got) = %d; want 5 (4 required + 1 augmentation optional)", len(got))
	}
}

func TestAugmentationAxisDoesNotTriggerLossEventWhenMissing(t *testing.T) {
	store := newFakeBudgetStore()
	tagger := NewAxisTagger(store)
	axisTags := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-12",
	}
	if err := tagger.Tag(context.Background(), 800, axisTags); err != nil {
		t.Fatalf("Tag: %v (must be nil — all 4 required axes present)", err)
	}
	losses, _ := store.QueryAxisTagLosses(context.Background(), 800)
	for _, l := range losses {
		if l == "augmentation" {
			t.Errorf("augmentation axis emitted loss event; it must be optional (not in requiredAxes)")
		}
	}
}

func TestAugmentationAxisNameConstantMatches(t *testing.T) {
	if AugmentationAxisName != "augmentation" {
		t.Errorf("AugmentationAxisName = %q; want \"augmentation\"", AugmentationAxisName)
	}

	found := false
	for _, a := range optionalAxes {
		if a == AugmentationAxisName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AugmentationAxisName=%q not in optionalAxes %v", AugmentationAxisName, optionalAxes)
	}
	for _, a := range requiredAxes {
		if a == AugmentationAxisName {
			t.Errorf("AugmentationAxisName=%q wrongly in requiredAxes %v (must be optional per spec §1 Q3)", AugmentationAxisName, requiredAxes)
		}
	}
}
