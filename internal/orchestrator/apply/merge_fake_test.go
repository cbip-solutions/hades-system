// go:build test
package apply_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/apply"
)

func TestMergeEngineFake_MergeReturnsErrMergeNotImplemented(t *testing.T) {
	f := apply.NewMergeEngineFake()
	out, err := f.Merge(context.Background(), apply.MergeRequest{TargetBranch: "main"})
	if !errors.Is(err, apply.ErrMergeNotImplemented) {
		t.Fatalf("err = %v; want ErrMergeNotImplemented", err)
	}
	// Outcome zero-value contract: callers MUST NOT depend on a
	// non-zero outcome from the fake (J-5 contract).
	if out.IntegrationSHA != "" || out.TestsPassed {
		t.Fatalf("outcome was non-zero: %+v", out)
	}
}

func TestMergeEngineFake_RecordsCallsInOrder(t *testing.T) {
	f := apply.NewMergeEngineFake()
	_, _ = f.Merge(context.Background(), apply.MergeRequest{TargetBranch: "main"})
	_, _ = f.Merge(context.Background(), apply.MergeRequest{TargetBranch: "release"})
	if got, want := f.CallCount(), 2; got != want {
		t.Fatalf("CallCount = %d; want %d", got, want)
	}
	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("Calls len = %d; want 2", len(calls))
	}
	if calls[0].TargetBranch != "main" || calls[1].TargetBranch != "release" {
		t.Fatalf("Calls order: %+v", calls)
	}
}

func TestMergeEngineFake_CallsReturnsDefensiveCopy(t *testing.T) {
	f := apply.NewMergeEngineFake()
	_, _ = f.Merge(context.Background(), apply.MergeRequest{TargetBranch: "main"})
	calls := f.Calls()
	calls[0].TargetBranch = "MUTATED"
	again := f.Calls()
	if again[0].TargetBranch != "main" {
		t.Fatalf("internal state mutated via Calls() return: %+v", again[0])
	}
}

func TestMergeEngineFake_RuntimeGuardPasses_UnderGoTest(t *testing.T) {
	if !apply.IsTestRun() {
		t.Fatal("IsTestRun() returned false under `go test` — guard inverted?")
	}
	// Constructor MUST NOT panic in this path.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewMergeEngineFake panicked under go test: %v", r)
		}
	}()
	_ = apply.NewMergeEngineFake()
}

const planSixSkipMsg = "Plan 6 not yet shipped — MergeEngine fake-only mode"

// TestMergeEngineFake_CrossWorkerScenarioSkippedUntilPlanSix demonstrates
// the canonical t.Skip(planSixSkipMsg) usage + future test
// suites MUST adopt for any scenario that requires the real
// MergeEngine to make a winner-decision. first commit greps
// for the planSixSkipMsg literal and removes the guards once the real
// engine is wired.
func TestMergeEngineFake_CrossWorkerScenarioSkippedUntilPlanSix(t *testing.T) {
	t.Skip(planSixSkipMsg)

	f := apply.NewMergeEngineFake()
	req := apply.MergeRequest{
		TargetBranch: "main",
		Candidates: []apply.MergeCandidate{
			{Branch: "worker/W1", HeadSHA: "deadbeef"},
			{Branch: "worker/W2", HeadSHA: "cafebabe"},
		},
	}
	out, err := f.Merge(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan 6 path: unexpected err = %v", err)
	}
	if out.IntegrationSHA == "" {
		t.Fatal("Plan 6 path: empty IntegrationSHA")
	}
}

func TestMergeEngineFake_SkipMessageIsLoadBearing(t *testing.T) {
	const expected = "Plan 6 not yet shipped — MergeEngine fake-only mode"
	if planSixSkipMsg != expected {
		t.Fatalf("planSixSkipMsg drift: got %q, want %q", planSixSkipMsg, expected)
	}
}
