// SPDX-License-Identifier: MIT
// cmd/zen-swarm-ctld/contract_federation_wiring.go
//
// workspace federation substrate (Phase A WorkspaceFederationDB) +
// the L10 Coordinator (Phase H OrchestratorCoordinator).
//
// This file is the daemon's composition root for Plan 20: it is the
// ONLY layer that imports BOTH internal/daemon (for the narrow
// ContractFederationForDaemon + ContractCoordinatorForDaemon
// interfaces) AND the concrete internal/caronte/store/federation +
// internal/caronte/coordinated packages. The intermediate layers see
// only the narrow seam interfaces (inv-zen-031 boundary; mirrors the
//
// Three public helpers exported to main.go (J-8):
//
//   - buildContractFederation(deps contractFederationWiringDeps)
//     (*federation.WorkspaceFederationDB,
//     *coordinated.OrchestratorCoordinator, error)
//     Opens the workspace.db via Phase A's federation.Open(ctx,
//     statePath, opts...) (variadic Option per as-built Wave-1),
//     constructs the Phase H Coordinator via plain struct-literal
//     with capability-detected pool (nil-tolerant per D9 — Plan 5
//     WorktreePool not yet daemon-wired at v0.19.0 ship; present →
//     ModeAutonomy, absent → ModeSurface). Returns both concretes;
//     main.go defers Close on fedDB + injects via the two narrow
//     adapters below.
//
//   - newFederationDaemonAdapter(*federation.WorkspaceFederationDB)
//     ContractFederationForDaemon
//     Thin adapter that translates federation.* row types into the
//     daemon-package mirror types so the daemon sees only its own
//     value types (inv-zen-031). Pure mapping; no behaviour change.
//
//   - newCoordinatorDaemonAdapter(*coordinated.OrchestratorCoordinator)
//     ContractCoordinatorForDaemon
//     Thin adapter that translates coordinated.DispatchDecision into
//     daemon.DispatchDecision (typed DispatchMode → string at the
//     boundary). Pure mapping; ring-buffer reads delegate to the
//     coordinator's RecentDispatches.
//
// PLUS defaultPolicyOracle — the production
// coordinated.AutonomyOracle impl (Phase H's seam). Master AS-BUILT
// CORRECTION #14 (MINOR-8 resolution): Phase J ships the sole
// production oracle for v0.19.0; co-located with the wiring file
// because both live in the daemon composition root. Consults
// WorkspacePolicy.PrivacyLocked() + a small blast-radius heuristic
// (≤5 affected consumers → ModeAutonomy; >5 or PrivacyLocked → ModeSurface).
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

var ErrBuildContractFederationNoAudit = errors.New("buildContractFederation: audit *tessera.Adapter is required (inv-zen-269 chokepoint)")

var ErrBuildContractFederationEmptyStatePath = errors.New("buildContractFederation: statePath is required (caller resolves via federation.WorkspaceDBPath)")

type contractFederationWiringDeps struct {
	ctx               context.Context
	audit             *tessera.Adapter
	pool              worktreepool.Pool
	doctrine          doctrineResolver
	workspaceID       string
	statePath         string
	recentDispatchCap int
	emitterFactory    func(*tessera.Adapter, string) federation.AuditEmitter
}

func buildContractFederation(deps contractFederationWiringDeps) (
	*federation.WorkspaceFederationDB,
	*coordinated.OrchestratorCoordinator,
	error,
) {
	if deps.audit == nil {

		return nil, nil, fmt.Errorf("%w", ErrBuildContractFederationNoAudit)
	}
	if deps.statePath == "" {

		return nil, nil, fmt.Errorf("%w", ErrBuildContractFederationEmptyStatePath)
	}

	workspaceID := deps.workspaceID
	if workspaceID == "" {
		workspaceID = "default"
	}

	emitterFactory := deps.emitterFactory
	if emitterFactory == nil {
		emitterFactory = federation.NewAuditEmitter
	}
	emitter := emitterFactory(deps.audit, workspaceID)

	fedDB, err := federation.Open(deps.ctx, deps.statePath, federation.WithAuditEmitter(emitter))
	if err != nil {
		return nil, nil, fmt.Errorf("buildContractFederation: federation.Open(%q): %w", deps.statePath, err)
	}

	autonomyOracle := newDefaultPolicyOracle(deps.doctrine)

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: autonomyOracle,
		Pool:     deps.pool,
		Audit:    deps.audit,
	}
	if deps.recentDispatchCap > 0 {
		coord.SetRecentCap(deps.recentDispatchCap)
	}

	return fedDB, coord, nil
}

