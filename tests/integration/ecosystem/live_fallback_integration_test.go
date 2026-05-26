//go:build integration && cgo

// Package ecosystem_test — live_fallback_integration_test.go
//
// LiveFallback (§4.3) substrate: the RAGAuditEmitter + DoctrineProfile
// + chain wiring that LiveFallback (Dispatcher.LiveFallback) depends on.
//
// Scope rationale — what's reachable from external pkg:
//
//   - The full Dispatcher.LiveFallback call path requires injecting
//     `sources`, `researchMCP`, `findingsCache`, `indexerDelta` —
//     these are PACKAGE-PRIVATE Dispatcher fields wired only via
//     same-package fixture construction (dispatcher_fallback_test.go
//     buildFallbackFixture). Cross-package callers cannot exercise the
//     full fallback flow through the public NewDispatcher Options.
//
//   - WHAT EXTERNAL PACKAGES CAN VERIFY: the LiveFallback contract's
//     audit-emission shape (inv-zen-203 "fresh_dispatch=true on EvtRAGQuery
//     emitted BEFORE any downstream dispatch") is enforced by the
//     RAGAuditEmitter + InMemoryRAGAuditChain primitives — both of which
//     ARE exported. This file proves the documented contract holds
//     end-to-end through the public surface (NewRAGAuditEmitter,
//     NewInMemoryRAGAuditChain, RAGQueryPayload, the canonical 8 event
//     types), independently of the Dispatcher orchestration.
//
//   - In particular: the Audit-FIRST ordering guarantee (§4.3 inv-zen-203
//     "audit chain captures dispatch reason BEFORE any downstream live
//     network call") is testable cross-package by driving the
//     RAGAuditEmitter directly + asserting the chain row is persisted
//     atomically with fresh_dispatch=true visible in the payload.
//
// Why this complements the same-package dispatcher_fallback_test.go:
//
//   - dispatcher_fallback_test.go covers the full 7 LiveFallback scenarios
//     using package-private fixture wiring. Those tests succeed when the
//     SAME-package internals agree.
//
//   - THIS file's tests cover the EXTERNAL-package contract: a downstream
//     consumer (daemon Phase F, future integrations, third-party
//     subscribers) that depends on the emitted audit row shape MUST be
//     able to round-trip RAGQueryPayload through the chain. Any drift in
//     the payload JSON encoding (renamed field, new mandatory field,
//     type change) surfaces here from the external-package perspective.
//
// Build tags `integration && cgo` match the directory convention.
package ecosystem_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

func newLiveFallbackTestEmitter(t *testing.T) (*ecosystem.InMemoryRAGAuditChain, *ecosystem.RAGAuditEmitter) {
	t.Helper()
	chain := ecosystem.NewInMemoryRAGAuditChain()
	profile := &ecosystem.DoctrineProfile{
		Name:               "default",
		AuditEmissionLevel: ecosystem.AuditAll8Events,
	}
	emitter := ecosystem.NewRAGAuditEmitter(chain, profile)
	return chain, emitter
}

