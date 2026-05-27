// go:build integration && cgo

// Package ecosystem_test — chain_integrity_test.go
//
// Exercises the (RAGAuditChainEmitter × eventlog.EventType ×
// canonical-chain-formula) wiring across package boundaries that the
// per-package unit tests intentionally stop short of.
//
// The audit chain is the load-bearing trust surface for the RAG
// substrate. The spec §4.6 + invariant contract says:
//
// (a) every Append produces a strictly-monotonic seq (1, 2, 3,...)
// (b) every row's parent_hash MUST equal the previous row's self_hash
// (or "" for the genesis row)
// (c) every row's self_hash MUST match the canonical formula
// sha256(seq || event_int || payload || parent_hash) (mock_chain.go
// chainHashFormula — production wrapper uses the same encoding)
// (d) all 8 EventTypes 92..99 appear in the canonical declaration order
// when emitted sequentially
// (e) concurrent Append calls preserve seq monotonicity (the chain
// serializes under its own mutex; invariant partial)
//
// Cross-package signal: the chain primitive (mock_chain.go) consumes
// `eventlog.EventType` (declared in internal/orchestrator/eventlog) and
// stores it via the canonical formula; downstream readers
// (audit_emitter.go, RAGAuditEmitter, Indexer) re-encode events using
// the same formula. Drift in ANY of the 5 contracts above surfaces as a
// hash mismatch here, regardless of where the regression actually lives.
//
// Why this complements the unit-tier coverage:
// - mock_chain_test.go (same-package) covers Append/Get/LastHash/Seal
// mechanics with synthetic payloads.
// - audit_emitter_test.go (same-package) covers emitter→chain dispatch
// via the doctrine filter.
// - THIS file proves the canonical formula is stable across
// real-shaped event payloads that mix all 8 EventTypes (92..99),
// including across concurrent Append fan-out — the exact failure
// mode a regression in chainHashFormula would produce.
//
// Build tags `integration && cgo` match the directory convention.
package ecosystem_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

// canonicalChainHash re-derives a self_hash from the canonical formula
// documented in mock_chain.go::chainHashFormula. This intentionally
// re-encodes the formula here (vs calling a public helper) so a
// regression in chainHashFormula surfaces as a test failure rather than
// being silently propagated by sharing the same buggy encoder. Drift
// between this re-derivation and the chain's stored hash is the EXACT
// signal the test is looking for.
//
// Production wrapper at audit_emitter.go::computeChainHash MUST use the
// same encoding; if it ever diverges (different separator, different
// integer width, missing field), this test breaks.
func canonicalChainHash(seq int64, evt eventlog.EventType, payload []byte, parent string) string {
	h := sha256.New()

	fmt.Fprintf(h, "%d|%d|", seq, int(evt))
	h.Write(payload)
	fmt.Fprintf(h, "|%s", parent)
	return hex.EncodeToString(h.Sum(nil))
}

func TestChainIntegrity_HashContinuityAcrossAppends(t *testing.T) {
	chain := ecosystem.NewInMemoryRAGAuditChain()
	ctx := context.Background()

	type append struct {
		evt     eventlog.EventType
		payload []byte
	}
	appends := []append{
		{eventlog.EvtRAGQuery, []byte(`{"q":"how to format strings","eco":"go"}`)},
		{eventlog.EvtRAGRetrieval, []byte(`{"fused":50,"ecos":["go"]}`)},
		{eventlog.EvtRAGCitation, []byte(`{"chunks":[1,2,3]}`)},
		{eventlog.EvtRAGVerify, []byte(`{"verified":true,"refs":2}`)},
		{eventlog.EvtRAGAnswer, []byte(`{"hash":"abc123"}`)},
	}

	for i, a := range appends {
		seq, err := chain.Append(ctx, a.evt, a.payload, "default")
		if err != nil {
			t.Fatalf("append #%d (%s): %v", i, a.evt, err)
		}
		if got, want := seq, int64(i+1); got != want {
			t.Errorf("append #%d seq = %d, want %d (monotonic 1-based)", i, got, want)
		}
	}

	if got, want := chain.Len(), len(appends); got != want {
		t.Fatalf("chain.Len = %d, want %d", got, want)
	}

	prevSelfHash := ""
	for i := int64(1); i <= int64(len(appends)); i++ {
		rec := chain.Get(i)
		if rec == nil {
			t.Fatalf("Get(%d) returned nil; chain length=%d", i, chain.Len())
		}
		if got, want := rec.ParentHash, prevSelfHash; got != want {
			t.Errorf("row %d: ParentHash = %q, want %q (must equal previous SelfHash)",
				i, got, want)
		}
		expectedSelfHash := canonicalChainHash(rec.Seq, rec.EventType, rec.Payload, rec.ParentHash)
		if got, want := rec.SelfHash, expectedSelfHash; got != want {
			t.Errorf("row %d: SelfHash = %q, want %q (canonical formula re-derivation must match)",
				i, got, want)
		}
		prevSelfHash = rec.SelfHash
	}
}

