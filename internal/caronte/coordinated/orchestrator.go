// SPDX-License-Identifier: MIT
// internal/caronte/coordinated/orchestrator.go
//
// Coordinator interface (master C-8). Runs the spec §8.3 seven-step flow:
//
// 1. capa-firewall gate: b.Workspace.AuthorizeProjects(scope) → on
// err, emit EvtFederatedQueryDenied audit row + return the error
// (wrapped). The chokepoint invariant boundary: even denied
// access produces exactly ONE audit leaf (the denial trail).
//
// 2. oracle decision: c.Autonomy.Decision(b) → DispatchMode.
// Defense-in-depth: any value other than ModeAutonomy/ModeSurface
// is treated as ModeSurface (oracle bug-safe degradation).
//
// 3. capability-detect Pool:
// Pool != nil AND mode == ModeAutonomy → branch (4a) Autonomy.
// Pool == nil OR mode == ModeSurface → branch (4b) Surface.
// (The Pool-nil-but-mode-Autonomy combination DEGRADES to
// Surface — the surface message explicitly notes
// "WorktreePool unavailable" so the operator can investigate.)
//
// 4a. Autonomy branch: for each unique repo in b.AffectedConsumers,
// call Pool.Lease(ctx) → on err, skip + continue (per-repo
// graceful degradation). Successful leases yield DispatchedRepos.
// The lease+release pair proves the coordinated-worktree
// machinery is reachable from the L10 path; actual worker spawn
// over the leased worktrees is the future plan's job. The L10
// contract is "coordinate worktrees + audit the coordination".
//
// 4b. Surface branch: build the structured recommendation via
// buildSurfaceMessage (modes.go); DispatchedRepos remains empty.
//
// 5. audit emit (the chokepoint — invariant): emitAuditFn(ctx,
// c.Audit, federation.Event{Type: EvtCoordinatedDispatch,
// WorkspaceID b.Change.WorkspaceID, Payload: <canonical-json>,
// OccurredAt now}) → on err, RETURN err (defense-in-depth: a
// missing audit row is a CRITICAL boundary violation; better to
// fail the dispatch than to dispatch without audit).
//
// 6. ring-buffer append: recordDecision(DispatchDecision{...})
// appends to the in-memory ring (rotating on cap); FAST-ACCESS
// cache TUI reads via RecentDispatches. The persistent
// ledger is the Tessera leaf from step 5; the ring is
// cache-only.
//
// 7. assemble + return DispatchResult{Mode, DispatchedRepos,
// SurfaceMessage, AuditID}.
//
// Boundary discipline (invariant): this file imports
// - context, encoding/json, errors, fmt, sort, sync, time (stdlib)
// - github.com/cbip-solutions/hades-system/internal/audit/tessera (LeafID type
// + *Adapter field type)
// - github.com/cbip-solutions/hades-system/internal/caronte/store/federation
//
// - github.com/cbip-solutions/hades-system/internal/orchestrator (the
// ContractFixAutonomyOracle interface — NOT
// hra/merge/confirmation_policy)
// - github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool
// (Pool interface — the SOLE release/6 bridge per invariant's
// capability-detect carve-out)
// It does NOT import internal/orchestrator/hra,
// internal/orchestrator/merge, internal/orchestrator/confirmation_policy
// — the invariant AST scan asserts this for the WHOLE coordinated/
// package import set.

package coordinated

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

const DefaultRecentDispatchCap = 50

// ErrCoordinatorNoOracle indicates the Coordinator was constructed
// without an Autonomy oracle — a misconfiguration the daemon
// composition root MUST prevent. Dispatch returns this error rather
// than treating nil as ModeSurface (a nil oracle is a wiring bug, not
// a degradation case).
var ErrCoordinatorNoOracle = errors.New("coordinated: Autonomy oracle not wired")

