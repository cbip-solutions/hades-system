// internal/caronte/coordinated/coverage_test.go
//
// Task 6 coverage-closing tests for branches the matrix +
// smoke tests don't exercise. Per master §13.7 the coordinated/
// package targets ≥95% (security/correctness-critical L10 logic).
// These tests close the gaps the matrix leaves open:
//
// - dispatchPayloadFor with non-nil LoreAttribution (the ADRRefs
// append branch).
// - truncate for the over-length branch (the rest is the no-op
// branch).
// - sortedConsumersOf for File tie-break + Line tie-break (the
// matrix only varies Repo).
// - consumersListPreview for the overflow ">N" branch.
// - recommendForOperator's default branch (autonomy with no
// dispatched repos — the degraded coordinator path between
// ModeAutonomy + empty dispatched, which orchestrator.go
// down-converts to ModeSurface but the builder itself can render).
// - autonomyBranch with a Lease error (per-repo degradation).
// - uniqueRepos with empty Repo entries (skipped per
// defense-in-depth).
// - Dispatch with a Pool that returns lease errors for EVERY repo
// (degrades the WHOLE dispatch to ModeSurface).

package coordinated

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestDispatchPayloadForLoreBranch(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-lore", EndpointRepo: "api", Kind: "removed_field"},
		LoreAttribution: &LoreAttribution{
			Author:    "alice@example.com",
			CommitSHA: "deadbeef",
			ADRRefs:   []string{"ADR-0001", "ADR-0002"},
		},
	}
	p := dispatchPayloadFor(b, ModeAutonomy, []string{"r1"}, "preview text")
	if p.LoreAuthor != "alice@example.com" {
		t.Errorf("LoreAuthor: want %q, got %q", "alice@example.com", p.LoreAuthor)
	}
	if p.LoreCommitSHA != "deadbeef" {
		t.Errorf("LoreCommitSHA: want %q, got %q", "deadbeef", p.LoreCommitSHA)
	}
	if len(p.LoreADRRefs) != 2 {
		t.Errorf("LoreADRRefs: want 2 refs, got %d", len(p.LoreADRRefs))
	}

	if _, err := json.Marshal(p); err != nil {
		t.Errorf("json.Marshal: %v", err)
	}
}

func TestTruncateOverlong(t *testing.T) {
	got := truncate("abcdefghij", 3)
	if got != "abc…" {
		t.Errorf("truncate(len 10, n 3): want %q, got %q", "abc…", got)
	}

	got = truncate("abc", 3)
	if got != "abc" {
		t.Errorf("truncate(len 3, n 3): want %q, got %q", "abc", got)
	}
}

func TestSortedConsumersFileLineTiebreak(t *testing.T) {
	in := []ConsumerRef{
		{Repo: "r", File: "z.go", Line: 5},
		{Repo: "r", File: "a.go", Line: 10},
		{Repo: "r", File: "a.go", Line: 2},
	}
	out := sortedConsumersOf(in)

	want := []ConsumerRef{
		{Repo: "r", File: "a.go", Line: 2},
		{Repo: "r", File: "a.go", Line: 10},
		{Repo: "r", File: "z.go", Line: 5},
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("sortedConsumersOf[%d]: want %+v, got %+v", i, want[i], out[i])
		}
	}
}

func TestConsumersListPreviewOverflow(t *testing.T) {
	in := []ConsumerRef{
		{Repo: "r1", File: "f1", Line: 1},
		{Repo: "r2", File: "f2", Line: 2},
		{Repo: "r3", File: "f3", Line: 3},
		{Repo: "r4", File: "f4", Line: 4},
		{Repo: "r5", File: "f5", Line: 5},
		{Repo: "r6", File: "f6", Line: 6},
		{Repo: "r7", File: "f7", Line: 7},
	}
	got := consumersListPreview(in, 3)
	if !strings.Contains(got, "+4 more") {
		t.Errorf("consumersListPreview overflow: want substring %q, got %q", "+4 more", got)
	}

	if consumersListPreview(nil, 5) != "" {
		t.Errorf("consumersListPreview(nil): want empty, got %q", consumersListPreview(nil, 5))
	}
}

func TestRecommendForOperatorDefaultBranch(t *testing.T) {
	b := ContractBreakage{
		Change:            store.BreakingChange{ChangeID: "ch", EndpointRepo: "api", Kind: "k"},
		AffectedConsumers: []ConsumerRef{{Repo: "r", File: "f", Line: 1}},
	}

	got := recommendForOperator(b, ModeAutonomy, nil)
	if !strings.Contains(got, "review the breakage manually") {
		t.Errorf("default-arm recommendForOperator: want substring %q, got %q",
			"review the breakage manually", got)
	}
}

