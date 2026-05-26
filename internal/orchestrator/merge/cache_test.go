package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type fakeEventReader struct {
	mu     sync.Mutex
	events []merge.Event
	failOn int
}

func (f *fakeEventReader) Each(ctx context.Context, fn func(e merge.Event) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, e := range f.events {
		if f.failOn == i {
			return errors.New("fakeEventReader injected failure")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

func makeOutcomeCache(headSHA, integrationSHA string, scores map[string]float64) merge.MergeOutcome {
	return merge.MergeOutcome{
		Winner: merge.MergeCandidate{
			Branch:       "feat-A",
			HeadSHA:      headSHA,
			Patch:        []byte("--- diff --- \n"),
			ReviewerVote: 1,
		},
		IntegrationSHA:  integrationSHA,
		TestsPassed:     true,
		ReviewerSummary: "all approved",
		AllScores:       scores,
		Reverted:        false,
	}
}

func makeReqCache(target, base, engineVer string, headSHAs ...string) merge.MergeRequest {
	cs := make([]merge.MergeCandidate, len(headSHAs))
	for i, sha := range headSHAs {
		cs[i] = merge.MergeCandidate{Branch: "feat-" + sha[:1], HeadSHA: sha}
	}
	return merge.MergeRequest{
		TargetBranch:  target,
		BaseSHA:       base,
		Mode:          merge.ModeNormal,
		Candidates:    cs,
		EngineVersion: engineVer,
	}
}

func TestCacheKeyDeterministic(t *testing.T) {
	r1 := makeReqCache("main", "deadbeef0000000000000000000000000000beef", "v0.6.0",
		"1111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222")
	r2 := makeReqCache("main", "deadbeef0000000000000000000000000000beef", "v0.6.0",
		"2222222222222222222222222222222222222222",
		"1111111111111111111111111111111111111111")
	if h1, h2 := merge.CacheKey(r1), merge.CacheKey(r2); h1 != h2 {
		t.Errorf("CacheKey not order-invariant: %s != %s", h1, h2)
	}
}

func TestCacheKeySensitiveToTarget(t *testing.T) {
	r1 := makeReqCache("main", "abc", "v1", "h1", "h2")
	r2 := makeReqCache("main2", "abc", "v1", "h1", "h2")
	if merge.CacheKey(r1) == merge.CacheKey(r2) {
		t.Error("CacheKey insensitive to TargetBranch")
	}
}

func TestCacheKeySensitiveToBase(t *testing.T) {
	r1 := makeReqCache("main", "abc", "v1", "h1")
	r2 := makeReqCache("main", "abd", "v1", "h1")
	if merge.CacheKey(r1) == merge.CacheKey(r2) {
		t.Error("CacheKey insensitive to BaseSHA")
	}
}

func TestCacheKeySensitiveToEngineVersion(t *testing.T) {
	r1 := makeReqCache("main", "abc", "v0.6.0", "h1")
	r2 := makeReqCache("main", "abc", "v0.6.1", "h1")
	if merge.CacheKey(r1) == merge.CacheKey(r2) {
		t.Error("CacheKey insensitive to EngineVersion (replay invalidation broken)")
	}
}

func TestCacheKeyBoundarySafety(t *testing.T) {
	r1 := makeReqCache("ab", "cd", "v1", "h")
	r2 := makeReqCache("a", "bcd", "v1", "h")
	if merge.CacheKey(r1) == merge.CacheKey(r2) {
		t.Error("CacheKey boundary collision (target/base concatenation)")
	}
}

func TestCacheKeyHexLength(t *testing.T) {
	got := merge.CacheKey(makeReqCache("main", "abc", "v1", "h1"))
	if len(got) != 64 {
		t.Errorf("CacheKey len = %d want 64 (sha256 hex)", len(got))
	}
}

func TestCacheLookupMissOnFreshCache(t *testing.T) {
	c := merge.NewCache()
	if _, ok := c.Lookup(makeReqCache("main", "abc", "v1", "h1")); ok {
		t.Error("Lookup on empty cache returned ok=true")
	}
}

func TestCacheStoreThenLookupReturnsExact(t *testing.T) {
	c := merge.NewCache()
	req := makeReqCache("main", "abc", "v1", "h1")
	want := makeOutcomeCache("h1", "intsha1", map[string]float64{"h1": 5.0})
	c.Store(req, want)
	got, ok := c.Lookup(req)
	if !ok {
		t.Fatal("Lookup miss after Store")
	}
	if got.IntegrationSHA != want.IntegrationSHA {
		t.Errorf("IntegrationSHA = %s want %s", got.IntegrationSHA, want.IntegrationSHA)
	}
	if got.Winner.HeadSHA != want.Winner.HeadSHA {
		t.Errorf("Winner.HeadSHA = %s want %s", got.Winner.HeadSHA, want.Winner.HeadSHA)
	}
	if got.AllScores["h1"] != 5.0 {
		t.Errorf("AllScores[h1] = %v want 5.0", got.AllScores["h1"])
	}
}

func TestCacheStoreOverwritesSameKey(t *testing.T) {
	c := merge.NewCache()
	req := makeReqCache("main", "abc", "v1", "h1")
	c.Store(req, makeOutcomeCache("h1", "first", nil))
	c.Store(req, makeOutcomeCache("h1", "second", nil))
	got, _ := c.Lookup(req)
	if got.IntegrationSHA != "second" {
		t.Errorf("Store did not overwrite: got %s want second", got.IntegrationSHA)
	}
}

func TestCacheConcurrentLookupAndStore(t *testing.T) {
	c := merge.NewCache()
	req := makeReqCache("main", "abc", "v1", "h1")
	c.Store(req, makeOutcomeCache("h1", "ints", nil))
	const goroutines = 32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, ok := c.Lookup(req); !ok {
					t.Error("concurrent Lookup miss")
				}
			}
		}()
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Store(req, makeOutcomeCache("h1", "ints", map[string]float64{"h1": float64(i*j) + 1}))
			}
		}(i)
	}
	wg.Wait()
}

