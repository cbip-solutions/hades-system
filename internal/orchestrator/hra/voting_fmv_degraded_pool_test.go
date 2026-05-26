package hra_test

// I-5: FMV mid-run pool-exhaustion degradation to plurality voting on
// SupportingReviewers (Q8 A pattern under Q8 B regime — spec §1 Q8 A×B
// + spec §3.2). When Pool.Lease returns ErrPoolExhausted mid-FMV, FMV
// MUST NOT abort: it picks the winner by plurality on
// SupportingReviewers over the FULL candidate set, preserves the
// partial trace verbatim for audit (input-order, not pass-desc-sorted),
// and emits EvtFMVDegradedToPlurality regardless of the plurality
// outcome (the audit signal "we degraded" is independent of whether
// the fallback itself ties).
//
// These tests reuse the helpers (fakePool, fakeApply, scriptRunner,
// captureAppender, errLeasePool, newFMV) declared in voting_fmv_test.go
// — they live in the same hra_test package.

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

func degradedPayload(t *testing.T, app *captureAppender) (eventlog.Event, map[string]any) {
	t.Helper()
	ev, ok := app.firstOf(eventlog.EvtFMVDegradedToPlurality)
	if !ok {
		t.Fatalf("no EvtFMVDegradedToPlurality emitted; got events: %+v", app.snapshot())
	}
	if ev.Payload == nil {
		t.Fatalf("EvtFMVDegradedToPlurality payload is nil")
	}
	return ev, ev.Payload
}

func asInt(t *testing.T, v any, name string) int {
	t.Helper()
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		t.Fatalf("%s = %v (%T), want int|float64", name, v, v)
		return 0
	}
}

func TestFMV_DegradesWhenPoolExhaustedMidRun_PicksHighestAgreement(t *testing.T) {
	t.Parallel()
	pool := &fakePool{cap: 1}
	apply := &fakeApply{}

	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 2},
		{ID: "Y", SupportingReviewers: 1},
		{ID: "Z", SupportingReviewers: 3},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v (degraded path must not return an error when plurality has a clear winner)", err)
	}
	if !res.Degraded {
		t.Fatal("res.Degraded = false, want true")
	}
	if res.Reason != "pool_exhausted" {
		t.Fatalf("res.Reason = %q, want %q", res.Reason, "pool_exhausted")
	}
	if res.Winner.ID != "Z" {
		t.Fatalf("winner = %q, want Z (highest agreement on FULL set)", res.Winner.ID)
	}
	if len(res.Trace) != 1 {
		t.Fatalf("trace len = %d, want 1 (only the one successfully-leased candidate)", len(res.Trace))
	}
	if res.Trace[0].Candidate.ID != "X" {
		t.Fatalf("trace[0] = %q, want X (input-order partial trace)", res.Trace[0].Candidate.ID)
	}

	_, p := degradedPayload(t, app)
	if got := p["reason"]; got != "pool_exhausted" {
		t.Errorf("reason = %v, want pool_exhausted", got)
	}
	if got := asInt(t, p["candidate_count"], "candidate_count"); got != 3 {
		t.Errorf("candidate_count = %d, want 3", got)
	}
	if got := asInt(t, p["completed_count"], "completed_count"); got != 1 {
		t.Errorf("completed_count = %d, want 1", got)
	}
	if got := p["winner_id"]; got != "Z" {
		t.Errorf("winner_id = %v, want Z", got)
	}
	if got, ok := p["tie"].(bool); ok && got {
		t.Errorf("tie = true, want false")
	}

	if _, ok := app.firstOf(eventlog.EvtVotingDecisionMade); ok {
		t.Fatal("EvtVotingDecisionMade emitted on degraded path; should NOT (degradation has its own canonical event)")
	}
}

func TestFMV_DegradesAtFirstLease(t *testing.T) {
	t.Parallel()
	pool := &exhaustedPool{}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 2},
		{ID: "Y", SupportingReviewers: 3},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if !res.Degraded {
		t.Fatal("res.Degraded = false, want true")
	}
	if res.Reason != "pool_exhausted" {
		t.Fatalf("res.Reason = %q, want pool_exhausted", res.Reason)
	}
	if res.Winner.ID != "Y" {
		t.Fatalf("winner = %q, want Y", res.Winner.ID)
	}
	if len(res.Trace) != 0 {
		t.Fatalf("trace len = %d, want 0 (no candidate evaluated)", len(res.Trace))
	}
	_, p := degradedPayload(t, app)
	if got := asInt(t, p["completed_count"], "completed_count"); got != 0 {
		t.Errorf("completed_count = %d, want 0", got)
	}
	if got := asInt(t, p["candidate_count"], "candidate_count"); got != 2 {
		t.Errorf("candidate_count = %d, want 2", got)
	}
	if got := p["winner_id"]; got != "Y" {
		t.Errorf("winner_id = %v, want Y", got)
	}
}

func TestFMV_Degraded_TieOnAgreement_ReturnsErrFMVTie(t *testing.T) {
	t.Parallel()
	pool := &fakePool{cap: 1}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 3},
		{ID: "Y", SupportingReviewers: 3},
		{ID: "Z", SupportingReviewers: 1},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if !errors.Is(err, hra.ErrFMVTie) {
		t.Fatalf("err = %v, want ErrFMVTie", err)
	}
	if !res.Degraded {
		t.Fatal("res.Degraded = false, want true (tie still degrades)")
	}
	if res.Reason != "pool_exhausted" {
		t.Fatalf("res.Reason = %q, want pool_exhausted", res.Reason)
	}
	if res.Winner.ID != "" {
		t.Fatalf("winner = %q on tie, want empty", res.Winner.ID)
	}

	_, p := degradedPayload(t, app)
	if got, ok := p["tie"].(bool); !ok || !got {
		t.Errorf("tie = %v (%T), want true", p["tie"], p["tie"])
	}
	if got := p["winner_id"]; got != "" {
		t.Errorf("winner_id = %v, want empty on tie", got)
	}
	if got := asInt(t, p["candidate_count"], "candidate_count"); got != 3 {
		t.Errorf("candidate_count = %d, want 3", got)
	}
	if got := asInt(t, p["completed_count"], "completed_count"); got != 1 {
		t.Errorf("completed_count = %d, want 1", got)
	}
}