type federationDaemonAdapter struct {
	db *federation.WorkspaceFederationDB
}

func newFederationDaemonAdapter(db *federation.WorkspaceFederationDB) *federationDaemonAdapter {
	return &federationDaemonAdapter{db: db}
}

func (a *federationDaemonAdapter) ListWorkspaces(ctx context.Context) ([]daemon.Workspace, error) {
	raw, err := a.db.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]daemon.Workspace, 0, len(raw))
	for _, w := range raw {
		out = append(out, daemon.Workspace{
			WorkspaceID:   w.WorkspaceID,
			OwningProject: w.OwningProject,
			PolicyLocked:  w.PolicyLocked,
			CreatedAt:     w.CreatedAt,
			SchemaVersion: w.SchemaVersion,
		})
	}
	return out, nil
}

func (a *federationDaemonAdapter) GetWorkspace(ctx context.Context, workspaceID string) (daemon.Workspace, error) {
	w, err := a.db.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return daemon.Workspace{}, err
	}
	return daemon.Workspace{
		WorkspaceID:   w.WorkspaceID,
		OwningProject: w.OwningProject,
		PolicyLocked:  w.PolicyLocked,
		CreatedAt:     w.CreatedAt,
		SchemaVersion: w.SchemaVersion,
	}, nil
}

func (a *federationDaemonAdapter) ListRecentBreakingChanges(ctx context.Context, workspaceID string, limit int) ([]daemon.BreakingChange, error) {
	raw, err := a.db.ListRecentBreakingChanges(ctx, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]daemon.BreakingChange, 0, len(raw))
	for _, b := range raw {
		out = append(out, daemon.BreakingChange{
			ChangeID:       b.ChangeID,
			WorkspaceID:    b.WorkspaceID,
			EndpointID:     b.EndpointID,
			EndpointRepo:   b.EndpointRepo,
			Kind:           b.Kind,
			Severity:       deriveSeverity(b.Kind),
			Detail:         b.Detail,
			DetectedAt:     b.DetectedAt,
			DetectorID:     b.DetectorID,
			LoreAuthor:     b.LoreAuthor,
			LoreCommitSHA:  b.LoreCommitSHA,
			LoreADRRefs:    b.LoreADRRefs,
			LoreSupersedes: b.LoreSupersedes,
		})
	}
	return out, nil
}

func (a *federationDaemonAdapter) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]daemon.Member, error) {
	raw, err := a.db.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]daemon.Member, 0, len(raw))
	for _, m := range raw {
		out = append(out, daemon.Member{
			WorkspaceID:  m.WorkspaceID,
			ProjectID:    m.ProjectID,
			RegisteredAt: m.RegisteredAt,
		})
	}
	return out, nil
}