func TestCacheSizeAfterMultipleStores(t *testing.T) {
	c := merge.NewCache()
	for i := 0; i < 10; i++ {
		c.Store(makeReqCache("main", "abc", "v1", strings.Repeat(string(rune('a'+i)), 40)), makeOutcomeCache("x", "ints", nil))
	}
	if got := c.Size(); got != 10 {
		t.Errorf("Size = %d want 10", got)
	}
}

func TestCacheClearEmptiesMap(t *testing.T) {
	c := merge.NewCache()
	for i := 0; i < 5; i++ {
		c.Store(makeReqCache("main", "abc", "v1", strings.Repeat(string(rune('a'+i)), 40)), makeOutcomeCache("x", "ints", nil))
	}
	if got := c.Size(); got != 5 {
		t.Fatalf("setup: Size = %d want 5", got)
	}
	c.Clear()
	if got := c.Size(); got != 0 {
		t.Errorf("Size after Clear = %d want 0", got)
	}

	prev := makeReqCache("main", "abc", "v1", strings.Repeat("a", 40))
	if _, ok := c.Lookup(prev); ok {
		t.Error("Lookup hit after Clear — Clear should have removed the entry")
	}

	c.Store(prev, makeOutcomeCache("x", "ints2", nil))
	if got, ok := c.Lookup(prev); !ok || got.IntegrationSHA != "ints2" {
		t.Errorf("Store after Clear: got=%+v ok=%v", got, ok)
	}
}

func TestCacheClearIsRaceSafe(t *testing.T) {
	c := merge.NewCache()
	req := makeReqCache("main", "abc", "v1", "h1")
	c.Store(req, makeOutcomeCache("h1", "ints", nil))

	const goroutines = 16
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = c.Lookup(req)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				c.Store(req, makeOutcomeCache("h1", "ints", nil))
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				c.Clear()
			}
		}()
	}
	wg.Wait()
}

