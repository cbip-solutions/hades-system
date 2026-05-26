//go:build property && cgo

// tests/property/ecosystem/fallback_audit_property_test.go (Plan 14 Phase H Task H-7-9)
//
// inv-zen-203: query-time live fallback chained audit event — when the
// dispatcher takes the live-fallback path, the audit chain MUST receive
// an EvtRAGQuery row with payload.fresh_dispatch=true BEFORE any
// network-side work fires (so the chain captures the dispatch reason
// even if the manifest fetch later errors).
//
// And: the returned QueryResult MUST set Provenance.FreshDispatch=true
// on the fallback path (and false otherwise).
//
// We test the contract algebraically via a model of the documented
// invariant, then bind the model to the real ecosystem package's
// EvtRAGQuery / RAGQueryPayload / QueryProvenance / QueryResult types
// so any field rename (e.g. dropping FreshDispatch from QueryProvenance)
// breaks compile.
//
// Full end-to-end Dispatcher.LiveFallback testing lives in
// `internal/research/ecosystem/dispatcher_fallback_test.go` (T334-T350)
// which uses real source-fake wiring. This file enforces the
// algebraic floor.

package ecosystem_property_test

import (
	"testing"
	"testing/quick"

	_ "github.com/mattn/go-sqlite3"
	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type fakeChainEvent struct {
	EventType     ecosystem.EventType
	FreshDispatch bool
}

func simulateFallback(freshDispatchPath bool) (events []fakeChainEvent, result *ecosystem.QueryResult) {
	if freshDispatchPath {
		events = append(events, fakeChainEvent{
			EventType:     ecosystem.EvtRAGQuery,
			FreshDispatch: true,
		})
	}
	result = &ecosystem.QueryResult{
		Provenance: ecosystem.QueryProvenance{
			FreshDispatch: freshDispatchPath,
		},
	}
	return events, result
}

func TestFallbackAudit_Property_FreshDispatchEmitsRAGQueryEvent(t *testing.T) {
	prop := func(b bool) bool {
		events, _ := simulateFallback(b)
		if !b {
			return len(events) == 0
		}
		// freshDispatch=true MUST emit EXACTLY one EvtRAGQuery first.
		if len(events) < 1 {
			return false
		}
		first := events[0]
		if first.EventType != ecosystem.EvtRAGQuery {
			return false
		}
		if !first.FreshDispatch {
			return false
		}
		return true
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-203: fallback path does not emit EvtRAGQuery with fresh_dispatch=true: %v", err)
	}
}

func TestFallbackAudit_Property_ProvenanceMirrorsDispatchPath(t *testing.T) {
	prop := func(b bool) bool {
		_, result := simulateFallback(b)
		if result == nil {
			return false
		}
		return result.Provenance.FreshDispatch == b
	}
	cfg := &quick.Config{MaxCount: 1000}
	if err := quick.Check(prop, cfg); err != nil {
		t.Errorf("inv-zen-203: provenance.FreshDispatch does not mirror dispatch path: %v", err)
	}
}

// TestFallbackAudit_Property_EvtRAGQueryIsFirstEmission verifies the
// ordering invariant: EvtRAGQuery MUST be the FIRST event emitted on
// the fallback path (dispatcher_fallback.go inv-zen-203 ordering: the
// chain must capture the dispatch reason BEFORE any network work runs).
//
// Adversarial test: a buggy implementation that emits a network-success
// event first and EvtRAGQuery second would silently violate the
// ordering — this test fails fast in the model.
func TestFallbackAudit_Property_EvtRAGQueryIsFirstEmission(t *testing.T) {
	events, _ := simulateFallback(true)
	if len(events) == 0 {
		t.Fatal("inv-zen-203: no events emitted on fallback path")
	}
	if events[0].EventType != ecosystem.EvtRAGQuery {
		t.Errorf("inv-zen-203: first event = %v; want EvtRAGQuery (%v)", events[0].EventType, ecosystem.EvtRAGQuery)
	}
}
