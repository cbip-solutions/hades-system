package ecosystem

// Tests for verifier.go — 3-stage verify-at-answer-time
// .
//
// Doctrine verifier is security/correctness-critical (≥90% coverage).
// Test matrix covers all 3 stages, LRU eviction + TTL expiry, concurrent
// callers, context cancellation, per-ecosystem dispatch, and live-cmd
// failure surfacing.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVerifier_StageA_SymbolIndexHit(t *testing.T) {
	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"crypto/sha256.Sum256": "func Sum256(data []byte) [Size]byte"},
	})
	v := newTestVerifier(t, si, nil)
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "crypto/sha256.Sum256", Version: "1.22"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !out.AllVerified {
		t.Errorf("AllVerified must be true on stage-A hit")
	}
	if out.Verifications[0].Source != string(VerifySourceSymbolIndex) {
		t.Errorf("want source=symbol_index, got %s", out.Verifications[0].Source)
	}
	if out.Verifications[0].Signature == "" {
		t.Errorf("signature must be populated on stage-A hit")
	}
	if !out.Verifications[0].Exists {
		t.Errorf("Exists must be true on stage-A hit")
	}
}

func TestVerifier_StageA_ShortCircuitsLiveCmd(t *testing.T) {
	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"k.Sym": "sig"},
	})
	fake := &fakeLiveCmdRunner{results: map[string]liveCmdResult{}}
	v := newTestVerifier(t, si, fake)
	_, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "k.Sym", Version: "1.22"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if fake.calls != 0 {
		t.Errorf("LiveCmdRunner must not be invoked on stage-A hit; got %d calls", fake.calls)
	}
}

func TestVerifier_StageB_LRUHit(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:crypto/missing.Sym": {Exists: true, Signature: "func Sym()"},
		},
	}
	v := newTestVerifier(t, si, fake)

	ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: "crypto/missing.Sym", Version: "1.22"}
	out, err := v.Verify(context.Background(), []SymbolRef{ref})
	if err != nil {
		t.Fatalf("Verify first call: %v", err)
	}
	if out.Verifications[0].Source != string(VerifySourceLiveCmd) {
		t.Fatalf("first call expected stage-C; got %s", out.Verifications[0].Source)
	}
	if fake.calls != 1 {
		t.Errorf("live runner call count = %d; want 1", fake.calls)
	}

	out2, err := v.Verify(context.Background(), []SymbolRef{ref})
	if err != nil {
		t.Fatalf("Verify second call: %v", err)
	}
	if out2.Verifications[0].Source != string(VerifySourceLiveCache) {
		t.Errorf("second call expected stage-B (LRU); got %s", out2.Verifications[0].Source)
	}
	if out2.Verifications[0].Signature != "func Sym()" {
		t.Errorf("signature must propagate from cache; got %q", out2.Verifications[0].Signature)
	}
	if fake.calls != 1 {
		t.Errorf("live runner must not be invoked again; got %d", fake.calls)
	}
}

func TestVerifier_StageC_LiveGoDoc(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:context.Context": {Exists: true, Signature: "type Context interface"},
		},
	}
	v := newTestVerifier(t, si, fake)
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "context.Context", Version: "1.22"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !out.AllVerified {
		t.Errorf("AllVerified must be true")
	}
	if out.Verifications[0].Source != string(VerifySourceLiveCmd) {
		t.Errorf("want stage-C; got %s", out.Verifications[0].Source)
	}
	if !strings.Contains(out.Verifications[0].Signature, "interface") {
		t.Errorf("signature should propagate; got %q", out.Verifications[0].Signature)
	}
}

func TestVerifier_StageC_NotFound(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"python:3.12:functools.composer": {Exists: false},
		},
	}
	v := newTestVerifier(t, si, fake)
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoPython, SymbolPath: "functools.composer", Version: "3.12"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.AllVerified {
		t.Errorf("AllVerified must be false on stage-C not_found")
	}
	if out.Verifications[0].Exists {
		t.Errorf("Exists must be false")
	}
	if out.Verifications[0].Source != string(VerifySourceLiveCmd) {
		t.Errorf("want source=live_cmd, got %s", out.Verifications[0].Source)
	}
}