func (a *federationDaemonAdapter) GetBreakingChangeWithConsumers(ctx context.Context, changeID string) (daemon.BreakingChange, []daemon.BreakingChangeConsumer, error) {
	bc, consumers, err := a.db.GetBreakingChangeWithConsumers(ctx, changeID)
	if err != nil {
		return daemon.BreakingChange{}, nil, err
	}
	bcOut := daemon.BreakingChange{
		ChangeID:       bc.ChangeID,
		WorkspaceID:    bc.WorkspaceID,
		EndpointID:     bc.EndpointID,
		EndpointRepo:   bc.EndpointRepo,
		Kind:           bc.Kind,
		Severity:       deriveSeverity(bc.Kind),
		Detail:         bc.Detail,
		DetectedAt:     bc.DetectedAt,
		DetectorID:     bc.DetectorID,
		LoreAuthor:     bc.LoreAuthor,
		LoreCommitSHA:  bc.LoreCommitSHA,
		LoreADRRefs:    bc.LoreADRRefs,
		LoreSupersedes: bc.LoreSupersedes,
	}
	cOut := make([]daemon.BreakingChangeConsumer, 0, len(consumers))
	for _, c := range consumers {
		cOut = append(cOut, daemon.BreakingChangeConsumer{
			ChangeID: c.ChangeID,
			CallID:   c.CallID,
			CallRepo: c.CallRepo,
		})
	}
	return bcOut, cOut, nil
}

func (a *federationDaemonAdapter) Close() error {
	return a.db.Close()
}

var _ daemon.ContractFederationForDaemon = (*federationDaemonAdapter)(nil)

func deriveSeverity(kind string) string {
	switch kind {
	case "removed_endpoint", "removed_field", "type_changed", "param_added_required":
		return "high"
	case "param_added_optional", "deprecation_announced", "extension_added":
		return "low"
	default:
		return "medium"
	}
}

type coordinatorSource interface {
	RecentDispatches(ctx context.Context, limit int) ([]coordinated.DispatchDecision, error)
}

type coordinatorDaemonAdapter struct {
	coord coordinatorSource
}

func newCoordinatorDaemonAdapter(c *coordinated.OrchestratorCoordinator) *coordinatorDaemonAdapter {
	return &coordinatorDaemonAdapter{coord: c}
}

var _ coordinatorSource = (*coordinated.OrchestratorCoordinator)(nil)

func (a *coordinatorDaemonAdapter) RecentDispatches(ctx context.Context, limit int) ([]daemon.DispatchDecision, error) {
	raw, err := a.coord.RecentDispatches(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]daemon.DispatchDecision, 0, len(raw))
	for _, d := range raw {
		out = append(out, daemon.DispatchDecision{
			ChangeID:        d.ChangeID,
			Mode:            dispatchModeToString(d.Mode),
			DispatchedRepos: d.DispatchedRepos,
			AuditID:         string(d.AuditID),
			DecidedAt:       d.DecidedAt.Unix(),
		})
	}
	return out, nil
}

func dispatchModeToString(m coordinated.DispatchMode) string {
	switch m {
	case coordinated.ModeAutonomy:
		return "Autonomy"
	case coordinated.ModeSurface:
		return "Surface"
	default:
		return "Unknown"
	}
}

var _ daemon.ContractCoordinatorForDaemon = (*coordinatorDaemonAdapter)(nil)

const defaultBlastRadiusAutonomyMax = 5

type defaultPolicyOracle struct {
	doctrine doctrineResolver
}

func newDefaultPolicyOracle(d doctrineResolver) *defaultPolicyOracle {
	return &defaultPolicyOracle{doctrine: d}
}

func (o *defaultPolicyOracle) Decision(b coordinated.ContractBreakage) coordinated.DispatchMode {
	if o.doctrine == nil {
		return coordinated.ModeSurface
	}
	p := o.doctrine.Policy()
	if p != nil && p.PrivacyLocked() {
		return coordinated.ModeSurface
	}
	if len(b.AffectedConsumers) > defaultBlastRadiusAutonomyMax {
		return coordinated.ModeSurface
	}
	return coordinated.ModeAutonomy
}

var _ coordinated.AutonomyOracle = (*defaultPolicyOracle)(nil)

type doctrineResolver interface {
	Policy() store.WorkspacePolicy
}

type productionDoctrineResolver struct{}

func newProductionDoctrineResolver() *productionDoctrineResolver {
	return &productionDoctrineResolver{}
}

