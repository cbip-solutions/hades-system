// tests/adversarial/plan20_dynamic_baseurl_unresolved_test.go
//
// → unresolved row + surface; NEVER false-link.
// Build tag: adversarial (per the tests/adversarial/ precedent).
// Runs under `make test-adversarial`.
//
// Scenario: a hostile / careless
// extractor produces an api_call whose `base_url_ref` points at a
// runtime-resolved value (e.g., `UNKNOWN_BACKEND` env-name, or a literal
// URL that no caronte.yaml service entry covers). The linker MUST:
//
// - never silently drop the row (PolicySurface is doctrine-default
// per spec §6);
// - never false-link by fabricating a low-confidence contract_links
// row (the schema CHECK on (confidence, link_method) refuses the
// forged shape; the Go-layer guard refuses BEFORE the CHECK);
// - persist an UnresolvedRow that surfaces to the operator with the
// original base_url_ref preserved (so the operator can repair the
// caronte.yaml manifest).
//
// This is the invariant normative statement under hostile input.
//
// Adversarial pattern (post code-review I-2):
// - Drive the invariant contract through the PUBLIC linker entry
// point (link.NewLinker + Linker.LinkProject) so a regression that
// ROUTES around UnresolvedStore (e.g., a future tier-4 path that
// forgot to call the surfacer) FAILS this test. The previous shape
// called UnresolvedStore.Insert directly, which bypassed the linker
// and would have left such a regression undetected.
// - The per-call source store is a thin in-file fake (mirrors
// internal/caronte/contract/link/link_test.go::fakeProjectStore;
// re-declared package-local for the adversarial-test self-contained
// posture) returning 4 hostile api_calls each with a base_url_ref
// the manifest does NOT cover.
// - The UnresolvedStorePort fake adapts onto a REAL
// federation.WorkspaceFederationDB so the test verifies BOTH (a) the
// linker invoked the port for every hostile call (linker side), AND
// (b) the federation store persists the rows queryable via
// ListUnresolvedByWorkspace (persistent side). Double-gating is the
// "no false-link AND no silent-drop AND the persistent surface
// reaches the operator" full chain.
//
// Bite-check: short-circuit the linker's Unresolved Surface invocation
// (replace `l.unresolved.Surface(ctx, call, policy, …)` in link.go::linkOne
// step 4 with `return nil`) → this test fails (the UnresolvedStore.Insert
// is never called, the fed-store ListUnresolvedByWorkspace returns 0 rows;
// the per-case error is "0 unresolved rows persisted; want 4"). Restoring
// the Surface call turns the gate green again.

// go:build adversarial
package adversarial

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/link"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type l8FakeProjectStore struct {
	calls     []store.APICall
	endpoints []store.APIEndpoint
	nodes     map[string]store.Node
}

