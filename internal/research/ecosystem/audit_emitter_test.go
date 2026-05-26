// internal/research/ecosystem/audit_emitter_test.go
//
// Tests for RAGAuditEmitter + RAGAuditChainEmitter interface + InMemoryRAGAuditChain
// (Plan 14 Phase A Task A-7; see plan-file lines 4279-4542 for canonical test set).
//
// Coverage discipline: per project doctrine `feedback_no_tech_debt.md`,
// security/correctness-critical files require ≥90% per-function coverage.
// Tests cover all branches of RAGAuditEmitter.Emit (ctx.Err / shouldEmit /
// marshal / chain.Append) AND every case arm of shouldEmit (AuditAll8Events,
// AuditQueryAbstainVerifyFailureAnswer, AuditMinimal, unknown-default).

package ecosystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestInMemoryRAGAuditChainImplementsInterface(t *testing.T) {
	var _ RAGAuditChainEmitter = (*InMemoryRAGAuditChain)(nil)
}

func TestRAGAuditEmitterEmitReturnsMonotonicSeq(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})

	ctx := context.Background()
	seq1, err := e.Emit(ctx, eventlog.EvtRAGQuery, map[string]string{"q": "foo"})
	if err != nil {
		t.Fatalf("Emit #1: %v", err)
	}
	seq2, err := e.Emit(ctx, eventlog.EvtRAGRetrieval, map[string]int{"n": 5})
	if err != nil {
		t.Fatalf("Emit #2: %v", err)
	}
	if seq2 <= seq1 {
		t.Errorf("seq2 = %d; want > seq1 (%d)", seq2, seq1)
	}
}

func TestRAGAuditEmitterEmitWritesAllEightEventTypes(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx := context.Background()
	for _, evt := range []eventlog.EventType{
		eventlog.EvtRAGQuery, eventlog.EvtRAGRetrieval, eventlog.EvtRAGCitation,
		eventlog.EvtRAGVerify, eventlog.EvtRAGAbstain, eventlog.EvtRAGAnswer,
		eventlog.EvtRAGIngestPackage, eventlog.EvtRAGIngestJoinKey,
	} {
		if _, err := e.Emit(ctx, evt, map[string]string{"evt": evt.String()}); err != nil {
			t.Fatalf("Emit(%s): %v", evt.String(), err)
		}
	}
	if got := chain.Len(); got != 8 {
		t.Errorf("chain len = %d; want 8", got)
	}
}

func TestRAGAuditEmitterChainLinkHashConsistency(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx := context.Background()

	payload1 := map[string]string{"q": "hash a file"}
	seq1, err := e.Emit(ctx, eventlog.EvtRAGQuery, payload1)
	if err != nil {
		t.Fatalf("Emit #1: %v", err)
	}

	payload2 := map[string]int{"candidates": 50}
	seq2, err := e.Emit(ctx, eventlog.EvtRAGRetrieval, payload2)
	if err != nil {
		t.Fatalf("Emit #2: %v", err)
	}

	r1 := chain.Get(seq1)
	r2 := chain.Get(seq2)
	if r1 == nil || r2 == nil {
		t.Fatalf("chain.Get returned nil: r1=%v r2=%v", r1, r2)
	}

	// Parent hash chain: r1.ParentHash == "" (genesis) OR matches prior tip.
	// r2.ParentHash MUST == r1.SelfHash (link consistency).
	if r2.ParentHash != r1.SelfHash {
		t.Errorf("chain link drift: r2.ParentHash = %q; want r1.SelfHash = %q",
			r2.ParentHash, r1.SelfHash)
	}

	// Self-hash MUST equal sha256(seq || evt || payload || parent_hash).
	// Verify r2 against the formula directly.
	payloadJSON2, _ := json.Marshal(payload2)
	expectedR2Hash := computeChainHash(seq2, eventlog.EvtRAGRetrieval, payloadJSON2, r1.SelfHash)
	if r2.SelfHash != expectedR2Hash {
		t.Errorf("r2.SelfHash = %q; want %q (formula sha256(seq||evt||payload||parent))",
			r2.SelfHash, expectedR2Hash)
	}
}

