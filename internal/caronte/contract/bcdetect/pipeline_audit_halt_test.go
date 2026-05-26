//go:build cgo

package bcdetect

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

// TestPipelineFanAuditEmitFailureMidLoopHaltsLoop pins the code-review
// I-5 documented partial-state contract for the audit-failure axis.
//
// Setup two SevBreaking findings → two per-finding iterations. Iteration 0
// SUCCEEDS (row + consumers + audit committed). Iteration 1's
// row+consumers commit succeeds, BUT EmitAudit fails (injected via the
// emitAuditFn seam). Pipeline.Fan MUST:
//  1. RETURN the wrapped EmitAudit error (NEVER silently swallow).
//  2. HALT the loop — NO iteration 2+ work attempted (in this test
//     there is no iteration 2, but the assertion is shape-preserving:
//     the second finding's row+consumers ARE persisted because their tx
//     committed BEFORE emitAuditFn fired; what does NOT happen is the
//     subsequent finding being processed AT ALL).
//  3. The audit chain has a GAP for iteration 1's row — gated indirectly
//     by the audit-emit error returning before the success-path event
//     append.
//
// This is the partial-state contract: persist → audit → on audit failure,
// the row is in SQLite, the audit chain has no leaf for it, the loop
// halts. Pre-Phase-H BreakingEvent-replay coordination MUST treat this
// as a known shape (a row without a matching audit leaf is the failed
// iteration's signature; not a bug, the documented behaviour).
//
// Bite-check: remove the `return nil, fmt.Errorf(...)` and the test
// fails — the loop would continue past iteration 1 + return nil err for
// the failed audit, masking the partial-state.
func TestPipelineFanAuditEmitFailureMidLoopHaltsLoop(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	ctx, db, audit, cleanup := newPipelineHarness(t)
	defer cleanup()
	wsID := "ws-1"
	if err := db.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: wsID, OwningProject: "backend",
		PolicyLocked: false, CreatedAt: 1700000000, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	ws := newTestWorkspace(t, []string{"backend", "client-a"}, false)

	var calls atomic.Int64
	auditErr := errors.New("simulated tessera unreachable")
	prev := emitAuditFn
	emitAuditFn = func(_ context.Context, _ *tessera.Adapter, _ federation.Event) (tessera.LeafID, error) {
		n := calls.Add(1)
		if n == 2 {
			return "", auditErr
		}
		return "fake-leaf", nil
	}
	t.Cleanup(func() { emitAuditFn = prev })

	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id: "oasdiff",
				results: []DiffResult{

					{DetectorID: "oasdiff", Kind: "param_added_required", Severity: SevBreaking, Detail: []byte(`{"i":0}`)},

					{DetectorID: "oasdiff", Kind: "response_field_removed", Severity: SevBreaking, Detail: []byte(`{"i":1}`)},
					// Iteration 2: MUST NOT be attempted (halt on iter-1 audit).
					{DetectorID: "oasdiff", Kind: "would_be_third", Severity: SevBreaking, Detail: []byte(`{"i":2}`)},
				},
			},
		},
		Store: db,
		Audit: audit,
		Linker: &fakeLinker{consumers: []coordinated.ConsumerRef{
			{Repo: "client-a", CallID: "c-1", NodeID: "pkg/a.F"},
		}},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:  ws,
		Params:     DefaultParams(),
	})

	_, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/r", "abc", nil, nil)

	// Contract 1: error MUST surface wrapped (NEVER silently swallowed).
	if err == nil {
		t.Fatal("expected wrapped audit error; got nil (silent-swallow violates I-5 contract)")
	}
	if !errors.Is(err, auditErr) {
		t.Errorf("err = %v; want errors.Is(err, auditErr) (the EmitAudit sentinel MUST propagate via %%w wrap)", err)
	}

	// Contract 2: emitAuditFn called EXACTLY twice (iter 0 success +
	// iter 1 failure). Iter 2 MUST NOT be attempted — that's the halt.
	if got := calls.Load(); got != 2 {
		t.Errorf("emitAuditFn call count = %d; want 2 (loop must HALT after iter 1's audit failure, NOT proceed to iter 2)", got)
	}

	rows, listErr := db.ListBreakingChangesByEndpoint(ctx, wsID, "ep-1", "backend")
	if listErr != nil {
		t.Fatalf("ListBreakingChangesByEndpoint: %v", listErr)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 breaking_changes rows post-halt (iter 0 success + iter 1 row-persisted-but-audit-failed); got %d; rows=%+v", len(rows), rows)
	}
}