// Policy implements doctrineResolver. Returns a non-nil WorkspacePolicy;
// callers MUST be safe to invoke PrivacyLocked() without an extra
// nil-check (the wrapping boolPolicy IS the contract).
func (productionDoctrineResolver) Policy() store.WorkspacePolicy {
	schema := active.Active()
	if schema == nil {

		return boolPolicy{locked: false}
	}
	name, ok := active.NameFor(schema)
	if !ok || name == "" {
		return boolPolicy{locked: false}
	}
	doc, err := doctrine.Get(doctrine.Name(name))
	if err != nil || doc == nil {
		return boolPolicy{locked: false}
	}
	return boolPolicy{locked: doc.PrivacyLocked()}
}

type boolPolicy struct {
	locked bool
}

func (p boolPolicy) PrivacyLocked() bool { return p.locked }

type wireContractFederationOpts struct {
	WorkspaceID string

	FederationProjectID string

	RecentDispatchCap int
}

// wireContractFederation packages the full main.go-side wiring chain
// (resolve statePath via federation.WorkspaceDBPath, lazy-load the
// federation tessera adapter via tesseraMgr.ProjectAdapter, construct
// the production doctrineResolver, call buildContractFederation, push
// the resulting concretes into the daemon via the narrow setters) so
// the composition-root flow is unit-testable + the main.go call-site
// stays a single-line invocation.
//
// Returns a closer that MUST be deferred at main.go scope; the closer
// invokes the underlying *federation.WorkspaceFederationDB.Close() (the
// tessera adapter's Close is owned by the Manager + its deferred Close,
// NOT by the federation closer).
//
// Errors surface as bootstrap-required (main.go os.Exit(1)s):
//   - nil srv → defense-in-depth refusal (would NPE in SetContract*)
//   - nil tesseraMgr → cannot resolve audit adapter (inv-zen-269)
//   - federation.WorkspaceDBPath fail → no XDG anchor in env (Phase A C-1)
//   - tesseraMgr.ProjectAdapter fail → audit subsystem misconfigured
//   - buildContractFederation fail → federation DB or coordinator wire-up failed
//
// Mirrors the buildCaronteEngine + srv.SetCaronteEngine pattern at
// main.go:520-543; the helper exists so the call-site is one line + the
// wiring chain (4 resolution steps + 2 setter calls + the closer) is
// covered by a dedicated unit test, mirror of the caronte_wiring tests.
func wireContractFederation(
	ctx context.Context,
	srv *daemon.Server,
	tesseraMgr *tessera.Manager,
	envSnapshot map[string]string,
	opts wireContractFederationOpts,
) (func() error, error) {
	if srv == nil {
		return func() error { return nil }, fmt.Errorf("wireContractFederation: srv *daemon.Server is required (defense-in-depth)")
	}
	if tesseraMgr == nil {
		return func() error { return nil }, fmt.Errorf("wireContractFederation: tesseraMgr *tessera.Manager is required (inv-zen-269 chokepoint)")
	}

	statePath, err := federation.WorkspaceDBPath(envSnapshot)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("wireContractFederation: federation.WorkspaceDBPath: %w", err)
	}

	federationProjectID := opts.FederationProjectID
	if federationProjectID == "" {
		federationProjectID = "federation"
	}
	auditAdapter, err := tesseraMgr.ProjectAdapter(ctx, federationProjectID)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("wireContractFederation: tesseraMgr.ProjectAdapter(%q): %w", federationProjectID, err)
	}

	fedDB, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:               ctx,
		audit:             auditAdapter,
		pool:              nil,
		doctrine:          newProductionDoctrineResolver(),
		workspaceID:       opts.WorkspaceID,
		statePath:         statePath,
		recentDispatchCap: opts.RecentDispatchCap,
	})
	if err != nil {
		return func() error { return nil }, fmt.Errorf("wireContractFederation: buildContractFederation: %w", err)
	}

	srv.SetContractFederation(newFederationDaemonAdapter(fedDB))
	srv.SetContractCoordinator(newCoordinatorDaemonAdapter(coord))

	closer := func() error { return fedDB.Close() }
	return closer, nil
}

func buildEnvSnapshot(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, kv := range environ {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		out[kv[:idx]] = kv[idx+1:]
	}
	return out
}