func TestFMV_Degraded_PreservesPartialTrace(t *testing.T) {
	t.Parallel()
	pool := &fakePool{cap: 2}
	apply := &fakeApply{}

	runner := &scriptRunner{pass: []int{10, 5}, fail: []int{0, 5}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 2},
		{ID: "Z", SupportingReviewers: 3},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if !res.Degraded {
		t.Fatal("res.Degraded = false, want true")
	}
	if res.Winner.ID != "Z" {
		t.Fatalf("winner = %q, want Z", res.Winner.ID)
	}
	if len(res.Trace) != 2 {
		t.Fatalf("trace len = %d, want 2 (only X+Y evaluated; Z exhausted)", len(res.Trace))
	}

	if res.Trace[0].Candidate.ID != "X" {
		t.Errorf("trace[0] = %q, want X (input-order)", res.Trace[0].Candidate.ID)
	}
	if res.Trace[1].Candidate.ID != "Y" {
		t.Errorf("trace[1] = %q, want Y (input-order)", res.Trace[1].Candidate.ID)
	}

	if res.Trace[0].PassCount != 10 || res.Trace[0].FailCount != 0 {
		t.Errorf("trace[0] X: pass=%d fail=%d, want 10/0", res.Trace[0].PassCount, res.Trace[0].FailCount)
	}
	if res.Trace[1].PassCount != 5 || res.Trace[1].FailCount != 5 {
		t.Errorf("trace[1] Y: pass=%d fail=%d, want 5/5", res.Trace[1].PassCount, res.Trace[1].FailCount)
	}
	_, p := degradedPayload(t, app)
	if got := asInt(t, p["completed_count"], "completed_count"); got != 2 {
		t.Errorf("completed_count = %d, want 2", got)
	}
}

func TestFMV_Degraded_EmitsEventEvenOnTie(t *testing.T) {
	t.Parallel()
	pool := &fakePool{cap: 1}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10}, fail: []int{0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 5},
		{ID: "Y", SupportingReviewers: 5},
	}
	_, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if !errors.Is(err, hra.ErrFMVTie) {
		t.Fatalf("err = %v, want ErrFMVTie", err)
	}

	ev, p := degradedPayload(t, app)
	if ev.Type != eventlog.EvtFMVDegradedToPlurality {
		t.Fatalf("event type = %v, want EvtFMVDegradedToPlurality", ev.Type)
	}
	if got, ok := p["tie"].(bool); !ok || !got {
		t.Errorf("tie = %v (%T), want true", p["tie"], p["tie"])
	}
}

func TestFMV_Degraded_PoolReleasesBalanced_OnPartialRun(t *testing.T) {
	t.Parallel()
	pool := &fakePool{cap: 2}
	apply := &fakeApply{}
	runner := &scriptRunner{pass: []int{10, 7}, fail: []int{0, 3}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
		{ID: "Z", SupportingReviewers: 2},
	}
	if _, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{}); err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if got, want := pool.seq.Load(), int32(2); got != want {
		t.Fatalf("leased = %d, want %d (cap=2)", got, want)
	}
	if got, want := pool.released.Load(), int32(2); got != want {
		t.Fatalf("released = %d, want %d (no leak on partial run)", got, want)
	}
}

// TestFMV_NonExhaustedLeaseError_StillFatal a lease error that is
// NOT ErrPoolExhausted (substrate bug, e.g. pool closed) MUST still
// propagate as a wrapped error — degradation is a cost-pressure
// surface, not a generic error sink. Pinning this here makes any
// future regression that overzealously catches all lease errors fail
// loudly.
func TestFMV_NonExhaustedLeaseError_StillFatal(t *testing.T) {
	t.Parallel()
	leaseFatal := errors.New("worktreepool: pool closed")
	pool := &errLeasePool{err: leaseFatal}
	apply := &fakeApply{}
	runner := &scriptRunner{}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 2},
	}
	res, err := newFMV(pool, apply, runner, app).Run(context.Background(), candidates, hra.FMVOptions{})
	if err == nil {
		t.Fatal("err = nil, want non-nil (non-exhausted lease error must stay fatal)")
	}
	if !errors.Is(err, leaseFatal) {
		t.Fatalf("err = %v, want errors.Is(leaseFatal)=true", err)
	}
	if errors.Is(err, hra.ErrPoolExhausted) {
		t.Fatalf("err = %v, must NOT match ErrPoolExhausted (substrate bug, not cost pressure)", err)
	}
	if res.Degraded {
		t.Fatal("res.Degraded = true, want false (no degradation on substrate-bug lease error)")
	}
	if res.Winner.ID != "" {
		t.Fatalf("winner picked despite fatal lease error: %q", res.Winner.ID)
	}
	if _, ok := app.firstOf(eventlog.EvtFMVDegradedToPlurality); ok {
		t.Fatal("EvtFMVDegradedToPlurality emitted on substrate-bug lease error; should NOT")
	}
}

type exhaustedPool struct{}

func (p *exhaustedPool) Lease(_ context.Context) (hra.Lease, error) {
	return nil, hra.ErrPoolExhausted
}