func TestCacheRebuildReplayDeterministic(t *testing.T) {
	reqA := makeReqCache("main", "abc", "v1", "h1")
	reqB := makeReqCache("dev", "def", "v1", "h2")
	outA := makeOutcomeCache("h1", "intA", nil)
	outB := makeOutcomeCache("h2", "intB", nil)

	mkPayload := func(req merge.MergeRequest, out merge.MergeOutcome) []byte {
		b, _ := json.Marshal(merge.MergeCompletedPayload{
			WinnerCandidateID: out.Winner.HeadSHA,
			IntegrationSHA:    out.IntegrationSHA,
			RequestHash:       merge.CacheKey(req),
			Outcome:           out,
		})
		return b
	}
	events := []merge.Event{
		{Type: merge.EvtMergeCompleted, GenerationID: 1, RequestHash: merge.CacheKey(reqA), Payload: mkPayload(reqA, outA), Timestamp: time.Now()},
		{Type: merge.EvtMergeCompleted, GenerationID: 2, RequestHash: merge.CacheKey(reqB), Payload: mkPayload(reqB, outB), Timestamp: time.Now()},
	}

	c1 := merge.NewCache()
	em1 := &recordingEmitter{}
	if err := c1.Rebuild(context.Background(), &fakeEventReader{events: events, failOn: -1}, em1); err != nil {
		t.Fatalf("c1 Rebuild: %v", err)
	}
	c2 := merge.NewCache()
	em2 := &recordingEmitter{}
	if err := c2.Rebuild(context.Background(), &fakeEventReader{events: events, failOn: -1}, em2); err != nil {
		t.Fatalf("c2 Rebuild: %v", err)
	}
	if c1.Size() != c2.Size() {
		t.Errorf("inv-zen-105 violated: c1.Size=%d c2.Size=%d", c1.Size(), c2.Size())
	}
	if outA1, _ := c1.Lookup(reqA); outA1.IntegrationSHA != outA.IntegrationSHA {
		t.Errorf("c1 lookup mismatch")
	}
	if outA2, _ := c2.Lookup(reqA); outA2.IntegrationSHA != outA.IntegrationSHA {
		t.Errorf("c2 lookup mismatch")
	}
}

func TestCacheRebuildEmitsCacheRebuiltOnSuccess(t *testing.T) {
	req := makeReqCache("main", "abc", "v1", "h1")
	out := makeOutcomeCache("h1", "intA", nil)
	payload, _ := json.Marshal(merge.MergeCompletedPayload{
		WinnerCandidateID: "h1", IntegrationSHA: "intA", RequestHash: merge.CacheKey(req),
		Outcome: out,
	})
	events := []merge.Event{
		{Type: merge.EvtMergeCompleted, GenerationID: 1, RequestHash: merge.CacheKey(req), Payload: payload},
	}
	c := merge.NewCache()
	em := &recordingEmitter{}
	if err := c.Rebuild(context.Background(), &fakeEventReader{events: events, failOn: -1}, em); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	snap := em.Snapshot()
	if len(snap) == 0 {
		t.Fatal("Rebuild emitted no events")
	}
	last := snap[len(snap)-1]
	if last.Type != merge.EvtMergeCacheRebuilt {
		t.Errorf("last emitted = %v want EvtMergeCacheRebuilt", last.Type)
	}
	var p merge.MergeCacheRebuiltPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode rebuilt payload: %v", err)
	}
	if p.EventsProcessed != 1 {
		t.Errorf("EventsProcessed = %d want 1", p.EventsProcessed)
	}
	if p.CacheSize != 1 {
		t.Errorf("CacheSize = %d want 1", p.CacheSize)
	}
	if p.RebuildError != "" {
		t.Errorf("RebuildError = %q want empty (success)", p.RebuildError)
	}
}

func TestCacheRebuildEmitsErrorPayloadOnFailure(t *testing.T) {

	events := []merge.Event{
		{Type: merge.EvtMergeCompleted, GenerationID: 1, Payload: []byte("malformed{{{")},
	}
	c := merge.NewCache()
	em := &recordingEmitter{}
	if err := c.Rebuild(context.Background(), &fakeEventReader{events: events, failOn: -1}, em); err == nil {
		t.Fatal("Rebuild expected error on malformed payload")
	}
	snap := em.Snapshot()
	if len(snap) == 0 {
		t.Fatal("Rebuild emitted no event on failure")
	}
	last := snap[len(snap)-1]
	if last.Type != merge.EvtMergeCacheRebuilt {
		t.Errorf("last emitted = %v want EvtMergeCacheRebuilt (Drift-E)", last.Type)
	}
	var p merge.MergeCacheRebuiltPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode rebuilt payload: %v", err)
	}
	if p.RebuildError == "" {
		t.Error("RebuildError empty on failure (Drift-E: must be non-empty)")
	}
}