func TestAutonomyBranchAllLeasesFail(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	failingPool := &stubPool{leaseErr: errors.New("pool exhausted")}
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeAutonomy),
		Pool:     failingPool,
		Audit:    audit.Adapter(),
	}
	ws := stubWorkspace(t, false, "owning", "client-a")
	b := ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-lease-fail", EndpointRepo: "owning",
			WorkspaceID: "ws-test", Kind: "removed_field"},
		Workspace: ws,
		AffectedConsumers: []ConsumerRef{
			{Repo: "owning", File: "a.go", Line: 1},
			{Repo: "client-a", File: "b.go", Line: 1},
		},
	}
	got, err := coord.Dispatch(context.Background(), b)
	if err != nil {
		t.Fatalf("Dispatch (all leases fail): unexpected error %v", err)
	}
	if got.Mode != ModeSurface {
		t.Errorf("Mode: want %q (degraded), got %q", ModeSurface, got.Mode)
	}
	if len(got.DispatchedRepos) != 0 {
		t.Errorf("DispatchedRepos: want empty (all leases failed), got %v", got.DispatchedRepos)
	}

	if audit.Count() != 1 {
		t.Errorf("audit count: want 1, got %d", audit.Count())
	}
}

func TestUniqueReposSkipsEmpty(t *testing.T) {
	b := ContractBreakage{
		AffectedConsumers: []ConsumerRef{
			{Repo: "r1"},
			{Repo: ""},
			{Repo: "r1"},
			{Repo: "r2"},
			{Repo: "r2"},
		},
	}
	out := uniqueRepos(b)
	if len(out) != 2 {
		t.Errorf("uniqueRepos: want 2 (empties + duplicates skipped), got %v", out)
	}

	if out[0] != "r1" || out[1] != "r2" {
		t.Errorf("uniqueRepos: want [r1, r2] sorted, got %v", out)
	}
}

func TestBuildSurfaceMessageAutonomyZeroConsumers(t *testing.T) {
	b := ContractBreakage{
		Change:            store.BreakingChange{ChangeID: "ch-zero", EndpointRepo: "api", Kind: "removed_field"},
		AffectedConsumers: nil,
	}
	got := buildSurfaceMessage(b, ModeAutonomy, []string{"r1"})
	if !strings.Contains(got, "no consumers affected") {
		t.Errorf("buildSurfaceMessage(Autonomy, 0 consumers): want substring %q, got %q",
			"no consumers affected", got)
	}

	if !strings.Contains(got, "L10 AUTONOMY") {
		t.Errorf("buildSurfaceMessage(Autonomy, 0 consumers): want substring %q, got %q",
			"L10 AUTONOMY", got)
	}
}

func TestDispatchUnknownDispatchModeFallback(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(DispatchMode("bogus-mode-the-oracle-invented")),
		Pool:     nil,
		Audit:    audit.Adapter(),
	}
	ws := stubWorkspace(t, false, "owning")
	b := ContractBreakage{
		Change:    store.BreakingChange{ChangeID: "ch-bogus-mode", WorkspaceID: "ws-a", EndpointRepo: "owning", Kind: "removed_field"},
		Workspace: ws,
		AffectedConsumers: []ConsumerRef{
			{Repo: "owning", File: "a.go", Line: 1},
		},
	}
	got, err := coord.Dispatch(context.Background(), b)
	if err != nil {
		t.Fatalf("Dispatch (bogus mode): unexpected error %v", err)
	}
	if got.Mode != ModeSurface {
		t.Errorf("Dispatch (bogus mode): want ModeSurface (defense-in-depth fallback), got %q", got.Mode)
	}

	if got, want := audit.Count(), 1; got != want {
		t.Errorf("audit appends: want %d, got %d", want, got)
	}
}

func TestScopeOfSkipsEmpty(t *testing.T) {
	b := ContractBreakage{
		Change: store.BreakingChange{EndpointRepo: ""},
		AffectedConsumers: []ConsumerRef{
			{Repo: ""},
			{Repo: "r1"},
		},
	}
	out := scopeOf(b)
	if len(out) != 1 || out[0] != "r1" {
		t.Errorf("scopeOf with empty entries: want [r1], got %v", out)
	}
}