func TestVerifier_StageC_NotFound_CachedNegative(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:no.such": {Exists: false},
		},
	}
	v := newTestVerifier(t, si, fake)
	ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: "no.such", Version: "1.22"}
	_, _ = v.Verify(context.Background(), []SymbolRef{ref})
	out2, _ := v.Verify(context.Background(), []SymbolRef{ref})
	if fake.calls != 1 {
		t.Errorf("not_found must be cached; expected 1 call, got %d", fake.calls)
	}
	if out2.Verifications[0].Source != string(VerifySourceLiveCache) {
		t.Errorf("second call should be served from LRU cache; got %s", out2.Verifications[0].Source)
	}
	if out2.Verifications[0].Exists {
		t.Errorf("cached not_found must surface Exists=false")
	}
}

func TestVerifier_PerEcosystemDispatch(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:net/http.AwaitResponse": {Exists: false},
			"python:3.12:functools.partial":  {Exists: true, Signature: "partial(func, /, *args, **keywords)"},
			"typescript:5.4:React.useAtom":   {Exists: false},
			"rust:1.78:std::async::spawn":    {Exists: false},
		},
	}
	v := newTestVerifier(t, si, fake)
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "net/http.AwaitResponse", Version: "1.22"},
		{Ecosystem: EcoPython, SymbolPath: "functools.partial", Version: "3.12"},
		{Ecosystem: EcoTypeScript, SymbolPath: "React.useAtom", Version: "5.4"},
		{Ecosystem: EcoRust, SymbolPath: "std::async::spawn", Version: "1.78"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.AllVerified {
		t.Errorf("AllVerified must be false (3/4 not found)")
	}
	if len(out.Verifications) != 4 {
		t.Fatalf("want 4 verifications; got %d", len(out.Verifications))
	}

	for i, sv := range out.Verifications {
		isPython := sv.Symbol.Ecosystem == EcoPython
		if isPython != sv.Exists {
			t.Errorf("ref[%d] ecosystem=%s Exists=%v; expected Exists=%v",
				i, sv.Symbol.Ecosystem, sv.Exists, isPython)
		}
	}
}

func TestVerifier_LRUTTLExpires(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:any.X": {Exists: true, Signature: "x"},
		},
	}
	clock := &fakeClock{now: time.Now()}
	v := newTestVerifierWithClock(t, si, fake, clock, 24*time.Hour)

	ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: "any.X", Version: "1.22"}
	_, _ = v.Verify(context.Background(), []SymbolRef{ref})
	clock.now = clock.now.Add(25 * time.Hour)
	out, _ := v.Verify(context.Background(), []SymbolRef{ref})
	if out.Verifications[0].Source != string(VerifySourceLiveCmd) {
		t.Errorf("expected re-run; got source %s", out.Verifications[0].Source)
	}
	if fake.calls != 2 {
		t.Errorf("runner must be called twice after TTL; got %d", fake.calls)
	}
}

func TestVerifier_LRUTTLBoundary_Inclusive(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:b.X": {Exists: true, Signature: "x"},
		},
	}
	clock := &fakeClock{now: time.Now()}
	v := newTestVerifierWithClock(t, si, fake, clock, 24*time.Hour)
	ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: "b.X", Version: "1.22"}
	_, _ = v.Verify(context.Background(), []SymbolRef{ref})
	clock.now = clock.now.Add(24 * time.Hour)
	out, _ := v.Verify(context.Background(), []SymbolRef{ref})
	if out.Verifications[0].Source != string(VerifySourceLiveCache) {
		t.Errorf("at TTL boundary entry must still be cached; got %s", out.Verifications[0].Source)
	}
	if fake.calls != 1 {
		t.Errorf("runner must not be re-invoked at TTL boundary; got %d", fake.calls)
	}
}

