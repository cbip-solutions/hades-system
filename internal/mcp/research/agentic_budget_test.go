package research

import (
	"context"
	"errors"
	"testing"
)

func TestAgenticBudgetBlocksOnFirstIteration(t *testing.T) {
	d := &canonDispatcher{}
	bud := &budgetGate{allowFn: func() bool { return false }}
	a := NewAgentic(AgenticOptions{
		Dispatcher: d,
		Budget:     bud,
		MaxIter:    5,
	})
	res, err := a.Run(context.Background(), "q")
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got err=%v", err)
	}
	if res.Iterations != 0 {
		t.Errorf("iterations = %d, want 0 (blocked before any dispatch)", res.Iterations)
	}
	// Dispatcher MUST NOT have been called.
	if cd, ok := any(d).(*canonDispatcher); ok && cd.calls != 0 {
		t.Errorf("Dispatcher.Dispatch called %d times despite first-iteration budget block", cd.calls)
	}
}

type estimateRecorder struct {
	estimates []float64
}

func (r *estimateRecorder) PreCall(_ context.Context, _, _ string, est float64) (bool, string, error) {
	r.estimates = append(r.estimates, est)
	return true, "", nil
}

func (r *estimateRecorder) Record(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func TestAgenticBudgetEstimateIsRealistic(t *testing.T) {
	rec := &estimateRecorder{}
	d := &canonDispatcher{
		results: []DispatchResult{
			{Findings: []SourceHit{{URL: "https://a"}}},
		},
	}
	a := NewAgentic(AgenticOptions{
		Dispatcher: d,
		Budget:     rec,
		MaxIter:    1,
	})
	if _, err := a.Run(context.Background(), "q"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.estimates) == 0 {
		t.Fatal("no estimates recorded")
	}
	for _, e := range rec.estimates {
		if e < 0.50 {
			t.Errorf("agentic per-iteration estimate %v < 0.50 (C-8 regression — under-counts realistic 5-backend + LLM cost)", e)
		}
	}
}

func TestAgenticErrBudgetExhaustedIsExported(t *testing.T) {
	if ErrBudgetExhausted == nil {
		t.Fatal("ErrBudgetExhausted sentinel must be exported")
	}
	wrapped := errors.New("agentic: " + ErrBudgetExhausted.Error())
	if errors.Is(wrapped, ErrBudgetExhausted) {
		t.Log("wrapped non-trivially still matches via errors.Is — that's intent")
	}
}
