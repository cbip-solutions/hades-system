package merge_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestEventTypeStringer(t *testing.T) {
	cases := []struct {
		et   merge.EventType
		want string
	}{
		{merge.EvtMergeStartedWithMode, "MergeStartedWithMode"},
		{merge.EvtMergeCacheHit, "MergeCacheHit"},
		{merge.EvtMergeCompleted, "MergeCompleted"},
		{merge.EvtMergeFailed, "MergeFailed"},
		{merge.EvtMergeAllCandidatesFailed, "MergeAllCandidatesFailed"},
		{merge.EvtBaselineStarted, "BaselineStarted"},
		{merge.EvtBaselineComplete, "BaselineComplete"},
		{merge.EvtBaselineFailed, "BaselineFailed"},
		{merge.EvtCandidateStarted, "CandidateStarted"},
		{merge.EvtCandidateComplete, "CandidateComplete"},
		{merge.EvtCandidateFailed, "CandidateFailed"},
		{merge.EvtFlakeRerunStarted, "FlakeRerunStarted"},
		{merge.EvtScoringComplete, "ScoringComplete"},
		{merge.EvtMergeCacheRebuilt, "MergeCacheRebuilt"},
		{merge.EvtMergeStragglerKilled, "MergeStragglerKilled"},
		{merge.EvtMergeAnomalyDetected, "MergeAnomalyDetected"},
	}
	for _, c := range cases {
		if got := c.et.String(); got != c.want {
			t.Errorf("EventType(%d).String() = %q want %q", int(c.et), got, c.want)
		}
	}
}

func TestEventTypeUnknown(t *testing.T) {
	if got := merge.EventType(9999).String(); got != "Unknown" {
		t.Errorf("EventType(9999).String() = %q want Unknown", got)
	}
}

func TestAllEventTypesCoverage(t *testing.T) {
	all := merge.AllEventTypes()
	if len(all) != 16 {
		t.Fatalf("AllEventTypes() len = %d want 16 (Phase A frozen)", len(all))
	}
	seen := make(map[merge.EventType]bool)
	for _, et := range all {
		if seen[et] {
			t.Errorf("duplicate EventType %v", et)
		}
		seen[et] = true
		if et.String() == "" || et.String() == "Unknown" {
			t.Errorf("EventType %d has empty/Unknown String", int(et))
		}
	}
}

func TestAnomalyTypeStringer(t *testing.T) {
	cases := []struct {
		at   merge.AnomalyType
		want string
	}{
		{merge.AnomalyScoringFormulaWinnerVetoed, "ScoringFormulaWinnerVetoed"},
		{merge.AnomalyBaselineUnstableAcrossSessions, "BaselineUnstableAcrossSessions"},
		{merge.AnomalyFlakeRateAboveThreshold, "FlakeRateAboveThreshold"},
		{merge.AnomalyTextualMergeUnresolvableRateHigh, "TextualMergeUnresolvableRateHigh"},
		{merge.AnomalyModeDegradationPersistent, "ModeDegradationPersistent"},
	}
	for _, c := range cases {
		if got := c.at.String(); got != c.want {
			t.Errorf("AnomalyType(%d).String() = %q want %q", int(c.at), got, c.want)
		}
	}
}

func TestAnomalyTypeUnknown(t *testing.T) {
	if got := merge.AnomalyType(9999).String(); got != "Unknown" {
		t.Errorf("AnomalyType(9999).String() = %q want Unknown", got)
	}
}

func TestAllAnomalyTypesCoverage(t *testing.T) {
	all := merge.AllAnomalyTypes()
	if len(all) != 5 {
		t.Fatalf("AllAnomalyTypes() len = %d want 5 (Phase A frozen, spec §2.6)", len(all))
	}
	seen := make(map[merge.AnomalyType]bool)
	for _, at := range all {
		if seen[at] {
			t.Errorf("duplicate AnomalyType %v", at)
		}
		seen[at] = true
		if at.String() == "" || at.String() == "Unknown" {
			t.Errorf("AnomalyType %d has empty/Unknown String", int(at))
		}
	}
}