func TestCacheRebuildIgnoresNonMergeCompletedEvents(t *testing.T) {
	events := []merge.Event{
		{Type: merge.EvtBaselineStarted, GenerationID: 1, Payload: []byte("{}")},
		{Type: merge.EvtCandidateComplete, GenerationID: 2, Payload: []byte("{}")},
	}
	c := merge.NewCache()
	em := &recordingEmitter{}
	if err := c.Rebuild(context.Background(), &fakeEventReader{events: events, failOn: -1}, em); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if got := c.Size(); got != 0 {
		t.Errorf("Size = %d want 0", got)
	}
}

func TestCacheRebuildFallsBackToEventRequestHashWhenPayloadHashEmpty(t *testing.T) {

	out := makeOutcomeCache("h1", "intA", nil)
	payload, _ := json.Marshal(merge.MergeCompletedPayload{
		WinnerCandidateID: "h1",
		IntegrationSHA:    "intA",

		Outcome: out,
	})
	const eventLevelHash = "framing-supplied-hash-deadbeef"
	events := []merge.Event{
		{Type: merge.EvtMergeCompleted, GenerationID: 1, RequestHash: eventLevelHash, Payload: payload},
	}
	c := merge.NewCache()
	em := &recordingEmitter{}
	if err := c.Rebuild(context.Background(), &fakeEventReader{events: events, failOn: -1}, em); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if got := c.Size(); got != 1 {
		t.Fatalf("Size = %d want 1 (fallback to e.RequestHash should still produce an entry)", got)
	}

	snap := em.Snapshot()
	last := snap[len(snap)-1]
	var p merge.MergeCacheRebuiltPayload
	if err := json.Unmarshal(last.Payload, &p); err != nil {
		t.Fatalf("decode rebuilt payload: %v", err)
	}
	if p.CacheSize != 1 {
		t.Errorf("CacheSize = %d want 1", p.CacheSize)
	}
}

func TestCacheRebuildHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := merge.NewCache()
	em := &recordingEmitter{}
	err := c.Rebuild(ctx, &fakeEventReader{events: []merge.Event{
		{Type: merge.EvtMergeCompleted, RequestHash: "abc"},
	}, failOn: -1}, em)
	if err == nil {
		t.Fatal("expected error on pre-cancelled context")
	}
}

func TestPassingSetHashOrderInvariant(t *testing.T) {
	a := merge.PassingSet{"test-c", "test-a", "test-b"}
	b := merge.PassingSet{"test-a", "test-b", "test-c"}
	if a.Hash() != b.Hash() {
		t.Errorf("PassingSet.Hash not order-invariant")
	}
}

func TestPassingSetHashSensitiveToContent(t *testing.T) {
	a := merge.PassingSet{"a", "b"}
	b := merge.PassingSet{"a", "c"}
	if a.Hash() == b.Hash() {
		t.Error("PassingSet.Hash collides on different contents")
	}
}

func TestPassingSetHashHexLength(t *testing.T) {
	got := merge.PassingSet{"a"}.Hash()
	if len(got) != 64 {
		t.Errorf("PassingSet.Hash len = %d want 64", len(got))
	}
}

func TestPassingSetHas(t *testing.T) {
	p := merge.PassingSet{"alpha", "beta"}
	if !p.Has("alpha") {
		t.Error("Has(alpha) = false")
	}
	if p.Has("delta") {
		t.Error("Has(delta) = true on absent ID")
	}
}

func TestPassingSetHasAnyMissing(t *testing.T) {
	baseline := merge.PassingSet{"a", "b", "c"}
	full := merge.PassingSet{"a", "b", "c", "d"}
	partial := merge.PassingSet{"a", "b"}
	if baseline.HasAnyMissing(full) {
		t.Error("baseline.HasAnyMissing(full) = true (full is superset)")
	}
	if !baseline.HasAnyMissing(partial) {
		t.Error("baseline.HasAnyMissing(partial) = false (missing 'c')")
	}
}