func TestRAGAuditEmitterCanonicalOrderAppendOnly(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx := context.Background()

	// Emit in non-monotonic order; chain MUST still assign monotonic seq
	// in CALL order (not in EventType order).
	evtOrder := []eventlog.EventType{
		eventlog.EvtRAGAnswer,
		eventlog.EvtRAGQuery,
		eventlog.EvtRAGCitation,
		eventlog.EvtRAGRetrieval,
	}
	var seqs []int64
	for _, evt := range evtOrder {
		seq, err := e.Emit(ctx, evt, map[string]string{})
		if err != nil {
			t.Fatalf("Emit(%s): %v", evt.String(), err)
		}
		seqs = append(seqs, seq)
	}
	// Seqs MUST be monotonically increasing.
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("seq[%d]=%d <= seq[%d]=%d (monotonic violation; inv-zen-197)",
				i, seqs[i], i-1, seqs[i-1])
		}
	}
}

func TestRAGAuditEmitterEmitRespectsDoctrineEmissionLevel(t *testing.T) {

	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "default",
		AuditEmissionLevel: AuditMinimal,
	})
	ctx := context.Background()

	if _, err := e.Emit(ctx, eventlog.EvtRAGQuery, nil); err != nil {
		t.Errorf("Emit(EvtRAGQuery) under AuditMinimal: unexpected error %v", err)
	}

	seqRetrieval, err := e.Emit(ctx, eventlog.EvtRAGRetrieval, nil)
	if err != nil {
		t.Errorf("Emit(EvtRAGRetrieval) under AuditMinimal: unexpected error %v", err)
	}
	if seqRetrieval != 0 {
		t.Errorf("Emit(EvtRAGRetrieval) under AuditMinimal: returned seq %d; want 0 (short-circuit)",
			seqRetrieval)
	}

	if _, err := e.Emit(ctx, eventlog.EvtRAGAbstain, nil); err != nil {
		t.Errorf("Emit(EvtRAGAbstain) under AuditMinimal: unexpected error %v", err)
	}

	if got := chain.Len(); got != 2 {
		t.Errorf("AuditMinimal chain len = %d; want 2 (Query + Abstain only)", got)
	}
}

func TestRAGAuditEmitterShouldEmitQueryAbstainVerifyAnswerFiltering(t *testing.T) {

	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "default",
		AuditEmissionLevel: AuditQueryAbstainVerifyFailureAnswer,
	})
	ctx := context.Background()

	admitted := []eventlog.EventType{
		eventlog.EvtRAGQuery,
		eventlog.EvtRAGAbstain,
		eventlog.EvtRAGVerify,
		eventlog.EvtRAGAnswer,
	}
	for _, evt := range admitted {
		seq, err := e.Emit(ctx, evt, map[string]string{})
		if err != nil {
			t.Errorf("Emit(%s): unexpected error %v", evt.String(), err)
		}
		if seq == 0 {
			t.Errorf("Emit(%s): seq 0 — wrongly filtered (want admitted)", evt.String())
		}
	}

	filtered := []eventlog.EventType{
		eventlog.EvtRAGRetrieval,
		eventlog.EvtRAGCitation,
		eventlog.EvtRAGIngestPackage,
		eventlog.EvtRAGIngestJoinKey,
	}
	for _, evt := range filtered {
		seq, err := e.Emit(ctx, evt, map[string]string{})
		if err != nil {
			t.Errorf("Emit(%s): unexpected error %v (should silent-drop)", evt.String(), err)
		}
		if seq != 0 {
			t.Errorf("Emit(%s): seq %d; want 0 (short-circuit filter)", evt.String(), seq)
		}
	}

	if got := chain.Len(); got != len(admitted) {
		t.Errorf("AuditQueryAbstainVerifyFailureAnswer chain len = %d; want %d (admitted only)",
			got, len(admitted))
	}
}