func TestChainIntegrity_AllRagEventsAppendInOrder(t *testing.T) {
	chain := ecosystem.NewInMemoryRAGAuditChain()
	ctx := context.Background()

	canonical := []eventlog.EventType{
		eventlog.EvtRAGQuery,
		eventlog.EvtRAGRetrieval,
		eventlog.EvtRAGCitation,
		eventlog.EvtRAGVerify,
		eventlog.EvtRAGAbstain,
		eventlog.EvtRAGAnswer,
		eventlog.EvtRAGIngestPackage,
		eventlog.EvtRAGIngestJoinKey,
	}

	for i, evt := range canonical {
		want := 92 + i
		if got := int(evt); got != want {
			t.Errorf("canonical[%d] = %d, want %d (inv-zen-197 numerical assignment)",
				i, got, want)
		}
	}

	for i, evt := range canonical {
		payload, err := json.Marshal(map[string]any{"event": int(evt), "idx": i})
		if err != nil {
			t.Fatalf("marshal payload #%d: %v", i, err)
		}
		if _, err := chain.Append(ctx, evt, payload, "default"); err != nil {
			t.Fatalf("Append(%d, %s): %v", i, evt, err)
		}
	}

	for i, want := range canonical {
		rec := chain.Get(int64(i + 1))
		if rec == nil {
			t.Fatalf("Get(%d) returned nil", i+1)
		}
		if rec.EventType != want {
			t.Errorf("row %d: EventType = %d, want %d (inv-zen-197 order)",
				i+1, rec.EventType, want)
		}
	}
}

// TestChainIntegrity_ConcurrentAppendsPreserveMonotonicSeq drives N
// goroutines each issuing M Append calls. The chain MUST serialize
// internally so seqs come out as a strictly-increasing 1..(N×M) set
// with zero duplicates and zero gaps. invariant partial enforcement
// (full fan-out correctness covered by H-7 property tests).
//
// Cross-package signal: the chain's mutex contract MUST hold even when
// the call sites are heterogeneous (Indexer-driven Append, Dispatcher-
// driven Append, RAGAuditEmitter-driven Append). A regression that
// lock-strips internally (e.g. swaps RWMutex.RLock for the write path)
// surfaces as a seq collision here under `-race`.
func TestChainIntegrity_ConcurrentAppendsPreserveMonotonicSeq(t *testing.T) {
	chain := ecosystem.NewInMemoryRAGAuditChain()
	const goroutines = 8
	const perGoroutine = 25
	totalAppends := goroutines * perGoroutine

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = make([]int64, 0, totalAppends)
	)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				payload := []byte(fmt.Sprintf(`{"g":%d,"j":%d}`, gid, j))
				seq, err := chain.Append(ctx, eventlog.EvtRAGQuery, payload, "default")
				if err != nil {
					t.Errorf("g=%d j=%d Append: %v", gid, j, err)
					return
				}
				mu.Lock()
				seen = append(seen, seq)
				mu.Unlock()
			}
		}(g)
	}
	wg.Wait()

	if got, want := chain.Len(), totalAppends; got != want {
		t.Fatalf("chain.Len = %d, want %d (some Appends lost)", got, want)
	}

	sort.Slice(seen, func(i, j int) bool { return seen[i] < seen[j] })
	for i, s := range seen {
		if want := int64(i + 1); s != want {
			t.Fatalf("seen[%d] = %d, want %d (seqs must be the contiguous 1..N set)", i, s, want)
		}
	}

	// Re-verify hash chain integrity post-concurrent-fanout. Even with
	// interleaved Appends, each row's ParentHash MUST link to the
	// previous row's SelfHash (chain serialization invariant).
	prev := ""
	for i := int64(1); i <= int64(totalAppends); i++ {
		rec := chain.Get(i)
		if rec == nil {
			t.Fatalf("Get(%d) returned nil after concurrent fanout", i)
		}
		if rec.ParentHash != prev {
			t.Errorf("row %d: ParentHash = %q, want %q (chain link broken post-concurrency)",
				i, rec.ParentHash, prev)
		}
		expected := canonicalChainHash(rec.Seq, rec.EventType, rec.Payload, rec.ParentHash)
		if rec.SelfHash != expected {
			t.Errorf("row %d: SelfHash = %q, want %q (canonical formula mismatch)",
				i, rec.SelfHash, expected)
		}
		prev = rec.SelfHash
	}
}