// TestLiveFallbackIntegration_AuditEmittedBeforeAnyDispatch enforces the
// inv-zen-203 ordering guarantee from the EXTERNAL-package perspective:
// when the LiveFallback path emits its EvtRAGQuery row, the row MUST
// land in the chain WITH `fresh_dispatch=true` in the payload, BEFORE
// any downstream operation runs. This is verified by:
//
//	(a) emitting one EvtRAGQuery payload via the public RAGAuditEmitter
//	    surface (the same surface LiveFallback consumes internally)
//	(b) asserting chain.Len() reports the row immediately (synchronous
//	    persistence — no buffering / async dispatch in the emitter)
//	(c) asserting payload.fresh_dispatch decodes to true round-trip
//	(d) asserting payload.doctrine is the configured profile name
//
// A regression that buffers Emit calls (async-flush mode), drops the
// fresh_dispatch field, or coerces it through a different JSON encoder
// surfaces here as a chain-row read failure.
func TestLiveFallbackIntegration_AuditEmittedBeforeAnyDispatch(t *testing.T) {
	chain, emitter := newLiveFallbackTestEmitter(t)

	payload := ecosystem.RAGQueryPayload{
		Query:         "newly-released github.com/new/pkg usage",
		Ecosystem:     ecosystem.EcoGo,
		Doctrine:      "default",
		FreshDispatch: true,
		ProjectPath:   "/Users/op/repo",
	}
	ctx, cancel := contextWithTimeoutDefault()
	defer cancel()

	seq, err := emitter.Emit(ctx, eventlog.EvtRAGQuery, payload)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if seq != 1 {
		t.Errorf("seq = %d, want 1 (genesis row)", seq)
	}

	if got, want := chain.Len(), 1; got != want {
		t.Fatalf("chain.Len = %d, want %d (Emit must persist synchronously per inv-zen-203)",
			got, want)
	}

	rec := chain.Get(1)
	if rec == nil {
		t.Fatal("Get(1) returned nil")
	}
	if rec.EventType != eventlog.EvtRAGQuery {
		t.Errorf("EventType = %d, want EvtRAGQuery (=%d)", rec.EventType, eventlog.EvtRAGQuery)
	}

	// Payload round-trip: fresh_dispatch MUST decode to true.
	var decoded ecosystem.RAGQueryPayload
	if err := json.Unmarshal(rec.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v (raw=%q)", err, string(rec.Payload))
	}
	if !decoded.FreshDispatch {
		t.Errorf("decoded.FreshDispatch = false; want true (inv-zen-203 fresh_dispatch flag MUST round-trip)")
	}
	if decoded.Doctrine != "default" {
		t.Errorf("decoded.Doctrine = %q, want %q", decoded.Doctrine, "default")
	}
	if decoded.Query != payload.Query {
		t.Errorf("decoded.Query = %q, want %q", decoded.Query, payload.Query)
	}
	if decoded.Ecosystem != ecosystem.EcoGo {
		t.Errorf("decoded.Ecosystem = %q, want %q", decoded.Ecosystem, ecosystem.EcoGo)
	}
	if decoded.ProjectPath != payload.ProjectPath {
		t.Errorf("decoded.ProjectPath = %q, want %q", decoded.ProjectPath, payload.ProjectPath)
	}
}

// TestLiveFallbackIntegration_FreshDispatchFalseDistinguishable verifies
// the OMITTED case: a non-fresh-dispatch query emits a payload whose
// decoded FreshDispatch reads false. The wire-format contract uses
// `omitempty` on FreshDispatch, so a non-fresh emit + a fresh emit MUST
// both round-trip to their declared boolean.
//
// Why this matters: a downstream consumer (the ecosystem doctor probe,
// the metrics aggregator) needs to distinguish "cached corpus hit"
// (fresh_dispatch absent / false) from "live-network fallback fired"
// (fresh_dispatch=true). Drift that always serializes `false` or
// always elides the field breaks the consumer's count of fallback events.
func TestLiveFallbackIntegration_FreshDispatchFalseDistinguishable(t *testing.T) {
	chain, emitter := newLiveFallbackTestEmitter(t)
	ctx, cancel := contextWithTimeoutDefault()
	defer cancel()

	if _, err := emitter.Emit(ctx, eventlog.EvtRAGQuery, ecosystem.RAGQueryPayload{
		Query: "cached corpus query", Doctrine: "default",
		Ecosystem: ecosystem.EcoGo, FreshDispatch: false,
	}); err != nil {
		t.Fatalf("Emit non-fresh: %v", err)
	}
	if _, err := emitter.Emit(ctx, eventlog.EvtRAGQuery, ecosystem.RAGQueryPayload{
		Query: "uncached fallback query", Doctrine: "default",
		Ecosystem: ecosystem.EcoGo, FreshDispatch: true,
	}); err != nil {
		t.Fatalf("Emit fresh: %v", err)
	}

	if chain.Len() != 2 {
		t.Fatalf("chain.Len = %d, want 2", chain.Len())
	}

	rec1 := chain.Get(1)
	rec2 := chain.Get(2)
	if rec1 == nil || rec2 == nil {
		t.Fatalf("Get returned nil; rec1=%v rec2=%v", rec1, rec2)
	}

	var d1, d2 ecosystem.RAGQueryPayload
	if err := json.Unmarshal(rec1.Payload, &d1); err != nil {
		t.Fatalf("unmarshal rec1: %v", err)
	}
	if err := json.Unmarshal(rec2.Payload, &d2); err != nil {
		t.Fatalf("unmarshal rec2: %v", err)
	}

	if d1.FreshDispatch {
		t.Errorf("rec1.FreshDispatch = true; want false (non-fresh path)")
	}
	if !d2.FreshDispatch {
		t.Errorf("rec2.FreshDispatch = false; want true (fresh-dispatch path)")
	}
}