func TestVerifier_LRUEviction(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{results: map[string]liveCmdResult{}}
	for i := 0; i < 1100; i++ {
		fake.results[livePathKeyForTest(EcoGo, "1.22", fakeSymbolPath(i))] = liveCmdResult{Exists: true, Signature: "sig"}
	}
	cfg := VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: fake,
		LRUSize:       1000,
		LRUTTL:        24 * time.Hour,
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	for i := 0; i < 1100; i++ {
		ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: fakeSymbolPath(i), Version: "1.22"}
		_, _ = v.Verify(context.Background(), []SymbolRef{ref})
	}
	if fake.calls != 1100 {
		t.Errorf("expected 1100 cold lookups; got %d", fake.calls)
	}

	ref0 := SymbolRef{Ecosystem: EcoGo, SymbolPath: fakeSymbolPath(0), Version: "1.22"}
	_, _ = v.Verify(context.Background(), []SymbolRef{ref0})
	if fake.calls != 1101 {
		t.Errorf("eviction failed; want 1101 calls; got %d", fake.calls)
	}
}

func TestVerifier_LRURecency_PreventsEviction(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{results: map[string]liveCmdResult{}}
	for i := 0; i < 3; i++ {
		fake.results[livePathKeyForTest(EcoGo, "1.22", fakeSymbolPath(i))] = liveCmdResult{Exists: true, Signature: "s"}
	}
	cfg := VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: fake,
		LRUSize:       2,
		LRUTTL:        24 * time.Hour,
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	ref := func(i int) SymbolRef {
		return SymbolRef{Ecosystem: EcoGo, SymbolPath: fakeSymbolPath(i), Version: "1.22"}
	}

	_, _ = v.Verify(context.Background(), []SymbolRef{ref(0)})
	_, _ = v.Verify(context.Background(), []SymbolRef{ref(1)})

	_, _ = v.Verify(context.Background(), []SymbolRef{ref(0)})

	_, _ = v.Verify(context.Background(), []SymbolRef{ref(2)})

	out, _ := v.Verify(context.Background(), []SymbolRef{ref(0)})
	if out.Verifications[0].Source != string(VerifySourceLiveCache) {
		t.Errorf("ref(0) should remain cached after recency refresh; got %s", out.Verifications[0].Source)
	}

	out1, _ := v.Verify(context.Background(), []SymbolRef{ref(1)})
	if out1.Verifications[0].Source != string(VerifySourceLiveCmd) {
		t.Errorf("ref(1) should have been evicted (oldest); got %s", out1.Verifications[0].Source)
	}
}

func TestVerifier_ConcurrentCalls_NoRace(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{results: map[string]liveCmdResult{}}
	for i := 0; i < 100; i++ {
		fake.results[livePathKeyForTest(EcoGo, "1.22", fakeSymbolPath(i))] = liveCmdResult{Exists: true, Signature: "s"}
	}
	v := newTestVerifier(t, si, fake)
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: fakeSymbolPath((seed*100 + i) % 100), Version: "1.22"}
				_, _ = v.Verify(context.Background(), []SymbolRef{ref})
			}
		}(w)
	}
	wg.Wait()
}

func TestVerifier_LiveCmdError_Reported(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:any.X": {Err: errors.New("boom")},
		},
	}
	v := newTestVerifier(t, si, fake)
	_, err := v.Verify(context.Background(), []SymbolRef{{Ecosystem: EcoGo, SymbolPath: "any.X", Version: "1.22"}})
	if err == nil {
		t.Errorf("expected error on live-cmd runner failure")
	}
	if !strings.Contains(err.Error(), "any.X") {
		t.Errorf("error should wrap symbol path; got %v", err)
	}
}

