package research

import (
	"context"
	"testing"
)

type dispatcherCounting struct {
	calls int
}

func (d *dispatcherCounting) Dispatch(_ context.Context, _ DispatchQuery) (DispatchResult, error) {
	d.calls++
	return DispatchResult{}, nil
}

func TestHandleAgenticDeep_BudgetBlockedAtEntry(t *testing.T) {
	d := &dispatcherCounting{}
	opts := testServerOptions()
	opts.Dispatcher = d
	opts.BudgetClient = &stubBudget{block: true}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	_, err = srv.InvokeTool(context.Background(), "agentic_deep", map[string]any{
		"initial_query": "q",
	})
	if err == nil {
		t.Fatal("expected budget block error from handler entry gate")
	}
	if d.calls != 0 {
		t.Errorf("Dispatcher.Dispatch was called %d times despite handler-entry budget block (C-20)",
			d.calls)
	}
}

func TestHandleAgenticDeep_BudgetErrorAtEntrySurfaces(t *testing.T) {
	d := &dispatcherCounting{}
	opts := testServerOptions()
	opts.Dispatcher = d
	opts.BudgetClient = errBudgetWithErr{}
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	_, err = srv.InvokeTool(context.Background(), "agentic_deep", map[string]any{
		"initial_query": "q",
	})
	if err == nil {
		t.Fatal("expected budget pre-check error to surface")
	}
	if d.calls != 0 {
		t.Errorf("Dispatcher.Dispatch was called %d times despite budget error (C-20)",
			d.calls)
	}
}

func TestHandleAgenticDeep_PreCallFires(t *testing.T) {
	rec := &recordingBudget{}
	d := &dispatcherCounting{}
	opts := testServerOptions()
	opts.Dispatcher = d
	opts.BudgetClient = rec
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	_, err = srv.InvokeTool(context.Background(), "agentic_deep", map[string]any{
		"initial_query": "q",
		"max_iter":      1,
	})
	if err != nil {
		t.Fatalf("InvokeTool: %v", err)
	}
	calls := rec.seenCalls()
	if len(calls) < 1 {
		t.Fatalf("expected at least one PreCall, got 0")
	}

	wantValue := "stage:research:agentic_deep"
	found := false
	for _, c := range calls {
		if c == wantValue {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing handler-entry PreCall %q; got calls=%v", wantValue, calls)
	}
}
