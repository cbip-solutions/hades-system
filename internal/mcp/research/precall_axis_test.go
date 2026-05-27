// precall_axis_test.go — regression tests for C-7 (CodeReview
// ): every research-MCP PreCall site MUST use one of the four
// daemon-recognised axes (project | doctrine | stage | worker_id) so
// the SEMANTIC budget gate is enforced (not silently downgraded to a
// NoOp by the daemon's BudgetCapStatus handler).
//
// The daemon's GET /v1/budget/cap_status?axis=... contract is documented
// in internal/daemon/handlers/budget_plan4.go: "axis (required):
// project | doctrine | stage | worker_id". Passing any other axis
// (e.g. "operation") makes cap_status return its zero result + the
// budget gate becomes unenforced.
package research

import (
	"context"
	"sync"
	"testing"
)

var validAxes = map[string]struct{}{
	"project":   {},
	"doctrine":  {},
	"stage":     {},
	"worker_id": {},
}

type axisRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *axisRecorder) PreCall(_ context.Context, scope, _ string, _ float64) (bool, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, scope)
	return true, "", nil
}

func (r *axisRecorder) Record(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (r *axisRecorder) Emit(_ context.Context, _ string, _ []byte) error { return nil }

func (r *axisRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

func TestPreCallValidAxis_Server(t *testing.T) {
	rec := &axisRecorder{}
	opts := testServerOptions()
	opts.BudgetClient = rec
	opts.AuditClient = rec
	srv, err := NewServer(opts)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	tools := []string{
		"web_search", "arxiv", "github_search", "code_graph",
		"ecosystem_docs", "synthesize", "agentic_deep",
	}
	for _, name := range tools {
		args := map[string]any{
			"query":         "q",
			"ecosystem":     "go",
			"initial_query": "q",
			"findings":      []any{},
			"source_id":     "x",
		}

		_, _ = srv.InvokeTool(context.Background(), name, args)
	}

	calls := rec.seen()
	if len(calls) == 0 {
		t.Fatal("no PreCall calls recorded; handlers must call BudgetClient.PreCall")
	}
	for _, axis := range calls {
		if _, ok := validAxes[axis]; !ok {
			t.Errorf("PreCall axis %q is not a daemon-recognised axis (project|doctrine|stage|worker_id) — semantic gate becomes a NoOp",
				axis)
		}
	}
}

func TestPreCallValidAxis_Dispatcher(t *testing.T) {
	rec := &axisRecorder{}
	web := &fakeBackend{hits: []SourceHit{{URL: "https://x"}}}
	arx := &fakeBackend{hits: []SourceHit{{URL: "https://y"}}}
	gh := &fakeBackend{hits: []SourceHit{{URL: "https://z"}}}
	eco := &fakeBackend{hits: []SourceHit{{URL: "https://w"}}}
	gn := &fakeGitnexus{res: CodeGraphResult{Hits: []CodeGraphHit{{Node: "n"}}}}

	d := NewDispatcher(DispatcherOptions{
		WebSearch:    web,
		Arxiv:        &arxivAdapter{arx},
		GitHub:       &ghAdapter{gh},
		Ecosystem:    &ecoAdapter{eco},
		Gitnexus:     gn,
		Cite:         passCite{},
		BudgetClient: rec,
	})
	if _, err := d.Dispatch(context.Background(), DispatchQuery{Query: "q"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	calls := rec.seen()
	if len(calls) == 0 {
		t.Fatal("no PreCall calls recorded; dispatcher must call BudgetClient.PreCall")
	}
	for _, axis := range calls {
		if _, ok := validAxes[axis]; !ok {
			t.Errorf("dispatcher PreCall axis %q is not a daemon-recognised axis", axis)
		}
	}
}

func TestPreCallValidAxis_Agentic(t *testing.T) {
	rec := &axisRecorder{}
	d := &canonDispatcher{
		results: []DispatchResult{{Findings: []SourceHit{{URL: "https://a"}}}},
	}
	a := NewAgentic(AgenticOptions{
		Dispatcher: d,
		Budget:     rec,
		MaxIter:    1,
	})
	if _, err := a.Run(context.Background(), "q"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	calls := rec.seen()
	if len(calls) == 0 {
		t.Fatal("agentic must call PreCall")
	}
	for _, axis := range calls {
		if _, ok := validAxes[axis]; !ok {
			t.Errorf("agentic PreCall axis %q is not a daemon-recognised axis", axis)
		}
	}
}
