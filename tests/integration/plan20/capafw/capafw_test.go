// go:build integration
package capafw

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestPrivacyLockedDeniesCrossRepoLink(t *testing.T) {
	disableKeychain(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmp := t.TempDir()

	wsDB, err := federation.Open(ctx, filepath.Join(tmp, "workspace.db"))
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = wsDB.Close() })
	auditAdapter := newTesseraAdapter(t, ctx, "capafw-itest", tmp)

	// ── 2. Construct a PrivacyLocked Workspace with two members ──────────
	//
	// The workspace policy is locked → the capa-firewall MUST refuse any
	// cross-project operation. Members come from in-memory caronte.db
	// stores (the K-5 test never WRITES api_calls/api_endpoints — the
	// gate fires before any persistence touches the per-repo stores).
	const (
		workspaceID = "capafw-itest"
		repoA       = "repo-a"
		repoB       = "repo-b"
	)
	storeA := openTempCaronteDB(t)
	storeB := openTempCaronteDB(t)
	members := []caronte_store.WorkspaceMember{
		{ProjectID: repoA, Store: storeA},
		{ProjectID: repoB, Store: storeB},
	}

	now := time.Now().Unix()
	if err := wsDB.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   workspaceID,
		OwningProject: repoA,
		PolicyLocked:  true,
		CreatedAt:     now,
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for _, m := range members {
		if err := wsDB.AddMember(ctx, federation.MemberRow{
			WorkspaceID:  workspaceID,
			ProjectID:    m.ProjectID,
			RegisteredAt: now,
		}); err != nil {
			t.Fatalf("AddMember(%s): %v", m.ProjectID, err)
		}
	}

	ws, err := caronte_store.NewWorkspaceWithOptions(
		workspaceID, members, lockedPolicy{},
		caronte_store.WithLinkStore(lockedLinkStorePort{fed: wsDB, workspaceID: workspaceID}),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })

	crossLink := caronte_store.ContractLink{
		CallID:       "repo-a:call:1",
		CallRepo:     repoA,
		EndpointID:   "repo-b:endpoint:1",
		EndpointRepo: repoB,
		Confidence:   "static_path",
		WorkspaceID:  workspaceID,
	}
	err = ws.CrossRepoLink(ctx, crossLink)
	if !errors.Is(err, caronte_store.ErrCrossProjectDenied) {
		t.Fatalf("CrossRepoLink under PrivacyLocked = %v; want ErrCrossProjectDenied (inv-zen-264 capa-firewall hard guard)", err)
	}

	if got := countContractLinksByWorkspace(t, ctx, wsDB, workspaceID); got != 0 {
		t.Errorf("contract_links count = %d; want 0 (denial MUST NOT insert; the gate is before persistence per workspace.go:373)", got)
	}

	change := caronte_store.BreakingChange{
		ChangeID:     "capafw-change-001",
		WorkspaceID:  workspaceID,
		EndpointID:   "repo-b:endpoint:1",
		EndpointRepo: repoB,
		Kind:         "param_renamed_required",
		Detail:       []byte(`{"why":"capafw test fixture"}`),
		DetectedAt:   time.Now().Unix(),
		DetectorID:   "oasdiff",
	}
	breakage := coordinated.ContractBreakage{
		Change: change,
		AffectedConsumers: []coordinated.ConsumerRef{
			{Repo: repoA, CallID: "repo-a:call:1", NodeID: "repo-a:node:1"},
		},
		Workspace: ws,
	}

	beforeLeafID, err := federation.EmitAudit(ctx, auditAdapter, federation.Event{
		Type:        federation.EvtWorkspacePolicySet,
		WorkspaceID: workspaceID,
		Payload:     mustJSON(t, map[string]string{"baseline": "before-dispatch"}),
		OccurredAt:  time.Now().UnixNano(),
	})
	if err != nil {
		t.Fatalf("baseline EmitAudit: %v", err)
	}

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: allowOracleCapafw{},
		Pool:     nil,
		Audit:    auditAdapter,
	}
	_, dispatchErr := coord.Dispatch(ctx, breakage)
	if !errors.Is(dispatchErr, caronte_store.ErrCrossProjectDenied) {
		t.Fatalf("Coordinator.Dispatch under PrivacyLocked = %v; want wrapped ErrCrossProjectDenied", dispatchErr)
	}

	afterLeafID, err := federation.EmitAudit(ctx, auditAdapter, federation.Event{
		Type:        federation.EvtWorkspacePolicySet,
		WorkspaceID: workspaceID,
		Payload:     mustJSON(t, map[string]string{"baseline": "after-dispatch"}),
		OccurredAt:  time.Now().UnixNano(),
	})
	if err != nil {
		t.Fatalf("post EmitAudit: %v", err)
	}

	if beforeLeafID == "" || afterLeafID == "" {
		t.Fatalf("baseline/post LeafIDs empty: before=%q after=%q (EmitAudit on a non-nil adapter must return a non-empty LeafID for a valid event)", beforeLeafID, afterLeafID)
	}
	if beforeLeafID == afterLeafID {
		t.Errorf("baseline LeafID == post LeafID (%q); want distinct (the Tessera chain advances on every successful Append)", beforeLeafID)
	}

	beforeCounter := parseLeafCounter(t, beforeLeafID)
	afterCounter := parseLeafCounter(t, afterLeafID)
	if delta := afterCounter - beforeCounter; delta != 2 {
		t.Errorf("LeafID counter delta = %d (before=%s, after=%s); want 2 (1 denial + 1 post-baseline) — the Coordinator's denial path is asserted to emit exactly ONE EvtFederatedQueryDenied leaf per Dispatch (inv-zen-269)", delta, beforeLeafID, afterLeafID)
	}
}

func parseLeafCounter(t *testing.T, id tessera.LeafID) int {
	t.Helper()
	s := string(id)
	colon := -1
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 || colon == len(s)-1 {
		t.Fatalf("LeafID %q has no ':<counter>' suffix; cannot parse counter (tessera adapter format change?)", s)
	}
	n := 0
	for i := colon + 1; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			t.Fatalf("LeafID %q counter segment is non-numeric at byte %d (%c)", s, i, c)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

type allowOracleCapafw struct{}

func (allowOracleCapafw) Decision(coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type lockedLinkStorePort struct {
	fed         *federation.WorkspaceFederationDB
	workspaceID string
}

func (p lockedLinkStorePort) Append(ctx context.Context, link caronte_store.ContractLink) error {
	ls := p.fed.LinkStore()
	return ls.Append(ctx, federation.LinkRow{
		CallID:       link.CallID,
		CallRepo:     link.CallRepo,
		EndpointID:   link.EndpointID,
		EndpointRepo: link.EndpointRepo,
		Confidence:   link.Confidence,
		WorkspaceID:  link.WorkspaceID,
		ResolvedAt:   link.ResolvedAt,
		LinkMethod:   link.LinkMethod,
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}