func TestAnomalyTypeIsIntKind(t *testing.T) {
	at := merge.AnomalyType(0)
	if k := reflect.TypeOf(at).Kind(); k != reflect.Int {
		t.Fatalf("AnomalyType.Kind() = %v want Int (inv-zen-110 — typed anomaly events)", k)
	}
}

func TestGenerationCounterMonotonic(t *testing.T) {
	var gc merge.GenerationCounter
	prev := int64(-1)
	for i := 0; i < 100; i++ {
		got := gc.Next()
		if got <= prev {
			t.Fatalf("Next() = %d not strictly monotonic from %d (call #%d)", got, prev, i)
		}
		prev = got
	}
}

func TestGenerationCounterConcurrent(t *testing.T) {
	var gc merge.GenerationCounter
	const goroutines = 16
	const calls = 1000
	got := make([]int64, goroutines*calls)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < calls; i++ {
				got[g*calls+i] = gc.Next()
			}
		}(g)
	}
	wg.Wait()
	seen := make(map[int64]bool, len(got))
	for _, v := range got {
		if seen[v] {
			t.Fatalf("GenerationCounter.Next produced duplicate %d under concurrency", v)
		}
		seen[v] = true
		if v < 1 {
			t.Fatalf("GenerationCounter.Next produced %d (must be ≥1)", v)
		}
	}
}

type recordingEmitter struct {
	mu sync.Mutex
	ev []merge.Event
}

func (r *recordingEmitter) Append(_ context.Context, e merge.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, e)
	return nil
}

func (r *recordingEmitter) Snapshot() []merge.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]merge.Event, len(r.ev))
	copy(out, r.ev)
	return out
}

func TestEventEmitterRecordingFakeRoundTrip(t *testing.T) {
	em := &recordingEmitter{}
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	in := merge.Event{
		Type:         merge.EvtMergeStartedWithMode,
		GenerationID: 1,
		RequestHash:  "abc123",
		Payload:      []byte(`{"mode":"Normal"}`),
		Timestamp:    now,
		CausalChain:  []string{"req-1"},
	}
	if err := em.Append(context.Background(), in); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got := em.Snapshot()
	if len(got) != 1 {
		t.Fatalf("Snapshot len = %d want 1", len(got))
	}
	if got[0].Type != in.Type || got[0].GenerationID != in.GenerationID || got[0].RequestHash != in.RequestHash {
		t.Errorf("Snapshot mismatch: %+v vs %+v", got[0], in)
	}
}

func TestGenerationCounterAtomicBacking(t *testing.T) {

	gcType := reflect.TypeOf((*merge.GenerationCounter)(nil)).Elem()
	if gcType.Kind() != reflect.Struct {
		t.Fatalf("GenerationCounter underlying kind = %v want Struct", gcType.Kind())
	}
	field, ok := gcType.FieldByName("n")
	if !ok {
		t.Fatal("GenerationCounter missing field 'n' (atomic backing)")
	}
	wantType := reflect.TypeOf((*atomic.Int64)(nil)).Elem()
	if field.Type != wantType {
		t.Fatalf("GenerationCounter.n type = %v want atomic.Int64 (Q9 D)", field.Type)
	}
}

func TestGenerationCounterCurrent(t *testing.T) {
	var gc merge.GenerationCounter

	if got := gc.Current(); got != 0 {
		t.Errorf("Current() before Next = %d want 0", got)
	}

	for i := 0; i < 5; i++ {
		if got := gc.Current(); got != 0 {
			t.Errorf("Current() iter %d = %d want 0 (must not advance)", i, got)
		}
	}

	want := gc.Next()
	if got := gc.Current(); got != want {
		t.Errorf("Current() after Next = %d want %d", got, want)
	}
	if got := gc.Current(); got != want {
		t.Errorf("Current() second call = %d want %d (must be idempotent)", got, want)
	}

	want2 := gc.Next()
	if want2 != want+1 {
		t.Fatalf("Next() second = %d want %d (monotonic+1)", want2, want+1)
	}
	if got := gc.Current(); got != want2 {
		t.Errorf("Current() after second Next = %d want %d", got, want2)
	}
}