func TestVerifier_ContextCancelled_BeforeStart(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	v := newTestVerifier(t, si, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := v.Verify(ctx, []SymbolRef{{Ecosystem: EcoGo, SymbolPath: "x", Version: "1"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled; got %v", err)
	}
}

func TestVerifier_ContextCancelled_MidLoop(t *testing.T) {
	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"a": "a", "b": "b"},
	})
	v := newTestVerifier(t, si, nil)
	ctx, cancel := context.WithCancel(context.Background())
	refs := make([]SymbolRef, 500)
	for i := range refs {
		refs[i] = SymbolRef{Ecosystem: EcoGo, SymbolPath: "a", Version: "1.22"}
	}

	go cancel()
	_, err := v.Verify(ctx, refs)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("expected nil or context.Canceled; got %v", err)
	}

}

func TestVerifier_ContextCancelled_BetweenRefs(t *testing.T) {
	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"a": "a", "b": "b"},
	})
	v := newTestVerifier(t, si, nil)
	gc := &gateContext{Context: context.Background()}
	refs := []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "a", Version: "1.22"},
		{Ecosystem: EcoGo, SymbolPath: "b", Version: "1.22"},
	}

	_, err := v.Verify(gc, refs)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled mid-loop; got %v", err)
	}
}

type gateContext struct {
	context.Context
	mu     sync.Mutex
	called bool
}

func (g *gateContext) Err() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.called {
		g.called = true
		return nil
	}
	return context.Canceled
}

func TestVerifier_SkipStageC_NoLiveCmd(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:x.Y": {Exists: true, Signature: "y"},
		},
	}
	cfg := VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: fake,
		SkipStageC:    true,
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "x.Y", Version: "1.22"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.AllVerified {
		t.Errorf("AllVerified must be false when skipped")
	}
	if out.Verifications[0].Source != string(VerifySourceSkipped) {
		t.Errorf("expected source=skipped; got %s", out.Verifications[0].Source)
	}
	if fake.calls != 0 {
		t.Errorf("LiveCmdRunner must not be invoked when SkipStageC=true; got %d calls", fake.calls)
	}
}

func TestVerifier_SkipStageC_BypassesLRU(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"go:1.22:c.Z": {Exists: true, Signature: "z"},
		},
	}

	cfgC := VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: fake,
		LRUSize:       16,
		LRUTTL:        24 * time.Hour,
	}
	v, err := NewVerifier(cfgC)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: "c.Z", Version: "1.22"}
	_, _ = v.Verify(context.Background(), []SymbolRef{ref})

	cfgSkip := VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: fake,
		SkipStageC:    true,
	}
	vSkip, err := NewVerifier(cfgSkip)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	out, _ := vSkip.Verify(context.Background(), []SymbolRef{ref})
	if out.Verifications[0].Source != string(VerifySourceSkipped) {
		t.Errorf("SkipStageC must short-circuit before LRU; got %s", out.Verifications[0].Source)
	}
}

func TestVerifier_NilRunner_DegradesToSkipped(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	v := newTestVerifier(t, si, nil)
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "no.runner", Version: "1.22"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Verifications[0].Source != string(VerifySourceSkipped) {
		t.Errorf("expected source=skipped when no runner; got %s", out.Verifications[0].Source)
	}
	if out.Verifications[0].Exists {
		t.Errorf("Exists must be false when stage-C disabled")
	}
}

func TestNewVerifier_RequiresSymbolIndex(t *testing.T) {
	_, err := NewVerifier(VerifierConfig{})
	if err == nil {
		t.Errorf("NewVerifier must error when SymbolIndex is nil")
	}
	if !strings.Contains(err.Error(), "SymbolIndex") {
		t.Errorf("error should mention SymbolIndex; got %v", err)
	}
}