func (f *l8FakeProjectStore) ListAPICallsByRepo(_ context.Context, repo string) ([]store.APICall, error) {
	out := []store.APICall{}
	for _, c := range f.calls {
		if c.Repo == repo {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *l8FakeProjectStore) ListAPIEndpointsByRepo(_ context.Context, repo string) ([]store.APIEndpoint, error) {
	out := []store.APIEndpoint{}
	for _, e := range f.endpoints {
		if e.Repo == repo {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *l8FakeProjectStore) GetAPICall(_ context.Context, callID string) (store.APICall, error) {
	for _, c := range f.calls {
		if c.CallID == callID {
			return c, nil
		}
	}
	return store.APICall{}, errors.New("no such call")
}

func (f *l8FakeProjectStore) GetNode(_ context.Context, nodeID string) (store.Node, error) {
	if n, ok := f.nodes[nodeID]; ok {
		return n, nil
	}
	return store.Node{}, errors.New("no such node")
}

// l8FakeWorkspace observes every link.WorkspaceLinkPort.CrossRepoLink call.
// The invariant contract requires the linker NEVER call CrossRepoLink for
// an unresolved tier; len(persisted) MUST be 0 post-LinkProject.
type l8FakeWorkspace struct {
	persisted []store.ContractLink
}

func (w *l8FakeWorkspace) CrossRepoLink(_ context.Context, link store.ContractLink) error {
	w.persisted = append(w.persisted, link)
	return nil
}

type l8FakeFedRead struct{}

func (l8FakeFedRead) ListContractLinks(_ context.Context, _ string, _ int) ([]federation.LinkRow, error) {
	return nil, nil
}

type l8FakeDeps struct {
	stores map[string]*l8FakeProjectStore
}

func (d *l8FakeDeps) OpenProjectStore(_ context.Context, repo string) (link.ProjectStorePort, error) {
	s, ok := d.stores[repo]
	if !ok {
		return nil, errors.New("no fake store for repo " + repo)
	}
	return s, nil
}

func (d *l8FakeDeps) FederationDB() link.FederationReadPort { return l8FakeFedRead{} }

type l8FedUnresolvedAdapter struct {
	inner    federation.UnresolvedStore
	observed []federation.UnresolvedRow
}

func (a *l8FedUnresolvedAdapter) Insert(ctx context.Context, row federation.UnresolvedRow) error {
	if err := a.inner.Insert(ctx, row); err != nil {
		return err
	}
	a.observed = append(a.observed, row)
	return nil
}

func TestPlan20AdversarialDynamicBaseURLUnresolved(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "workspace.db")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wsdb, err := federation.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	defer wsdb.Close()

	const (
		wsID   = "ws-adv-baseurl"
		client = "client-svc"
	)
	if err := wsdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   wsID,
		OwningProject: client,
		PolicyLocked:  false,
		CreatedAt:     time.Now().Unix(),
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := wsdb.AddMember(ctx, federation.MemberRow{
		WorkspaceID:  wsID,
		ProjectID:    client,
		RegisteredAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	// 4 hostile base_url_ref shapes the manifest does NOT cover. The
	// manifest's ONLY Service entry binds env "KNOWN_BACKEND_URL" — every
	// hostile ref below MUST land in tier-4 (unresolved).
	hostileRefs := []string{
		"UNKNOWN_BACKEND_URL",
		"https://undeclared.api/",
		"${RUNTIME_ENV_NAME}",
		"http://hostile.example/v",
	}

	calls := make([]store.APICall, 0, len(hostileRefs))
	for i, ref := range hostileRefs {
		calls = append(calls, store.APICall{
			CallID:             "hostile-call-" + itoaAdv(i),
			Repo:               client,
			CallerNodeID:       "n" + itoaAdv(i),
			TargetMethod:       "GET",
			TargetPathTemplate: "/x",
			BaseURLRef:         ref,
			Confidence:         "static_path",
			ExtractedAt:        time.Now().Unix(),
			ExtractorID:        "gohttp",
		})
	}
	srcStore := &l8FakeProjectStore{calls: calls}

	manifests := map[string]*yaml.Manifest{
		client: {
			SchemaVersion: 1,
			Services: []yaml.Service{
				{BaseURLEnv: "KNOWN_BACKEND_URL", TargetRepo: "auth-svc"},
			},
			UnresolvedPolicy: yaml.PolicySurface,
		},
	}

	usAdapter := &l8FedUnresolvedAdapter{inner: wsdb.UnresolvedStore()}
	ws := &l8FakeWorkspace{}
	auditEmitter := federation.NewAuditEmitter(nil, wsID)
	deps := &l8FakeDeps{stores: map[string]*l8FakeProjectStore{client: srcStore}}

	linker := link.NewLinker(ws, usAdapter, auditEmitter, manifests, nil, wsID, deps)

	res, err := linker.LinkProject(ctx, client, client)
	if err != nil {
		t.Fatalf("link.LinkProject: %v", err)
	}

	if len(usAdapter.observed) != len(hostileRefs) {
		t.Errorf("plan20 adv L-8: linker invoked UnresolvedStore.Insert %d times; want %d (every hostile ref MUST transit the surfacer)",
			len(usAdapter.observed), len(hostileRefs))
	}

	if res.UnresolvedRows != len(hostileRefs) {
		t.Errorf("plan20 adv L-8: LinkProject.Result.UnresolvedRows = %d; want %d", res.UnresolvedRows, len(hostileRefs))
	}

	// Assertion 3 (linker-side): NO contract_links persisted (the
	// invariant no-false-link guarantee). The linker MUST NOT route a
	// hostile call to the workspace CrossRepoLink port.
	if len(ws.persisted) != 0 {
		t.Errorf("plan20 adv L-8: linker called workspace.CrossRepoLink %d times; want 0 (false-link guarantee violated)", len(ws.persisted))
	}
	if res.LinksPersisted != 0 {
		t.Errorf("plan20 adv L-8: LinkProject.Result.LinksPersisted = %d; want 0", res.LinksPersisted)
	}

	rows, err := wsdb.ListUnresolvedByWorkspace(ctx, wsID, 100)
	if err != nil {
		t.Fatalf("ListUnresolvedByWorkspace: %v", err)
	}
	if len(rows) != len(hostileRefs) {
		t.Errorf("plan20 adv L-8: %d unresolved rows persisted (federation surface); want %d (every hostile ref must surface durably)",
			len(rows), len(hostileRefs))
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.BaseURLRef] = true
		if r.WorkspaceID != wsID {
			t.Errorf("plan20 adv L-8: row.WorkspaceID = %q want %q (workspace scoping violated)", r.WorkspaceID, wsID)
		}
		if r.Reason == "" {
			t.Errorf("plan20 adv L-8: row for ref=%q has empty Reason (operator-actionable hint missing)", r.BaseURLRef)
		}
	}
	for _, ref := range hostileRefs {
		if !got[ref] {
			t.Errorf("plan20 adv L-8: hostile ref %q absent from UnresolvedRow set (silently dropped)", ref)
		}
	}

	// Assertion 5 (persistent-side, the invariant anti-false-link
	// guarantee at the federation layer): NO contract_links rows exist
	// for these hostile refs. The schema CHECK on contract_links.
	// confidence + link_method enforces the closed enum, so a fabricated
	// `confidence='fuzzy_path'` row WITHOUT a true fuzzy match would
	// either (a) be refused at the Go layer (the linker's
	// checkTierConsistency gate), or (b) be inserted as a real row
	// (which we do NOT want — the absence is the assertion).
	links, err := wsdb.ListContractLinks(ctx, wsID, 100)
	if err != nil {
		t.Fatalf("ListContractLinks: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("plan20 adv L-8: %d contract_links rows present after unresolved-only path; want 0 (false-link guarantee violated at federation layer)", len(links))
	}
}

func itoaAdv(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	neg := n < 0
	if neg {
		n = -n
	}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