func TestRAGAuditEmitterShouldEmitUnknownLevelConservativeEmit(t *testing.T) {

	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "custom-bad-level",
		AuditEmissionLevel: AuditLevel("nonsense-level-not-in-enum"),
	})
	ctx := context.Background()

	for _, evt := range []eventlog.EventType{
		eventlog.EvtRAGQuery, eventlog.EvtRAGRetrieval, eventlog.EvtRAGCitation,
		eventlog.EvtRAGVerify, eventlog.EvtRAGAbstain, eventlog.EvtRAGAnswer,
		eventlog.EvtRAGIngestPackage, eventlog.EvtRAGIngestJoinKey,
	} {
		seq, err := e.Emit(ctx, evt, nil)
		if err != nil {
			t.Errorf("Emit(%s) unknown-level: unexpected error %v", evt.String(), err)
		}
		if seq == 0 {
			t.Errorf("Emit(%s) unknown-level: seq 0 — expected conservative emit-all",
				evt.String())
		}
	}
	if got := chain.Len(); got != 8 {
		t.Errorf("unknown-level chain len = %d; want 8 (conservative emit-all)", got)
	}
}

func TestRAGAuditEmitterEmitContextCancel(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Emit(ctx, eventlog.EvtRAGQuery, nil); err == nil {
		t.Errorf("Emit(cancelled-ctx): want error; got nil")
	}
}

func TestRAGAuditEmitterEmitJSONMarshalError(t *testing.T) {

	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx := context.Background()

	type bad struct {
		Ch chan int
	}
	_, err := e.Emit(ctx, eventlog.EvtRAGQuery, bad{Ch: make(chan int)})
	if err == nil {
		t.Errorf("Emit(unmarshallable): want error; got nil")
	}
	if got := chain.Len(); got != 0 {
		t.Errorf("chain len = %d; want 0 (marshal err must short-circuit before Append)", got)
	}
}

func TestRAGAuditEmitterEmitChainAppendError(t *testing.T) {

	failChain := &failingChain{err: errors.New("disk full")}
	e := NewRAGAuditEmitter(failChain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	if _, err := e.Emit(context.Background(), eventlog.EvtRAGQuery, nil); err == nil {
		t.Errorf("Emit(chain-error): want error; got nil")
	}
}

func TestInMemoryRAGAuditChainLastHashReturnsTip(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx := context.Background()

	tip, err := chain.LastHash(ctx)
	if err != nil {
		t.Fatalf("LastHash on empty chain: %v", err)
	}
	if tip != "" {
		t.Errorf("empty chain tip = %q; want \"\" (genesis)", tip)
	}

	if _, err := chain.Append(ctx, eventlog.EvtRAGQuery, []byte(`{"a":1}`), "default"); err != nil {
		t.Fatalf("Append #1: %v", err)
	}
	if _, err := chain.Append(ctx, eventlog.EvtRAGRetrieval, []byte(`{"b":2}`), "default"); err != nil {
		t.Fatalf("Append #2: %v", err)
	}

	tip2, err := chain.LastHash(ctx)
	if err != nil {
		t.Fatalf("LastHash after appends: %v", err)
	}
	if tip2 == "" {
		t.Errorf("tip after appends = empty; want non-empty hash")
	}

	// LastHash MUST equal the second record's SelfHash.
	r2 := chain.Get(2)
	if r2 == nil || r2.SelfHash != tip2 {
		t.Errorf("LastHash = %q; want r2.SelfHash = %q", tip2, r2.SelfHash)
	}
}

func TestInMemoryRAGAuditChainSealPartitionRecords(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx := context.Background()
	partID := "2026-05"

	if err := chain.SealPartition(ctx, partID); err != nil {
		t.Fatalf("SealPartition: %v", err)
	}
	seals := chain.Seals()
	if got := seals[partID]; got.IsZero() {
		t.Errorf("partition %q not sealed", partID)
	}

	if err := chain.SealPartition(ctx, partID); err != nil {
		t.Errorf("SealPartition (re-seal): %v", err)
	}
}

func TestInMemoryRAGAuditChainAppendContextCancel(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chain.Append(ctx, eventlog.EvtRAGQuery, []byte("{}"), "max-scope"); err == nil {
		t.Errorf("Append(cancelled-ctx): want error; got nil")
	}
}

func TestInMemoryRAGAuditChainLastHashContextCancel(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chain.LastHash(ctx); err == nil {
		t.Errorf("LastHash(cancelled-ctx): want error; got nil")
	}
}

func TestInMemoryRAGAuditChainSealPartitionContextCancel(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := chain.SealPartition(ctx, "2026-05"); err == nil {
		t.Errorf("SealPartition(cancelled-ctx): want error; got nil")
	}
}

func TestInMemoryRAGAuditChainGetOutOfRange(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx := context.Background()
	if _, err := chain.Append(ctx, eventlog.EvtRAGQuery, []byte("{}"), "max-scope"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if r := chain.Get(0); r != nil {
		t.Errorf("Get(0) = %v; want nil", r)
	}
	if r := chain.Get(-1); r != nil {
		t.Errorf("Get(-1) = %v; want nil", r)
	}
	if r := chain.Get(2); r != nil {
		t.Errorf("Get(2) (past end) = %v; want nil", r)
	}

	if r := chain.Get(1); r == nil {
		t.Errorf("Get(1) = nil; want record")
	}
}

func TestInMemoryRAGAuditChainAllSeqsAndSealsIntrospection(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	ctx := context.Background()

	if got := chain.AllSeqs(); len(got) != 0 {
		t.Errorf("AllSeqs empty = %v; want []", got)
	}
	if got := chain.Seals(); len(got) != 0 {
		t.Errorf("Seals empty = %v; want {}", got)
	}

	for i := 0; i < 2; i++ {
		if _, err := chain.Append(ctx, eventlog.EvtRAGRetrieval, []byte("{}"), "max-scope"); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	seqs := chain.AllSeqs()
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Errorf("AllSeqs = %v; want [1 2]", seqs)
	}

	// Seals copy: mutating the returned map MUST NOT mutate internal state.
	_ = chain.SealPartition(ctx, "2026-05")
	sealsCopy := chain.Seals()
	if len(sealsCopy) != 1 {
		t.Errorf("Seals after one seal = %v; want 1 entry", sealsCopy)
	}
	sealsCopy["mutation-attempt"] = time.Now()
	if got := chain.Seals(); len(got) != 1 {
		t.Errorf("internal seals mutated via returned map: len = %d; want 1", len(got))
	}
}

func TestNewRAGAuditEmitterNilChainPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewRAGAuditEmitter(nil chain): want panic; got none")
		}
	}()
	_ = NewRAGAuditEmitter(nil, &DoctrineProfile{Name: "max-scope"})
}

func TestNewRAGAuditEmitterNilProfilePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("NewRAGAuditEmitter(nil profile): want panic; got none")
		}
	}()
	_ = NewRAGAuditEmitter(NewInMemoryRAGAuditChain(), nil)
}

func TestRAGAuditEmitterConcurrentEmit(t *testing.T) {
	chain := NewInMemoryRAGAuditChain()
	e := NewRAGAuditEmitter(chain, &DoctrineProfile{
		Name:               "max-scope",
		AuditEmissionLevel: AuditAll8Events,
	})
	ctx := context.Background()

	const numGoroutines = 50
	const emitsPerG = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < emitsPerG; i++ {
				if _, err := e.Emit(ctx, eventlog.EvtRAGRetrieval,
					map[string]int{"g": gid, "i": i}); err != nil {
					t.Errorf("Emit g=%d i=%d: %v", gid, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	want := numGoroutines * emitsPerG
	if got := chain.Len(); got != want {
		t.Errorf("concurrent Emit chain len = %d; want %d (race violation)", got, want)
	}

	// All seqs MUST be unique + monotonic.
	allSeqs := chain.AllSeqs()
	seen := make(map[int64]bool, len(allSeqs))
	for _, s := range allSeqs {
		if seen[s] {
			t.Errorf("duplicate seq: %d", s)
		}
		seen[s] = true
	}
}

type failingChain struct {
	err error
}

func (f *failingChain) Append(_ context.Context, _ eventlog.EventType, _ []byte, _ string) (int64, error) {
	return 0, f.err
}
func (f *failingChain) LastHash(_ context.Context) (string, error) { return "", f.err }
func (f *failingChain) SealPartition(_ context.Context, _ string) error {
	return f.err
}

func computeChainHash(seq int64, evt eventlog.EventType, payload []byte, parent string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|", seq, int(evt))
	h.Write(payload)
	fmt.Fprintf(h, "|%s", parent)
	return hex.EncodeToString(h.Sum(nil))
}