func TestNewVerifier_AppliesDefaults(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	v, err := NewVerifier(VerifierConfig{SymbolIndex: si})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if v.cfg.LRUSize != defaultVerifierLRUSize {
		t.Errorf("default LRUSize = %d; want %d", v.cfg.LRUSize, defaultVerifierLRUSize)
	}
	if v.cfg.LRUTTL != defaultVerifierLRUTTL {
		t.Errorf("default LRUTTL = %v; want %v", v.cfg.LRUTTL, defaultVerifierLRUTTL)
	}
	if v.cfg.Clock == nil {
		t.Errorf("Clock must default to realClock; got nil")
	}

	now := v.cfg.Clock.Now()
	if now.IsZero() {
		t.Errorf("realClock.Now returned zero time")
	}
}

func TestVerifier_EmptyRefs(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	v := newTestVerifier(t, si, nil)
	out, err := v.Verify(context.Background(), nil)
	if err != nil {
		t.Fatalf("Verify(nil): %v", err)
	}
	if !out.AllVerified {
		t.Errorf("AllVerified for empty input should be vacuously true; got false")
	}
	if len(out.Verifications) != 0 {
		t.Errorf("Verifications for empty input should be empty; got %d", len(out.Verifications))
	}
}

func TestVerifier_LatencyPopulated(t *testing.T) {
	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"x.Y": "sig"},
	})

	clock := &advancingClock{now: time.Now(), step: 100 * time.Nanosecond}
	v, err := NewVerifier(VerifierConfig{SymbolIndex: si, Clock: clock})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "x.Y", Version: "1.22"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Verifications[0].Latency <= 0 {
		t.Errorf("Latency must be positive; got %v", out.Verifications[0].Latency)
	}
}

func TestVerifier_LRU_ReInsert_UpdatesExisting(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	results := map[string]liveCmdResult{
		"go:1.22:re.insert": {Exists: true, Signature: "v1"},
	}
	fake := &fakeLiveCmdRunner{results: results}
	clock := &fakeClock{now: time.Now()}
	v := newTestVerifierWithClock(t, si, fake, clock, 24*time.Hour)
	ref := SymbolRef{Ecosystem: EcoGo, SymbolPath: "re.insert", Version: "1.22"}

	_, _ = v.Verify(context.Background(), []SymbolRef{ref})
	if v.lru.Len() != 1 {
		t.Fatalf("LRU should have 1 entry after first call; got %d", v.lru.Len())
	}

	clock.now = clock.now.Add(25 * time.Hour)
	results["go:1.22:re.insert"] = liveCmdResult{Exists: true, Signature: "v2"}
	_, _ = v.Verify(context.Background(), []SymbolRef{ref})
	if v.lru.Len() != 1 {
		t.Errorf("LRU should still have 1 entry after re-insert; got %d", v.lru.Len())
	}

	out, _ := v.Verify(context.Background(), []SymbolRef{ref})
	if out.Verifications[0].Source != string(VerifySourceLiveCache) {
		t.Errorf("third call should hit cache; got %s", out.Verifications[0].Source)
	}
	if out.Verifications[0].Signature != "v2" {
		t.Errorf("cache should serve updated signature v2; got %q", out.Verifications[0].Signature)
	}
}

func TestVerifier_SymbolFieldPreserved_AllStages(t *testing.T) {
	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {"a.A": "sig-a"},
	})
	fake := &fakeLiveCmdRunner{
		results: map[string]liveCmdResult{
			"python:3.12:b.B":    {Exists: true, Signature: "sig-b"},
			"typescript:5.4:c.C": {Exists: false},
		},
	}
	v := newTestVerifier(t, si, fake)
	refs := []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: "a.A", Version: "1.22"},
		{Ecosystem: EcoPython, SymbolPath: "b.B", Version: "3.12"},
		{Ecosystem: EcoTypeScript, SymbolPath: "c.C", Version: "5.4"},
	}
	out, _ := v.Verify(context.Background(), refs)
	for i, sv := range out.Verifications {
		if sv.Symbol != refs[i] {
			t.Errorf("Symbol field at idx %d not preserved; got %+v want %+v", i, sv.Symbol, refs[i])
		}
	}
}

// ---------- validateSymbolRef + cascade-entry security gate (D-5 fix-cycle 2) ----------

