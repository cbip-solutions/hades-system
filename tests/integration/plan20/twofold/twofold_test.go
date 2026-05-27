// go:build integration
package twofold

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect"
	bcdetectopenapi "github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect/openapi"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract/gohttp/chi"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/link"
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/yaml"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestTwofoldEndToEnd(t *testing.T) {
	requirePython3(t)
	disableKeychain(t)
	fixturesDir := requireFixtures(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmp := t.TempDir()
	wsDBPath := filepath.Join(tmp, "workspace.db")
	wsDB, err := federation.Open(ctx, wsDBPath)
	if err != nil {
		t.Fatalf("federation.Open(%s): %v", wsDBPath, err)
	}
	t.Cleanup(func() { _ = wsDB.Close() })

	backendDir := copyDirToTemp(t, filepath.Join(fixturesDir, "backend_go_chi"))
	clientDir := copyDirToTemp(t, filepath.Join(fixturesDir, "client_python_httpx"))

	const (
		backendRepo = "backend_go_chi"
		clientRepo  = "client_python_httpx"
	)

	backendStore := openCaronteDB(t, backendDir)
	clientStore := openCaronteDB(t, clientDir)

	const workspaceID = "twofold-itest"
	members := []caronte_store.WorkspaceMember{
		{ProjectID: backendRepo, Store: backendStore},
		{ProjectID: clientRepo, Store: clientStore},
	}
	ws := registerWorkspaceAndMembers(t, ctx, wsDB, workspaceID, members, permissivePolicy{})

	// ── 5. Validate both caronte.yaml manifests via yaml.Load ────
	//
	// The manifest's target_repo entries MUST be in the workspace roster.
	// yaml.Load takes the roster slice; an unknown target yields
	// ErrUnknownTargetRepo. We assert both happy paths (schema_version=1
	// + the client manifest's services[0].base_url_env == BACKEND_URL).
	roster := ws.Projects()
	backendManifest, err := yaml.Load(filepath.Join(backendDir, "caronte.yaml"), roster)
	if err != nil {
		t.Fatalf("yaml.Load(backend manifest): %v", err)
	}
	if backendManifest.SchemaVersion != 1 {
		t.Errorf("backend manifest schema_version = %d; want 1", backendManifest.SchemaVersion)
	}
	clientManifest, err := yaml.Load(filepath.Join(clientDir, "caronte.yaml"), roster)
	if err != nil {
		t.Fatalf("yaml.Load(client manifest): %v", err)
	}
	if len(clientManifest.Services) != 1 || clientManifest.Services[0].BaseURLEnv != "BACKEND_URL" {
		t.Errorf("client manifest services[0].base_url_env = %+v; want BACKEND_URL → backend_go_chi", clientManifest.Services)
	}
	if clientManifest.Services[0].TargetRepo != backendRepo {
		t.Errorf("client manifest services[0].target_repo = %q; want %q", clientManifest.Services[0].TargetRepo, backendRepo)
	}

	chiExtractor := chi.New()
	backendEndpoints, err := chiExtractor.ExtractFromPackage(ctx, backendDir, backendRepo)
	if err != nil {
		t.Fatalf("chi.ExtractFromPackage: %v", err)
	}
	if len(backendEndpoints) == 0 {
		t.Fatalf("no api_endpoints extracted from backend; expected at least GET /users/{param}")
	}
	const normalizedUsersPath = "/users/{param}"
	var backendEP *caronte_store.APIEndpoint
	for i, ep := range backendEndpoints {
		if ep.Kind == "http" && ep.Method == "GET" && ep.PathTemplate == normalizedUsersPath {
			backendEP = &backendEndpoints[i]

			if ep.ExtractorID != "gohttp-chi-v1" {
				t.Errorf("endpoint extractor_id = %q; want gohttp-chi-v1", ep.ExtractorID)
			}
		}
	}
	if backendEP == nil {
		t.Fatalf("api_endpoints missing GET %s; got %+v", normalizedUsersPath, backendEndpoints)
	}

	if err := backendStore.InsertAPIEndpoint(ctx, *backendEP); err != nil {
		t.Fatalf("InsertAPIEndpoint(backend): %v", err)
	}

	clientCall := caronte_store.APICall{
		CallID:             "client_python_httpx:client.py:18",
		Repo:               clientRepo,
		CallerNodeID:       "client_python_httpx:client.py:main",
		TargetMethod:       "GET",
		TargetPathTemplate: normalizedUsersPath,
		BaseURLRef:         "BACKEND_URL",
		Confidence:         string(link.ConfStaticPath),
		ExtractedAt:        time.Now().Unix(),
		ExtractorID:        "python-httpx-v1",
	}
	if err := clientStore.InsertAPICall(ctx, clientCall); err != nil {
		t.Fatalf("InsertAPICall(client): %v", err)
	}

	contractLink := caronte_store.ContractLink{
		CallID:       clientCall.CallID,
		CallRepo:     clientRepo,
		EndpointID:   backendEP.EndpointID,
		EndpointRepo: backendRepo,
		Confidence:   string(link.ConfStaticPath),
		WorkspaceID:  workspaceID,
	}
	if err := ws.CrossRepoLink(ctx, contractLink); err != nil {
		t.Fatalf("Workspace.CrossRepoLink: %v", err)
	}

	persisted, err := wsDB.ListContractLinks(ctx, workspaceID, 0)
	if err != nil {
		t.Fatalf("ListContractLinks: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("ListContractLinks returned %d rows; want 1", len(persisted))
	}
	if persisted[0].Confidence != string(link.ConfStaticPath) {
		t.Errorf("contract_link confidence = %q; want %q", persisted[0].Confidence, link.ConfStaticPath)
	}
	if persisted[0].LinkMethod != string(link.LinkCaronteYAML) {
		t.Errorf("contract_link method = %q; want %q", persisted[0].LinkMethod, link.LinkCaronteYAML)
	}

	before, err := os.ReadFile(filepath.Join(fixturesDir, "breaking_diff", "before.openapi.json"))
	if err != nil {
		t.Fatalf("read before.openapi.json: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(fixturesDir, "breaking_diff", "after.openapi.json"))
	if err != nil {
		t.Fatalf("read after.openapi.json: %v", err)
	}

	detector := bcdetectopenapi.NewOpenAPIDetector(bcdetect.DefaultParams())
	if detector.DetectorID() != "oasdiff" {
		t.Errorf("openapi detector id = %q; want oasdiff (the master C-2 CHECK enum)", detector.DetectorID())
	}
	diffs, err := detector.Detect(ctx, before, after)
	if err != nil {
		t.Fatalf("openapi.Detect: %v", err)
	}
	var breakingHit bool
	for _, d := range diffs {
		if d.Severity == bcdetect.SevBreaking {
			breakingHit = true
			break
		}
	}
	if !breakingHit {
		t.Fatalf("breaking-change diff returned no SevBreaking results; before→after has {id}→{user_id} + new required query → expected at least one BREAKING; got %d total diffs", len(diffs))
	}

	auditAdapter := newTesseraAdapter(t, ctx, "twofold-itest", tmp)
	auditCounter := &federationAuditCounter{}
	changeID := "twofold-change-001"
	if err := wsDB.InsertBreakingChange(ctx, federation.BreakingChange{
		ChangeID:     changeID,
		WorkspaceID:  workspaceID,
		EndpointID:   backendEP.EndpointID,
		EndpointRepo: backendRepo,
		Kind:         "param_renamed_required",
		Detail:       `{"reason":"id renamed to user_id + new required force query param"}`,
		DetectedAt:   time.Now().Unix(),
		DetectorID:   "oasdiff",
	}); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	if err := wsDB.InsertBreakingChangeConsumer(ctx, federation.BreakingChangeConsumer{
		ChangeID: changeID,
		CallID:   clientCall.CallID,
		CallRepo: clientRepo,
	}); err != nil {
		t.Fatalf("InsertBreakingChangeConsumer: %v", err)
	}
	emitTrackedAudit(t, ctx, auditAdapter, auditCounter,
		federation.EvtBreakingChange, workspaceID,
		map[string]string{"change_id": changeID, "endpoint_id": backendEP.EndpointID},
	)

	change := caronte_store.BreakingChange{
		ChangeID:     changeID,
		WorkspaceID:  workspaceID,
		EndpointID:   backendEP.EndpointID,
		EndpointRepo: backendRepo,
		Kind:         "param_renamed_required",
		Detail:       []byte(`{"reason":"id renamed to user_id"}`),
		DetectedAt:   time.Now().Unix(),
		DetectorID:   "oasdiff",
	}
	breakage := coordinated.ContractBreakage{
		Change: change,
		AffectedConsumers: []coordinated.ConsumerRef{
			{
				Repo:   clientRepo,
				CallID: clientCall.CallID,
				NodeID: clientCall.CallerNodeID,
				File:   "client.py",
				Line:   0,
			},
		},
		Workspace:       ws,
		LoreAttribution: nil,
	}
	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: allowOracleTwofold{},
		Pool:     nil,
		Audit:    auditAdapter,
	}
	res, err := coord.Dispatch(ctx, breakage)
	if err != nil {
		t.Fatalf("Coordinator.Dispatch: %v", err)
	}
	if res.Mode != coordinated.ModeSurface {
		t.Errorf("DispatchResult.Mode = %q; want %q (Pool=nil deterministic per C-8 §8.3)", res.Mode, coordinated.ModeSurface)
	}
	if res.AuditID == "" {
		t.Errorf("DispatchResult.AuditID empty; want non-empty (every dispatch emits an audit row per C-11 / inv-zen-269)")
	}
	if res.SurfaceMessage == "" {
		t.Errorf("DispatchResult.SurfaceMessage empty; want a structured recommendation (no-stub doctrine: ModeSurface is real production code)")
	}

	auditCounter.record(res.AuditID)

	if got, want := auditCounter.count(), 2; got < want {
		t.Errorf("audit chain has %d leaves; want ≥ %d (breaking_change + coordinated_dispatch per C-11)", got, want)
	}
}

func (allowOracleTwofold) Decision(coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type allowOracleTwofold struct{}
