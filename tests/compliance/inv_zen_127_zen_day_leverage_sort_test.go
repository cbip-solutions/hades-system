// Package compliance — inv-zen-127: zen day items are ALWAYS rendered
// in canonical leverage rank order. Defense-in-depth: a compile-time
// anchor (sentinel error referencing "inv-zen-127" + the literal
// LeverageRank 1..7 enum) plus a runtime panic in zenday.Render() when
// the slice is not pre-sorted via SortByLeverage.
//
// Spec §1 Q14 B + §7.2 inv-zen-127 wording (Plan 7 Phase F):
//
//	"zen day items sorted by canonical leverage rank (1..7).
//	 Compile-check via _ = zenDayLeverageSortSentinel() anchor;
//	 runtime via Render() re-asserts IsSorted(items, ByLeverage)
//	 BEFORE rendering — Collect always calls SortByLeverage first;
//	 the Render assertion catches contract violations from a
//	 hypothetical caller bypassing Collect."
//
// In-package coverage in internal/zenday/leverage_test.go validates the
// SortByLeverage / IsSorted contract on a hand-picked sample; this
// boundary-side compliance witness exhaustively fuzzes N=10000 random
// inputs (deterministic seed, reproducible CI) so any future refactor
// of the comparator — e.g. swapping rank ascending to descending,
// breaking the CreatedAt-desc tiebreak, or accidentally dropping
// sort.Stable for sort.Slice — gets caught at the public surface.
//
// Coverage matrix:
//
//	(a) Property fuzz N=10000 with deterministic seed (127): random
//	    item count [1..50], random rank [1..7], random CreatedAt
//	    within a 24-hour window. Pre-sort: IsSorted may be false (the
//	    scrambler intentionally feeds out-of-order input). Post-sort
//	    via SortByLeverage: IsSorted MUST be true for every trial.
//	(b) Pre-sort negative-control witness — at least one trial in the
//	    fuzz must exhibit IsSorted=false BEFORE the sort call, proving
//	    the test actually exercises the "scrambled input" path. A
//	    100% pre-sorted-input run would be a vacuous green.
//	(c) Rank range pinned 1..7 — LeverageRank(0) and LeverageRank(8+)
//	    MUST report Valid()=false. The zero value rejection is
//	    load-bearing: BriefItem.Validate() rejects rank=0, so
//	    forgetting to set the rank in a downstream emitter surfaces
//	    immediately rather than silently sorting to the front.
//	(d) Sentinel error string contains both "inv-zen-127" and the
//	    "LeverageRank 1..7" range descriptor — the verify-invariants
//	    grep gate uses these substrings as the static-anchor witness.
//	(e) Render panics on unsorted input — even a trivial 2-item
//	    swap (rank 7 then rank 1) MUST panic with a message containing
//	    "inv-zen-127". Closes the runtime defense-in-depth Layer 2
//	    contract from the boundary side.
//	(f) Stable-sort tiebreak: equal-rank items preserve relative
//	    insertion order when CreatedAt is equal (sort.Stable
//	    contract). A refactor that swaps Stable for Slice would still
//	    pass IsSorted but break the deterministic ordering documented
//	    in spec §1 Q14 B; this test pins it.
//
// Boundary (inv-zen-031): this test imports only internal/zenday +
// stdlib. internal/zenday is the load-bearing surface; the
// dispatcheradapter / store layers are not touched.
//
// Inv-zen-127 contract.
package compliance

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

var _ = zenDayLeverageSortAnchorReference()

func zenDayLeverageSortAnchorReference() error {
	return zenday.ErrZenDayLeverageSortAnchor
}

func TestInvZen127_RandomInputPostSortIsSorted(t *testing.T) {
	const trials = 10000
	rng := rand.New(rand.NewSource(127))
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sawUnsortedPreSort := false
	for trial := 0; trial < trials; trial++ {
		n := 1 + rng.Intn(50)
		items := make([]zenday.BriefItem, n)
		for i := range items {
			items[i] = zenday.BriefItem{
				Rank:      zenday.LeverageRank(1 + rng.Intn(7)),
				Message:   fmt.Sprintf("item %d", i),
				CreatedAt: base.Add(time.Duration(rng.Intn(86400)) * time.Second),
			}
		}
		if !zenday.IsSorted(items) {
			sawUnsortedPreSort = true
		}
		zenday.SortByLeverage(items)
		if !zenday.IsSorted(items) {
			t.Fatalf("inv-zen-127 violation at trial %d (n=%d): post-sort IsSorted = false", trial, n)
		}
	}
	if !sawUnsortedPreSort {
		t.Errorf("inv-zen-127: vacuous-green guard: no scrambled input observed across %d trials; scrambler broken?", trials)
	}
}

