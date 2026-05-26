//go:build cgo

package bcdetect

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

// TestPipelineFanConsumerInsertFailureRollsBackPartialState pins the
// code-review I-3 fix: when a mid-iteration consumer INSERT fails (PK
// collision in this test), the WHOLE per-finding write block MUST
// rollback — the breaking_changes parent row MUST NOT persist, and the
// already-inserted consumer rows for the same finding MUST NOT persist.
//
// Pre-I-3 behaviour: Pipeline.Fan called InsertBreakingChange first +
// auto-committed; THEN InsertBreakingChangeConsumer N times each
// auto-commit. A failure at consumer K of N left the breaking_changes
// row + K-1 consumer rows persisted + audit chain potentially gap-shaped.
//
// Post-I-3: per-finding writes are wrapped in *sql.Tx; any error inside
// rolls back the WHOLE block (parent + every inserted consumer). The
// audit emission stays OUTSIDE the tx (Tessera is append-only and owns
// its own chain) — that ordering is gated by
// TestPipelineFanAuditEmitFailureMidLoopHaltsLoop (I-5).
//
// Forcing the failure: fakeLinker returns two consumer rows with
// IDENTICAL (CallID, CallRepo) — the second InsertBreakingChangeConsumer
// hits the PRIMARY KEY (change_id, call_id, call_repo) constraint and
// fails. Pre-I-3 this leaves a row + 1 consumer; post-I-3 it leaves zero
// rows in both tables.
//
// Bite-check: revert pipeline.go's I-3 tx-wrap and this test fails (the
// assertion below sees 1 breaking_changes row + 1 consumer row persisted
// instead of zero of each).
func TestPipelineFanConsumerInsertFailureRollsBackPartialState(t *testing.T) {
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
	ws := newTestWorkspace(t, []string{"backend", "client-dup"}, false)

	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id: "oasdiff",
				results: []DiffResult{
					{DetectorID: "oasdiff", Kind: "param_added_required", Severity: SevBreaking, Detail: []byte(`{}`)},
				},
			},
		},
		Store: db,
		Audit: audit,
		Linker: &fakeLinker{consumers: []coordinated.ConsumerRef{

			{Repo: "client-dup", CallID: "c-dup", NodeID: "pkg/a.F"},
			{Repo: "client-dup", CallID: "c-dup", NodeID: "pkg/b.G"},
		}},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:  ws,
		Params:     DefaultParams(),
	})

	_, err := pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID,
		"/tmp/repo", "abc", []byte("{}"), []byte("{}"))
	if err == nil {
		t.Fatal("expected error from duplicate-consumer PK violation; got nil (mid-iteration failure must surface)")
	}

	// Atomicity gate: NO breaking_changes row should be persisted (the tx
	// wrapping the per-finding writes MUST have rolled back when the
	// second consumer INSERT failed).
	rows, listErr := db.ListBreakingChangesByEndpoint(ctx, wsID, "ep-1", "backend")
	if listErr != nil {
		t.Fatalf("ListBreakingChangesByEndpoint: %v", listErr)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 breaking_changes rows post-rollback (I-3 atomicity); got %d (partial-write leaked: %+v)", len(rows), rows)
	}
}

// TestPipelineFanConsumerInsertFailureNoConsumerRowsPersist gates the
// inverse atomicity axis: NO breaking_change_consumers rows persist when
// the mid-iteration failure rolls back. The previous test asserts the
// PARENT row absence; this asserts the CHILD-table absence (the K-1
// successful inserts MUST also roll back, not just the K-th failing one).
//
// Pre-I-3 (auto-commit per row): one consumer row leaks (the first
// successful insert before the duplicate fails).
// Post-I-3 (tx wrap): zero consumer rows.
func TestPipelineFanConsumerInsertFailureNoConsumerRowsPersist(t *testing.T) {
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
	ws := newTestWorkspace(t, []string{"backend", "client-dup"}, false)

	pipeline := NewPipeline(PipelineDeps{
		Detectors: map[store.APIEndpointKind]Detector{
			store.KindHTTP: &fakeDetector{
				id: "oasdiff",
				results: []DiffResult{
					{DetectorID: "oasdiff", Kind: "param_added_required", Severity: SevBreaking, Detail: []byte(`{}`)},
				},
			},
		},
		Store: db,
		Audit: audit,
		Linker: &fakeLinker{consumers: []coordinated.ConsumerRef{
			{Repo: "client-dup", CallID: "c-dup", NodeID: "pkg/a.F"},
			{Repo: "client-dup", CallID: "c-dup", NodeID: "pkg/b.G"},
		}},
		Attributor: &fakeAttributor{att: &LoreAttribution{CommitSHA: "abc"}},
		Workspace:  ws,
		Params:     DefaultParams(),
	})

	_, _ = pipeline.Fan(ctx, store.KindHTTP, "ep-1", "backend", wsID, "/tmp/repo", "abc", nil, nil)

	var n int
	if err := db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM breaking_change_consumers`).Scan(&n); err != nil {
		t.Fatalf("COUNT consumers: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 breaking_change_consumers rows post-rollback; got %d (the K-1 successful pre-failure inserts must also roll back per I-3)", n)
	}
}