// TestChainIntegrity_TamperedPayloadProducesHashMismatch is a defensive
// guard: take a recorded payload, mutate one byte locally, recompute the
// formula → the recomputed hash MUST differ from the stored SelfHash.
// Documents that the chain's tamper-evidence property holds end-to-end
// across the (payload bytes × canonical formula) cross-package contract.
//
// This is NOT testing the chain implementation (it can't tamper with
// itself); it's documenting the CONSUMER-side guarantee a verifier MUST
// rely on. A regression that, say, hashed only the FIRST 32 bytes of
// payload (silently truncating long payloads) would fail this test for
// any payload >32 bytes.
func TestChainIntegrity_TamperedPayloadProducesHashMismatch(t *testing.T) {
	chain := ecosystem.NewInMemoryRAGAuditChain()
	ctx := context.Background()

	original := []byte(`{"query":"original payload that is reasonably long to catch truncation"}`)
	if _, err := chain.Append(ctx, eventlog.EvtRAGQuery, original, "default"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	rec := chain.Get(1)
	if rec == nil {
		t.Fatal("Get(1) returned nil")
	}

	// The pristine recompute MUST match.
	if got := canonicalChainHash(rec.Seq, rec.EventType, rec.Payload, rec.ParentHash); got != rec.SelfHash {
		t.Fatalf("pristine recompute = %q, want %q", got, rec.SelfHash)
	}

	tampered := make([]byte, len(original))
	copy(tampered, original)
	tampered[len(tampered)-2] = 'X'

	tamperedHash := canonicalChainHash(rec.Seq, rec.EventType, tampered, rec.ParentHash)
	if tamperedHash == rec.SelfHash {
		t.Errorf("tampered hash matches stored hash; canonical formula has tamper-detection weakness (got %q)",
			tamperedHash)
	}
}

func TestChainIntegrity_IndexerEmittedRowMatchesCanonicalFormula(t *testing.T) {
	db := openEcosystemDBForCrossPackage(t)
	chain := ecosystem.NewInMemoryRAGAuditChain()

	idx, err := ecosystem.NewIndexer(ecosystem.IndexerOptions{
		DB:       db,
		Chain:    chain,
		Doctrine: "default",
	})
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}

	pkg := ecosystem.PackageRef{
		Ecosystem:           ecosystem.EcoPython,
		Name:                "argparse",
		CanonicalNamespace:  "argparse",
		UpstreamURL:         "https://docs.python.org/3/library/argparse.html",
		LatestStableVersion: "3.12.0",
	}
	chunks := []ecosystem.Chunk{buildDeterministicChunk(7, "3.12.0", ecosystem.KindFunction)}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := idx.WriteChunks(ctx, pkg, "3.12.0", chunks, nil, nil); err != nil {
		t.Fatalf("WriteChunks: %v", err)
	}
	if chain.Len() != 1 {
		t.Fatalf("chain.Len = %d, want 1", chain.Len())
	}

	rec := chain.Get(1)
	if rec == nil {
		t.Fatal("Get(1) returned nil after Indexer write")
	}
	expected := canonicalChainHash(rec.Seq, rec.EventType, rec.Payload, rec.ParentHash)
	if rec.SelfHash != expected {
		t.Errorf("Indexer-emitted row SelfHash = %q, want %q "+
			"(canonical formula must agree between Indexer payload encoder + chain hasher)",
			rec.SelfHash, expected)
	}
	if rec.EventType != eventlog.EvtRAGIngestPackage {
		t.Errorf("Indexer-emitted EventType = %d, want EvtRAGIngestPackage (=%d)",
			rec.EventType, eventlog.EvtRAGIngestPackage)
	}
}