func TestLiveFallbackIntegration_SynthesisPathFullEventSequence(t *testing.T) {
	chain, emitter := newLiveFallbackTestEmitter(t)
	ctx, cancel := contextWithTimeoutDefault()
	defer cancel()

	canonicalSequence := []struct {
		evt     eventlog.EventType
		payload any
	}{
		{eventlog.EvtRAGQuery, ecosystem.RAGQueryPayload{
			Query: "synthesize obscure package docs", Ecosystem: ecosystem.EcoGo,
			Doctrine: "default", FreshDispatch: true,
		}},
		{eventlog.EvtRAGRetrieval, map[string]any{"fused_count": 0, "ecosystems": []string{"go"}}},
		{eventlog.EvtRAGCitation, map[string]any{"chunks": []int64{}}},
		{eventlog.EvtRAGVerify, map[string]any{"refs": 0, "all_verified": true}},
		{eventlog.EvtRAGAnswer, map[string]any{"answer_hash": "abc123", "cited_chunks": []int64{}}},
	}

	for i, step := range canonicalSequence {
		seq, err := emitter.Emit(ctx, step.evt, step.payload)
		if err != nil {
			t.Fatalf("Emit step %d (%s): %v", i, step.evt, err)
		}
		if got, want := seq, int64(i+1); got != want {
			t.Errorf("step %d seq = %d, want %d", i, got, want)
		}
	}

	if got, want := chain.Len(), len(canonicalSequence); got != want {
		t.Fatalf("chain.Len = %d, want %d", got, want)
	}

	for i, step := range canonicalSequence {
		rec := chain.Get(int64(i + 1))
		if rec == nil {
			t.Fatalf("Get(%d) returned nil", i+1)
		}
		if rec.EventType != step.evt {
			t.Errorf("row %d: EventType = %d, want %d", i+1, rec.EventType, step.evt)
		}
	}

	// The first event MUST carry fresh_dispatch=true (LiveFallback
	// inv-zen-203 enforcement). A regression that emits Query AFTER
	// Retrieval (wrong order) would break this even if the chain itself
	// records the events.
	first := chain.Get(1)
	if first == nil {
		t.Fatal("Get(1) returned nil")
	}
	var qpayload ecosystem.RAGQueryPayload
	if err := json.Unmarshal(first.Payload, &qpayload); err != nil {
		t.Fatalf("unmarshal first payload: %v", err)
	}
	if !qpayload.FreshDispatch {
		t.Errorf("first event payload.FreshDispatch = false; want true (inv-zen-203)")
	}

	seqs := chain.AllSeqs()
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for i, s := range seqs {
		if want := int64(i + 1); s != want {
			t.Errorf("seqs[%d] = %d, want %d", i, s, want)
		}
	}
}

