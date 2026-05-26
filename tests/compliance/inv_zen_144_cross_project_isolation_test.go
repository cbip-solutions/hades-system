// tests/compliance/inv_zen_144_cross_project_isolation_test.go
//
// Compliance gate for inv-zen-144 (per-project Tessera tile-log isolation).
//
// Invariant text (inv-zen-144):
//
//	"Every Tessera tile-log is addressable by exactly one project_id. An
//	 Adapter constructed for project A MUST refuse to AppendLeaf or
//	 AppendSeal carrying a different project_id, returning
//	 ErrCrossProjectAccess. Cross-project isolation is enforced at the API
//	 surface, not just by directory separation, so a misrouted call cannot
//	 silently land on the wrong tile-log."
//
// internal/audit/tessera/adapter.go:
//
//   - NewProjectAdapter rejects empty projectID (ErrEmptyProjectID).
//   - AppendLeaf inspects leaf.ProjectID against a.projectID and refuses
//     with %w ErrCrossProjectAccess when they differ (line ~183).
//   - AppendSeal inspects the projectID argument against a.projectID and
//     refuses with %w ErrCrossProjectAccess when they differ (line ~273).
//
// This compliance test is the runtime gate that catches drift: if a
// refactor relaxes either check, the test fails and CI blocks the regression
// before it reaches main.
//
// Plan-file: docs/superpowers/plans/2026-05-07-plan-9-phase-K-tests.md
// lines 4113-4174 (Task K-11 Step 2). The plan-file uses speculative method
// names "Append" and "LeafFromOtherProject"; the actual API surface is
// AppendLeaf (with a Leaf struct carrying ProjectID) and AppendSeal (with
// projectID as the second argument). This test uses the real API. The
// invariant guarantee is identical: a cross-project call returns
// ErrCrossProjectAccess.
//
// Driver isolation note: this test imports
// github.com/cbip-solutions/hades-system/internal/audit/tessera which does not
// link any SQLite driver, so it coexists cleanly with the compliance
// package's existing ncruces driver registration (inv_zen_073_test.go).
//
// Refs: spec §7.2 lines 1666-1679 (Plan 9 invariant declaration); Plan 9
// Phase A Task A-3 (Adapter ProjectID guard); inv-zen-144 sentinel
// declared in internal/audit/tessera/errors.go.
package compliance

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
)

func inv144TestConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

// TestInvZen144_CrossProjectAppendLeafRefused constructs a per-project
// Adapter for project "p-a" and attempts to AppendLeaf a Leaf whose
// ProjectID is "p-b". The adapter MUST refuse with ErrCrossProjectAccess.
//
// This is the load-bearing inv-zen-144 assertion: API-surface isolation
// catches misrouted writes that would otherwise corrupt a peer project's
// tile-log.
func TestInvZen144_CrossProjectAppendLeafRefused(t *testing.T) {

	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx := context.Background()
	root := t.TempDir()

	adapterA, err := tessera.NewProjectAdapter(ctx, "p-a", root, inv144TestConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter(p-a): %v", err)
	}
	t.Cleanup(func() { _ = adapterA.Close() })

	foreignLeaf := tessera.Leaf{
		EventID:     "evt-cross-1",
		EventType:   "test.cross_project",
		PayloadHash: make([]byte, 32),
		RecordHash:  make([]byte, 32),
		ProjectID:   "p-b",
	}

	_, err = adapterA.AppendLeaf(ctx, foreignLeaf)
	if err == nil {
		t.Fatalf("inv-zen-144: AppendLeaf with foreign project_id accepted; isolation broken")
	}
	if !errors.Is(err, tessera.ErrCrossProjectAccess) {
		t.Errorf("inv-zen-144: AppendLeaf returned %v; want errors.Is(..., ErrCrossProjectAccess)", err)
	}
}

func TestInvZen144_CrossProjectAppendSealRefused(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx := context.Background()
	root := t.TempDir()

	adapterA, err := tessera.NewProjectAdapter(ctx, "p-a", root, inv144TestConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter(p-a): %v", err)
	}
	t.Cleanup(func() { _ = adapterA.Close() })

	_, err = adapterA.AppendSeal(ctx, "p-b", "2026_05", []byte(`{"seal":"test"}`))
	if err == nil {
		t.Fatalf("inv-zen-144: AppendSeal with foreign project_id accepted; isolation broken")
	}
	if !errors.Is(err, tessera.ErrCrossProjectAccess) {
		t.Errorf("inv-zen-144: AppendSeal returned %v; want errors.Is(..., ErrCrossProjectAccess)", err)
	}
}

// TestInvZen144_NewProjectAdapterRejectsEmptyProjectID is the constructor-
// level half of inv-zen-144: an Adapter MUST be addressable by a non-empty
// project_id. Empty-string project ID returns ErrEmptyProjectID; this is
// the structural pre-condition that makes the API-surface checks
// (AppendLeaf / AppendSeal) meaningful.
//
// Without this guard, two callers passing "" would both target the SAME
// "<empty>" tile-log directory and collide silently.
func TestInvZen144_NewProjectAdapterRejectsEmptyProjectID(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	_, err := tessera.NewProjectAdapter(context.Background(), "", t.TempDir(), inv144TestConfig())
	if err == nil {
		t.Fatalf("inv-zen-144: NewProjectAdapter accepted empty project_id; constructor invariant broken")
	}
	if !errors.Is(err, tessera.ErrEmptyProjectID) {
		t.Errorf("inv-zen-144: NewProjectAdapter returned %v; want errors.Is(..., ErrEmptyProjectID)", err)
	}
}

func TestInvZen144_DistinctProjectDirectoriesIsolated(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")

	ctx := context.Background()
	root := t.TempDir()

	adapterA, err := tessera.NewProjectAdapter(ctx, "p-a", root, inv144TestConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter(p-a): %v", err)
	}
	t.Cleanup(func() { _ = adapterA.Close() })

	adapterB, err := tessera.NewProjectAdapter(ctx, "p-b", root, inv144TestConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter(p-b): %v", err)
	}
	t.Cleanup(func() { _ = adapterB.Close() })

	dirA := adapterA.Dir()
	dirB := adapterB.Dir()

	if dirA == dirB {
		t.Fatalf("inv-zen-144: distinct project adapters share directory %q (path collision)", dirA)
	}
	if _, err := os.Stat(dirA); err != nil {
		t.Errorf("inv-zen-144: adapterA.Dir() %q not created: %v", dirA, err)
	}
	if _, err := os.Stat(dirB); err != nil {
		t.Errorf("inv-zen-144: adapterB.Dir() %q not created: %v", dirB, err)
	}

	if adapterA.ProjectID() != "p-a" {
		t.Errorf("inv-zen-144: adapterA.ProjectID() = %q; want %q", adapterA.ProjectID(), "p-a")
	}
	if adapterB.ProjectID() != "p-b" {
		t.Errorf("inv-zen-144: adapterB.ProjectID() = %q; want %q", adapterB.ProjectID(), "p-b")
	}
}