func TestTestSuiteEquals(t *testing.T) {
	a := merge.TestSuite{
		Smoke: []string{"go", "test", "-tags=smoke", "./..."},
		Full:  []string{"go", "test", "./..."},
	}
	b := merge.TestSuite{
		Smoke: []string{"go", "test", "-tags=smoke", "./..."},
		Full:  []string{"go", "test", "./..."},
	}
	if !a.Equals(b) {
		t.Errorf("Equals(identical) = false want true")
	}

	c := merge.TestSuite{
		Smoke: []string{"go", "test", "-tags=other", "./..."},
		Full:  []string{"go", "test", "./..."},
	}
	if a.Equals(c) {
		t.Errorf("Equals(different Smoke) = true want false")
	}

	d := merge.TestSuite{
		Smoke: []string{"go", "test", "-tags=smoke", "./..."},
		Full:  []string{"go", "test", "-v", "./..."},
	}
	if a.Equals(d) {
		t.Errorf("Equals(different Full) = true want false")
	}

	empty := merge.TestSuite{}
	if a.Equals(empty) {
		t.Errorf("Equals(empty) = true want false")
	}

	empty2 := merge.TestSuite{}
	if !empty.Equals(empty2) {
		t.Errorf("Equals(empty,empty) = false want true")
	}

	if !a.Equals(a) {
		t.Errorf("Equals(a, a) = false want true (reflexivity)")
	}
}

func TestSentinelErrorsExportedAndIsMatch(t *testing.T) {
	cases := []struct {
		sentinel error
		name     string
	}{
		{merge.ErrInvalidRequest, "ErrInvalidRequest"},
		{merge.ErrTargetNotExist, "ErrTargetNotExist"},
		{merge.ErrBaseNotMergeBase, "ErrBaseNotMergeBase"},
		{merge.ErrCandidatesNotUnique, "ErrCandidatesNotUnique"},
		{merge.ErrPoolInsufficient, "ErrPoolInsufficient"},
		{merge.ErrGitVersionTooOld, "ErrGitVersionTooOld"},
		{merge.ErrGitNotFound, "ErrGitNotFound"},
		{merge.ErrPatchRejected, "ErrPatchRejected"},
		{merge.ErrBaselineFailed, "ErrBaselineFailed"},
	}
	for _, c := range cases {
		if c.sentinel == nil {
			t.Errorf("%s sentinel is nil", c.name)
			continue
		}
		wrapped := fmt.Errorf("merge.test: contextual wrap: %w", c.sentinel)
		if !errors.Is(wrapped, c.sentinel) {
			t.Errorf("errors.Is(wrap of %s, %s) = false", c.name, c.name)
		}
		msg := c.sentinel.Error()
		if msg == "" {
			t.Errorf("%s has empty Error() string", c.name)
		}
		if msg[:6] != "merge:" {
			t.Errorf("%s.Error()=%q does not start with 'merge:' (Plan 5 Phase J convention)", c.name, msg)
		}
	}
}

func TestSentinelErrorsDistinct(t *testing.T) {
	all := []error{
		merge.ErrInvalidRequest, merge.ErrTargetNotExist,
		merge.ErrBaseNotMergeBase, merge.ErrCandidatesNotUnique,
		merge.ErrPoolInsufficient, merge.ErrGitVersionTooOld,
		merge.ErrGitNotFound, merge.ErrPatchRejected,
		merge.ErrBaselineFailed,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) returned true (sentinels must be distinct)", a, b)
			}
		}
	}
}