func TestValidateSymbolRef_AcceptsLegitimateShapes(t *testing.T) {
	cases := []string{
		"crypto/sha256.Sum256",
		"encoding/json.Marshal",
		"functools.partial",
		"asyncio",
		"react.useState",
		"tokio::spawn",
		"std::async::spawn",
		"std::sync::Mutex",
		"a",
		"_underscored",
		"x86_64",
		"fake.pkg.Symbol42",
		"github.com/zen-swarm/zen",
		"net/http/httptest.Recorder",
		"a.b.c.d.e.f",
		"kotlin:Sym",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := validateSymbolRef(SymbolRef{Ecosystem: EcoGo, SymbolPath: p, Version: "1"})
			if err != nil {
				t.Errorf("validateSymbolRef(%q) unexpectedly rejected: %v", p, err)
			}
		})
	}
}

func TestValidateSymbolRef_RejectsHostileShapes(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{

		{"python-rce-system", `os; __import__('os').system('whoami')#.x`},
		{"python-rce-exec", `os; exec('print(1)')#`},
		{"python-shell-meta", `os; ls`},
		{"python-newline", "os\nimport sys"},
		{"python-semicolon", "os;__import__('os').system('ls')"},
		{"python-quote-attempt", `os.system("rm")`},

		{"npm-flag-registry", "--registry=http://attacker.example/.x"},
		{"npm-flag-help", "--help"},
		{"cargo-flag-registry", "--registry=hostile"},
		{"flag-bare-dash", "-x"},

		{"backtick", "pkg`whoami`"},
		{"dollar-paren", "pkg$(whoami)"},
		{"pipe", "pkg|ls"},
		{"redirect", "pkg>/tmp/out"},
		{"ampersand", "pkg&"},
		{"glob-star", "pkg*"},
		{"glob-question", "pkg?"},
		{"space", "pkg name"},
		{"tab", "pkg\tname"},

		{"path-traversal", "../etc/passwd"},
		{"file-scheme", "file:///etc/passwd"},
		{"https-scheme", "https://attacker.example/exec"},

		{"unicode-greek", "πkg.sym"},
		{"unicode-cyrillic", "паkg.sym"},
		{"null-byte", "pkg\x00trailer"},

		{"empty", ""},
		{"whitespace", "   "},
		{"only-dots", "..."},
		{"only-separator", "::"},
		{"trailing-separator", "pkg."},
		{"leading-separator", ".pkg"},
		{"double-slash", "pkg//sub"},

		{"numeric-start", "1pkg"},
		{"numeric-start-segment", "pkg.1sub"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSymbolRef(SymbolRef{Ecosystem: EcoGo, SymbolPath: c.path, Version: "1"})
			if err == nil {
				t.Errorf("validateSymbolRef(%q) should have rejected hostile input", c.path)
			}
		})
	}
}

func TestValidateSymbolRef_RejectsOverLengthPath(t *testing.T) {
	long := strings.Repeat("a", maxSymbolPathLen+1)
	err := validateSymbolRef(SymbolRef{Ecosystem: EcoGo, SymbolPath: long, Version: "1"})
	if err == nil {
		t.Fatalf("expected error on over-length symbol path")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error should mention length cap; got %v", err)
	}
}

func TestValidateSymbolRef_AtCapBoundary_Accepted(t *testing.T) {
	atCap := strings.Repeat("a", maxSymbolPathLen)
	err := validateSymbolRef(SymbolRef{Ecosystem: EcoGo, SymbolPath: atCap, Version: "1"})
	if err != nil {
		t.Errorf("at-cap path should be accepted; got %v", err)
	}
}