// TestLiveFallbackIntegration_AbstainOnEmptyResult mirrors the
// dispatcher_fallback path where a live-fallback finds zero candidates
// across all ecosystems → MUST emit EvtRAGAbstain instead of silently
// returning empty results. Documents the cross-package contract that
// downstream consumers depend on (the metrics aggregator counts
// abstentions; the doctor probe surfaces the abstain rate).
func TestLiveFallbackIntegration_AbstainOnEmptyResult(t *testing.T) {
	chain, emitter := newLiveFallbackTestEmitter(t)
	ctx, cancel := contextWithTimeoutDefault()
	defer cancel()

	if _, err := emitter.Emit(ctx, eventlog.EvtRAGQuery, ecosystem.RAGQueryPayload{
		Query: "no candidates anywhere", Ecosystem: ecosystem.EcoRust,
		Doctrine: "default", FreshDispatch: true,
	}); err != nil {
		t.Fatalf("Emit Query: %v", err)
	}

	if _, err := emitter.Emit(ctx, eventlog.EvtRAGAbstain, map[string]any{
		"reason":    "empty_candidates",
		"ecosystem": "rust",
		"doctrine":  "default",
	}); err != nil {
		t.Fatalf("Emit Abstain: %v", err)
	}

	if got, want := chain.Len(), 2; got != want {
		t.Fatalf("chain.Len = %d, want %d (Query + Abstain pair)", got, want)
	}

	rec := chain.Get(2)
	if rec == nil {
		t.Fatal("Get(2) returned nil")
	}
	if rec.EventType != eventlog.EvtRAGAbstain {
		t.Errorf("row 2 EventType = %d, want EvtRAGAbstain (=%d)",
			rec.EventType, eventlog.EvtRAGAbstain)
	}

	var decoded map[string]any
	if err := json.Unmarshal(rec.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal Abstain payload: %v", err)
	}
	reason, ok := decoded["reason"].(string)
	if !ok || reason == "" {
		t.Errorf("Abstain payload missing required `reason` field; got %v", decoded)
	}
	if reason != "empty_candidates" {
		t.Errorf("Abstain.reason = %q, want %q", reason, "empty_candidates")
	}
}

// TestLiveFallbackIntegration_DoctrineLabelPropagatesEndToEnd verifies
// the doctrine name set on the emitter's bound DoctrineProfile lands in
// the chain row's Doctrine field. The LiveFallback path documents this
// in spec §4.6: every chain row carries the doctrine name that governed
// the emission. Downstream consumers (the doctor probe, the audit
// reconciler) MUST be able to filter audit rows by doctrine at query
// time.
//
// A regression that drops the doctrine label, or sets it to a hardcoded
// "default" regardless of the bound profile, fires here for the
// "max-scope" sub-test.
func TestLiveFallbackIntegration_DoctrineLabelPropagatesEndToEnd(t *testing.T) {
	for _, doctrineName := range []string{"default", "max-scope", "capa-firewall"} {
		t.Run(doctrineName, func(t *testing.T) {
			chain := ecosystem.NewInMemoryRAGAuditChain()
			profile := &ecosystem.DoctrineProfile{
				Name:               doctrineName,
				AuditEmissionLevel: ecosystem.AuditAll8Events,
			}
			emitter := ecosystem.NewRAGAuditEmitter(chain, profile)

			ctx, cancel := contextWithTimeoutDefault()
			defer cancel()

			if _, err := emitter.Emit(ctx, eventlog.EvtRAGQuery, ecosystem.RAGQueryPayload{
				Query: "any", Ecosystem: ecosystem.EcoGo, Doctrine: doctrineName,
			}); err != nil {
				t.Fatalf("Emit: %v", err)
			}

			rec := chain.Get(1)
			if rec == nil {
				t.Fatal("Get(1) returned nil")
			}
			if rec.Doctrine != doctrineName {
				t.Errorf("chain row Doctrine = %q, want %q (profile.Name propagation)",
					rec.Doctrine, doctrineName)
			}
		})
	}
}

func contextWithTimeoutDefault() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30_000_000_000)
}