// TestInvZen127_RankRangePinned locks the canonical 1..7 enum range
// per spec §1 Q14 B. LeverageRank(0) — the Go zero-value — MUST be
// invalid; this is what makes BriefItem.Validate() catch a "forgot to
// set Rank" emitter bug. LeverageRank(8) and LeverageRank(255) close
// the upper boundary. A future refactor that widens the range to 1..10
// or accidentally accepts 0 as "default lowest priority" would silently
// re-order operator briefs; this test is the contract pin.
func TestInvZen127_RankRangePinned(t *testing.T) {
	cases := []struct {
		rank zenday.LeverageRank
		want bool
	}{
		{zenday.LeverageRank(0), false},
		{zenday.LeverageRank(1), true},
		{zenday.LeverageRank(2), true},
		{zenday.LeverageRank(3), true},
		{zenday.LeverageRank(4), true},
		{zenday.LeverageRank(5), true},
		{zenday.LeverageRank(6), true},
		{zenday.LeverageRank(7), true},
		{zenday.LeverageRank(8), false},
		{zenday.LeverageRank(255), false},
		{zenday.LeverageRank(-1), false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("rank=%d", tc.rank), func(t *testing.T) {
			if got := tc.rank.Valid(); got != tc.want {
				t.Errorf("inv-zen-127: LeverageRank(%d).Valid() = %t, want %t", tc.rank, got, tc.want)
			}
		})
	}
}

func TestInvZen127_SentinelErrorWording(t *testing.T) {
	err := zenday.LeverageSortSentinelForTest()
	if err == nil {
		t.Fatal("inv-zen-127: zenday.LeverageSortSentinelForTest() returned nil; expected sentinel anchor")
	}
	msg := err.Error()
	if !strings.Contains(msg, "inv-zen-127") {
		t.Errorf("inv-zen-127: sentinel msg = %q, want containing %q", msg, "inv-zen-127")
	}
	if !strings.Contains(msg, "LeverageRank 1..7") {
		t.Errorf("inv-zen-127: sentinel msg = %q, want containing %q", msg, "LeverageRank 1..7")
	}
	if !errors.Is(err, zenday.ErrZenDayLeverageSortAnchor) {
		t.Errorf("inv-zen-127: LeverageSortSentinelForTest() != ErrZenDayLeverageSortAnchor (anchor identity drift)")
	}
}

// TestInvZen127_RenderPanicsOnUnsortedInput is the runtime defense-in-
// depth Layer 2 boundary witness: when a hypothetical caller bypasses
// SortByLeverage and feeds Render an out-of-order slice, Render MUST
// panic with a message containing "inv-zen-127" so observability can
// route on the wire-stable substring per spec §7.3.
//
// The minimal failing input is a 2-item slice with rank 7 first and
// rank 1 second — the comparator says rank 1 should sort first, so the
// pre-condition violates IsSorted. A refactor that drops the
// pre-render IsSorted check would cause this test to fall through to
// the template execution path; the panic catch traps that drift.
func TestInvZen127_RenderPanicsOnUnsortedInput(t *testing.T) {
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	doc := zenday.BriefDoc{
		Date: base,
		Type: zenday.BriefTypeMorning,
		Items: []zenday.BriefItem{
			{Rank: zenday.RankInfoImmediate, Message: "low priority first", CreatedAt: base},
			{Rank: zenday.RankOperatorGate, Message: "operator gate second", CreatedAt: base},
		},
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("inv-zen-127 violation NOT caught: Render returned on unsorted input")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "inv-zen-127") {
			t.Errorf("inv-zen-127 panic msg = %q, want containing %q", msg, "inv-zen-127")
		}
	}()
	_ = zenday.Render(doc)
}

func TestInvZen127_StableSortTiebreakPreservesInsertionOrder(t *testing.T) {
	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	items := []zenday.BriefItem{
		{Rank: zenday.RankOperatorGate, Message: "a", CreatedAt: base},
		{Rank: zenday.RankOperatorGate, Message: "b", CreatedAt: base},
		{Rank: zenday.RankOperatorGate, Message: "c", CreatedAt: base},
	}
	zenday.SortByLeverage(items)
	want := []string{"a", "b", "c"}
	for i, msg := range want {
		if items[i].Message != msg {
			t.Errorf("inv-zen-127: stable-sort tiebreak drift at index %d: got %q, want %q",
				i, items[i].Message, msg)
		}
	}
}