func TestVerifier_HostileSymbolPath_SkipsCascade(t *testing.T) {
	si := newFakeSymbolIndex(nil)
	fake := &fakeLiveCmdRunner{results: map[string]liveCmdResult{}}
	v := newTestVerifier(t, si, fake)

	hostile := []string{
		`os; __import__('os').system('whoami')#.x`,
		"--registry=evil.example",
		"pkg`whoami`",
		"",
		"../etc/passwd",
	}
	for _, h := range hostile {
		t.Run(h, func(t *testing.T) {
			out, err := v.Verify(context.Background(), []SymbolRef{
				{Ecosystem: EcoGo, SymbolPath: h, Version: "1.22"},
			})
			if err != nil {
				t.Fatalf("Verify must not surface error on hostile input (returns skipped); got %v", err)
			}
			if out.AllVerified {
				t.Errorf("AllVerified must be false on hostile input")
			}
			if out.Verifications[0].Source != string(VerifySourceSkipped) {
				t.Errorf("expected source=skipped on hostile input; got %s", out.Verifications[0].Source)
			}
			if out.Verifications[0].Exists {
				t.Errorf("Exists must be false on hostile input")
			}
		})
	}
	// Runner MUST NOT have been invoked for any hostile ref.
	if fake.calls != 0 {
		t.Errorf("LiveCmdRunner must NOT be invoked on hostile SymbolPath; got %d calls", fake.calls)
	}
}

func TestVerifier_HostileSymbolPath_BypassesStageA(t *testing.T) {
	hostile := `os; __import__('os').system('whoami')#.x`

	si := newFakeSymbolIndex(map[Ecosystem]map[string]string{
		EcoGo: {hostile: "this-must-not-be-served"},
	})
	v := newTestVerifier(t, si, nil)
	out, err := v.Verify(context.Background(), []SymbolRef{
		{Ecosystem: EcoGo, SymbolPath: hostile, Version: "1"},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Verifications[0].Source != string(VerifySourceSkipped) {
		t.Errorf("expected source=skipped (gate before stage A); got %s", out.Verifications[0].Source)
	}
	if out.Verifications[0].Signature == "this-must-not-be-served" {
		t.Errorf("signature from hostile index entry leaked through gate")
	}
}

type fakeSymbolIndex struct {
	byEco map[Ecosystem]map[string]string
}

func newFakeSymbolIndex(m map[Ecosystem]map[string]string) *fakeSymbolIndex {
	if m == nil {
		m = map[Ecosystem]map[string]string{}
	}
	return &fakeSymbolIndex{byEco: m}
}

func (f *fakeSymbolIndex) Contains(eco Ecosystem, symbolPath, version string) (string, bool) {
	if m, ok := f.byEco[eco]; ok {
		sig, exists := m[symbolPath]
		return sig, exists
	}
	return "", false
}

type fakeLiveCmdRunner struct {
	mu      sync.Mutex
	results map[string]liveCmdResult
	calls   int
}

func (f *fakeLiveCmdRunner) Run(ctx context.Context, eco Ecosystem, ref SymbolRef) (liveCmdResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	key := livePathKeyForTest(eco, ref.Version, ref.SymbolPath)
	res, ok := f.results[key]
	if !ok {
		return liveCmdResult{Exists: false}, nil
	}
	if res.Err != nil {
		return liveCmdResult{}, res.Err
	}
	return res, nil
}

func livePathKeyForTest(eco Ecosystem, version, path string) string {
	return string(eco) + ":" + version + ":" + path
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type advancingClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(c.step)
	return c.now
}

func newTestVerifier(t *testing.T, si SymbolIndexLookup, runner LiveCmdRunner) *Verifier {
	return newTestVerifierWithClock(t, si, runner, &fakeClock{now: time.Now()}, 24*time.Hour)
}

func newTestVerifierWithClock(t *testing.T, si SymbolIndexLookup, runner LiveCmdRunner, clock Clock, ttl time.Duration) *Verifier {
	t.Helper()
	cfg := VerifierConfig{
		SymbolIndex:   si,
		LiveCmdRunner: runner,
		LRUSize:       1000,
		LRUTTL:        ttl,
		Clock:         clock,
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}
