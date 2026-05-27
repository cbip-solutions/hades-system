// contract per spec §1 Q8 (Q9 D × Q8 A interaction).
// CostGatingEngine reports cost pressure; FMV.Run MUST skip the
// lease/apply/test cycle entirely under that regime and decide by
// plurality on SupportingReviewers. The doctrine signal:
//
// - lease/apply/test is the cost-dominant operation (one subprocess
// per lease + one `make test` per candidate).
// - under budget pressure, spending 5x cost to pick the best fix
// when plurality picks a good fix is the wrong tradeoff.
// - audit emits EvtFMVDegradedToPlurality with reason="cost_pressure"
// so the morning brief surfaces every cost-pressure-induced
// degradation (spec §11.2).
//
// Schema design: I-5 introduced a single typed event family
// (FMVDegradedToPlurality) discriminated by the Reason field
// ("pool_exhausted" | "cost_pressure"); I-7 reuses that family rather
// than introducing a parallel event type. One typed event with
// structured discrimination is the max-scope design choice over two
// near-identical events.

package hra_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

// TestFMV_BudgetPressureSkipsFMV_PluralityOnly BudgetPressure is set,
// so Run short-circuits before any Lease call. Winner is the candidate
// with the highest SupportingReviewers; pool MUST stay untouched
// (zero leases, zero releases). Audit emits EvtFMVDegradedToPlurality
// with reason="cost_pressure", CompletedCount=0, WinnerID=Y, Tie=false.
func TestFMV_BudgetPressureSkipsFMV_PluralityOnly(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 3},
		{ID: "Z", SupportingReviewers: 2},
	}
	res, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), candidates,
		hra.FMVOptions{BudgetMode: hra.BudgetPressure},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !res.Degraded {
		t.Fatal("Degraded = false, want true (BudgetPressure short-circuit)")
	}
	if res.Reason != "cost_pressure" {
		t.Fatalf("Reason = %q, want cost_pressure", res.Reason)
	}
	if res.Winner.ID != "Y" {
		t.Fatalf("Winner = %s, want Y (max SupportingReviewers)", res.Winner.ID)
	}
	if got := pool.seq.Load(); got != 0 {
		t.Fatalf("pool leased = %d, want 0 (BudgetPressure must skip leases)", got)
	}
	if got := pool.released.Load(); got != 0 {
		t.Fatalf("pool released = %d, want 0", got)
	}

	ev, ok := app.firstOf(eventlog.EvtFMVDegradedToPlurality)
	if !ok {
		t.Fatalf("no EvtFMVDegradedToPlurality emitted; got events: %+v", app.snapshot())
	}
	if got := ev.Payload["reason"]; got != "cost_pressure" {
		t.Fatalf("payload reason = %v, want cost_pressure", got)
	}
	if got := ev.Payload["candidate_count"]; got != float64(3) && got != 3 {
		t.Fatalf("payload candidate_count = %v, want 3", got)
	}
	if got := ev.Payload["completed_count"]; got != float64(0) && got != 0 {
		t.Fatalf("payload completed_count = %v, want 0 (no lease ever ran)", got)
	}
	if got := ev.Payload["winner_id"]; got != "Y" {
		t.Fatalf("payload winner_id = %v, want Y", got)
	}

	if _, ok := app.firstOf(eventlog.EvtVotingDecisionMade); ok {
		t.Fatal("EvtVotingDecisionMade emitted under BudgetPressure; should NOT")
	}
	if _, ok := app.firstOf(eventlog.EvtFMVAllFailed); ok {
		t.Fatal("EvtFMVAllFailed emitted under BudgetPressure; should NOT")
	}
}

func TestFMV_BudgetPressureTie_EscalatesL3(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 2},
		{ID: "Y", SupportingReviewers: 2},
	}
	res, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), candidates,
		hra.FMVOptions{BudgetMode: hra.BudgetPressure},
	)
	if !errors.Is(err, hra.ErrFMVTie) {
		t.Fatalf("err = %v, want ErrFMVTie", err)
	}
	if !res.Degraded {
		t.Fatal("Degraded = false, want true even on tie")
	}
	if res.Reason != "cost_pressure" {
		t.Fatalf("Reason = %q, want cost_pressure", res.Reason)
	}
	ev, ok := app.firstOf(eventlog.EvtFMVDegradedToPlurality)
	if !ok {
		t.Fatal("no EvtFMVDegradedToPlurality emitted on tie; should still emit")
	}
	if got := ev.Payload["tie"]; got != true {
		t.Fatalf("payload tie = %v, want true", got)
	}
	if got := ev.Payload["winner_id"]; got != "" {
		t.Fatalf("payload winner_id = %v, want empty on tie", got)
	}
	if got := pool.seq.Load(); got != 0 {
		t.Fatalf("pool leased = %d, want 0 (BudgetPressure must skip even on tie)", got)
	}
}

func TestFMV_BudgetPressureSingleCandidate_Wins(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "Solo", SupportingReviewers: 1},
	}
	res, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), candidates,
		hra.FMVOptions{BudgetMode: hra.BudgetPressure},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if res.Winner.ID != "Solo" {
		t.Fatalf("Winner = %s, want Solo", res.Winner.ID)
	}
	if !res.Degraded || res.Reason != "cost_pressure" {
		t.Fatalf("Degraded=%v Reason=%q want true/cost_pressure", res.Degraded, res.Reason)
	}
	if got := pool.seq.Load(); got != 0 {
		t.Fatalf("pool leased = %d, want 0", got)
	}
}

func TestFMV_BudgetPressureEmpty_ErrNoVotes(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	_, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), nil,
		hra.FMVOptions{BudgetMode: hra.BudgetPressure},
	)
	if !errors.Is(err, hra.ErrNoVotes) {
		t.Fatalf("err = %v, want ErrNoVotes (empty must be checked before BudgetPressure short-circuit)", err)
	}

	if _, ok := app.firstOf(eventlog.EvtFMVDegradedToPlurality); ok {
		t.Fatal("EvtFMVDegradedToPlurality emitted on empty input; should NOT")
	}
}

// TestFMV_BudgetNormalDoesNotShortCircuit sanity — BudgetNormal (the
// zero value) MUST take the full FMV path. With a passing scriptRunner
// the algorithm reaches pickWinner and emits EvtVotingDecisionMade.
func TestFMV_BudgetNormalDoesNotShortCircuit(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{{ID: "Solo", SupportingReviewers: 1}}
	res, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), candidates,
		hra.FMVOptions{BudgetMode: hra.BudgetNormal},
	)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if res.Degraded {
		t.Fatal("Degraded = true under BudgetNormal; want false")
	}
	if res.Winner.ID != "Solo" {
		t.Fatalf("Winner = %s, want Solo", res.Winner.ID)
	}
	if got := pool.seq.Load(); got != 1 {
		t.Fatalf("pool leased = %d, want 1 (BudgetNormal must take full path)", got)
	}
	if _, ok := app.firstOf(eventlog.EvtVotingDecisionMade); !ok {
		t.Fatal("no EvtVotingDecisionMade under BudgetNormal happy path")
	}
	if _, ok := app.firstOf(eventlog.EvtFMVDegradedToPlurality); ok {
		t.Fatal("EvtFMVDegradedToPlurality emitted under BudgetNormal; should NOT")
	}
}