// ErrCoordinatorNoAudit indicates the Coordinator was constructed
// without an Audit adapter. Dispatch returns this error before any side
// effect — every dispatch MUST emit an audit row per invariant (the
// single-call-site chokepoint guarantee), so a nil Audit is a wiring
// bug.
//
// Note federation.EmitAudit gracefully degrades on a nil adapter
// (returns ("", nil) for the bootstrap window). stricter
// stance — refusing nil up-front — is deliberate: by the time the L10
// Coordinator runs, release wiring MUST be live; the Coordinator
// never dispatches in the bootstrap window.
var ErrCoordinatorNoAudit = errors.New("coordinated: Audit adapter not wired")

var ErrCoordinatorNoWorkspace = errors.New("coordinated: ContractBreakage.Workspace not wired")

var emitAuditFn = federation.EmitAudit

type OrchestratorCoordinator struct {
	Autonomy AutonomyOracle
	Pool     worktreepool.Pool
	Audit    *tessera.Adapter

	recentMu  sync.RWMutex
	recent    []DispatchDecision
	recentCap int
}

var _ Coordinator = (*OrchestratorCoordinator)(nil)

func (c *OrchestratorCoordinator) Dispatch(ctx context.Context, b ContractBreakage) (DispatchResult, error) {
	if c.Autonomy == nil {
		return DispatchResult{}, ErrCoordinatorNoOracle
	}
	if c.Audit == nil {
		return DispatchResult{}, ErrCoordinatorNoAudit
	}
	if b.Workspace == nil {
		return DispatchResult{}, ErrCoordinatorNoWorkspace
	}

	scope := scopeOf(b)
	if err := b.Workspace.AuthorizeProjects(scope); err != nil {

		deniedPayload, _ := json.Marshal(deniedPayloadFor(b, err))
		_, emitErr := emitAuditFn(ctx, c.Audit, federation.Event{
			Type:        federation.EvtFederatedQueryDenied,
			WorkspaceID: b.Change.WorkspaceID,
			Payload:     deniedPayload,
			OccurredAt:  time.Now().UnixNano(),
		})
		denyErr := fmt.Errorf("coordinated: capa-firewall denied: %w", err)
		if emitErr != nil {

			return DispatchResult{}, errors.Join(denyErr,
				fmt.Errorf("coordinated: emit denial audit: %w", emitErr),
			)
		}
		return DispatchResult{}, denyErr
	}

	mode := c.Autonomy.Decision(b)
	if mode != ModeAutonomy && mode != ModeSurface {

		mode = ModeSurface
	}

	var dispatchedRepos []string
	switch {
	case mode == ModeAutonomy && c.Pool != nil:
		dispatchedRepos = c.autonomyBranch(ctx, b)
		if len(dispatchedRepos) == 0 {

			mode = ModeSurface
		}
	default:

		if mode == ModeAutonomy {

			mode = ModeSurface
		}
	}

	surface := buildSurfaceMessage(b, mode, dispatchedRepos)

	payload, err := json.Marshal(dispatchPayloadFor(b, mode, dispatchedRepos, surface))
	if err != nil {
		return DispatchResult{}, fmt.Errorf("coordinated: marshal audit payload: %w", err)
	}
	leafID, err := emitAuditFn(ctx, c.Audit, federation.Event{
		Type:        federation.EvtCoordinatedDispatch,
		WorkspaceID: b.Change.WorkspaceID,
		Payload:     payload,
		OccurredAt:  time.Now().UnixNano(),
	})
	if err != nil {

		return DispatchResult{}, fmt.Errorf("coordinated: emit audit: %w", err)
	}

	c.recordDecision(DispatchDecision{
		ChangeID:        b.Change.ChangeID,
		Mode:            mode,
		DispatchedRepos: dispatchedRepos,
		AuditID:         leafID,
		DecidedAt:       time.Now(),
	})

	return DispatchResult{
		Mode:            mode,
		DispatchedRepos: dispatchedRepos,
		SurfaceMessage:  surface,
		AuditID:         leafID,
	}, nil
}

