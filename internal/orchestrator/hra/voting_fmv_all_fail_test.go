package hra_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
)

func TestFMV_AllApplyErrorsCountAsAllFailed(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{
		failOn: map[string]error{
			"X": errors.New("apply: patch conflict on file A"),
			"Y": errors.New("apply: patch conflict on file B"),
		},
	}

	runner := &scriptRunner{pass: []int{999, 999}, fail: []int{0, 0}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 2},
	}
	_, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), candidates, hra.FMVOptions{},
	)
	if !errors.Is(err, hra.ErrFMVAllFailed) {
		t.Fatalf("err = %v, want ErrFMVAllFailed", err)
	}
	ev, ok := app.firstOf(eventlog.EvtFMVAllFailed)
	if !ok {
		t.Fatalf("no EvtFMVAllFailed emitted; got events: %+v", app.snapshot())
	}
	if got := ev.Payload["candidate_count"]; got != float64(2) && got != 2 {
		t.Fatalf("candidate_count = %v (%T), want 2", got, got)
	}

	if got := ev.Payload["test_failures"]; got != float64(0) && got != 0 {
		t.Fatalf("test_failures = %v (%T), want 0 (no test ran)", got, got)
	}
	if _, ok := app.firstOf(eventlog.EvtVotingDecisionMade); ok {
		t.Fatal("EvtVotingDecisionMade emitted on all-apply-error path; should NOT")
	}
	// Pool releases must be balanced even on the error path: every
	// successful Lease MUST be released regardless of apply outcome.
	// Two candidates → two leases → two releases.
	if got := pool.released.Load(); got != 2 {
		t.Fatalf("pool releases = %d, want 2 (no leak on all-apply-error)", got)
	}
}

func TestFMV_MixedApplyAndTestFailures_AllFailed(t *testing.T) {
	t.Parallel()
	pool := &fakePool{}
	apply := &fakeApply{
		failOn: map[string]error{"X": errors.New("apply: missing target")},
	}

	runner := &scriptRunner{pass: []int{0}, fail: []int{10}}
	app := &captureAppender{}

	candidates := []hra.FixProposal{
		{ID: "X", SupportingReviewers: 1},
		{ID: "Y", SupportingReviewers: 1},
	}
	_, err := newFMV(pool, apply, runner, app).Run(
		context.Background(), candidates, hra.FMVOptions{},
	)
	if !errors.Is(err, hra.ErrFMVAllFailed) {
		t.Fatalf("err = %v, want ErrFMVAllFailed", err)
	}
	ev, ok := app.firstOf(eventlog.EvtFMVAllFailed)
	if !ok {
		t.Fatalf("no EvtFMVAllFailed emitted; got events: %+v", app.snapshot())
	}

	if got := ev.Payload["test_failures"]; got != float64(10) && got != 10 {
		t.Fatalf("test_failures = %v (%T), want 10 (Y's failures only)", got, got)
	}
}