func (c *OrchestratorCoordinator) recordDecision(d DispatchDecision) {
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	if c.recentCap <= 0 {
		c.recentCap = DefaultRecentDispatchCap
	}
	if len(c.recent) < c.recentCap {
		c.recent = append(c.recent, d)
		return
	}

	copy(c.recent, c.recent[1:])
	c.recent[len(c.recent)-1] = d
}

func (c *OrchestratorCoordinator) RecentDispatches(ctx context.Context, limit int) ([]DispatchDecision, error) {
	_ = ctx
	c.recentMu.RLock()
	defer c.recentMu.RUnlock()
	n := len(c.recent)
	if n == 0 {
		return []DispatchDecision{}, nil
	}
	take := n
	if limit > 0 && limit < take {
		take = limit
	}

	out := make([]DispatchDecision, take)
	for i := 0; i < take; i++ {
		out[i] = c.recent[n-1-i]
	}
	return out, nil
}

func (c *OrchestratorCoordinator) SetRecentCap(n int) {
	if n <= 0 {
		return
	}
	c.recentMu.Lock()
	defer c.recentMu.Unlock()
	c.recentCap = n
	if len(c.recent) > n {

		c.recent = c.recent[len(c.recent)-n:]
	}
}

func (c *OrchestratorCoordinator) autonomyBranch(ctx context.Context, b ContractBreakage) []string {
	repos := uniqueRepos(b)
	dispatched := make([]string, 0, len(repos))
	for _, repo := range repos {
		w, err := c.Pool.Lease(ctx)
		if err != nil {

			continue
		}
		dispatched = append(dispatched, repo)

		_ = c.Pool.Release(ctx, w)
	}
	return dispatched
}

func scopeOf(b ContractBreakage) []string {
	seen := map[string]struct{}{}
	if b.Change.EndpointRepo != "" {
		seen[b.Change.EndpointRepo] = struct{}{}
	}
	for _, c := range b.AffectedConsumers {
		if c.Repo != "" {
			seen[c.Repo] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func uniqueRepos(b ContractBreakage) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(b.AffectedConsumers))
	for _, c := range b.AffectedConsumers {
		if c.Repo == "" {
			continue
		}
		if _, ok := seen[c.Repo]; ok {
			continue
		}
		seen[c.Repo] = struct{}{}
		out = append(out, c.Repo)
	}
	sort.Strings(out)
	return out
}

type dispatchPayload struct {
	ChangeID        string   `json:"change_id"`
	Mode            string   `json:"mode"`
	DispatchedRepos []string `json:"dispatched_repos,omitempty"`
	SurfacePreview  string   `json:"surface_preview"`
	ConsumerCount   int      `json:"consumer_count"`
	LoreAuthor      string   `json:"lore_author,omitempty"`
	LoreCommitSHA   string   `json:"lore_commit_sha,omitempty"`
	LoreADRRefs     []string `json:"lore_adr_refs,omitempty"`
}

func dispatchPayloadFor(b ContractBreakage, mode DispatchMode, dispatched []string, surface string) dispatchPayload {
	p := dispatchPayload{
		ChangeID:        b.Change.ChangeID,
		Mode:            string(mode),
		DispatchedRepos: dispatched,
		SurfacePreview:  truncate(surface, 200),
		ConsumerCount:   len(b.AffectedConsumers),
	}
	if b.LoreAttribution != nil {
		p.LoreAuthor = b.LoreAttribution.Author
		p.LoreCommitSHA = b.LoreAttribution.CommitSHA
		p.LoreADRRefs = append(p.LoreADRRefs, b.LoreAttribution.ADRRefs...)
	}
	return p
}

type deniedPayload struct {
	ChangeID    string   `json:"change_id"`
	DeniedScope []string `json:"denied_scope"`
	Reason      string   `json:"reason"`
}

func deniedPayloadFor(b ContractBreakage, err error) deniedPayload {
	return deniedPayload{
		ChangeID:    b.Change.ChangeID,
		DeniedScope: scopeOf(b),
		Reason:      err.Error(),
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
