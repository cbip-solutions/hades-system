// SPDX-License-Identifier: MIT
// Package handlers — orchestrator.go.
//
// Replaces the thin pin/unpin/status surface (which used
// package-level globals) with a six-endpoint real surface backed by the
// +C+D+E components:
//
// - GET /v1/orchestrator/status — per-tier circuit breaker state +
// active pins summary + 30d cost summary.
// - POST /v1/orchestrator/pin — operator pin (calls PinOverrides.Set).
// - POST /v1/orchestrator/unpin — operator unpin (calls PinOverrides.Unset
// or UnpinAll).
// - GET /v1/orchestrator/pins — list every active pin (PinOverrides.ListAll).
// - POST /v1/orchestrator/probe — trigger AttemptRecovery on each
// non-Closed tier and return the post-probe state.
// - GET /v1/orchestrator/history — current state per-tier (post-rescope
// placeholder; CircuitBreaker doesn't track transition history yet —
// see K-3 self-review concern).
//
// Boundary: handlers consume the Server pointer as `any` and
// type-assert against locally-defined interfaces (mirrors handlers/bypass.go
// pattern) so this package never imports the daemon back. The orchestrator
// package types (CircuitBreaker, PinOverrides, CostCounters) are imported
// directly because that import edge does not introduce a cycle.
//
// Tier names: handlers consume Server.Tiers() (the SAME providers.TierBackend
// slice the dispatcher iterates) so adding a new tier in buildOrchestrator
// automatically widens this surface — no separate registry to drift.
//
// globals + resetOrchestratorStateForTest are GONE. Existing route
// /v1/orchestrator/switch is dropped in favour of RESTful /pin + /unpin.
package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

// orchestratorAccessor narrows the daemon to the K-3-handler surface.
// *daemon.Server satisfies this structurally because all method names +
// signatures match. Defined as an interface here to avoid the
// daemon→handlers→daemon import cycle.
//
// Each accessor returns nil (or the zero value) until main.go has
// finished wiring; handlers MUST guard for nil and degrade to a
// shape-correct empty response rather than 500.
type orchestratorAccessor interface {
	CircuitBreaker() *orchestrator.CircuitBreaker
	PinOverrides() *orchestrator.PinOverrides
	CostCounters() *orchestrator.CostCounters
	Tiers() []providers.TierBackend
}

func resolveOrchestratorAccessor(s any) orchestratorAccessor {
	if acc, ok := s.(orchestratorAccessor); ok {
		return acc
	}
	return nil
}

type pinSummary struct {
	ID        int64      `json:"id"`
	Scope     string     `json:"scope"`
	ScopeID   string     `json:"scope_id,omitempty"`
	Tier      string     `json:"tier"`
	Provider  string     `json:"provider,omitempty"`
	SetAt     time.Time  `json:"set_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

type tierStateRow struct {
	Provider string `json:"provider"`
	Tier     string `json:"tier"`
	State    string `json:"state"`
}

type costRow struct {
	Tier   string  `json:"tier"`
	Total  float64 `json:"total_usd_30d"`
	Window string  `json:"window"`
}

type orchestratorStatusResp struct {
	Tiers []tierStateRow `json:"tiers"`
	Pins  []pinSummary   `json:"pins"`
	Costs []costRow      `json:"costs"`
}

type pinReq struct {
	Scope    string `json:"scope"`
	Project  string `json:"project,omitempty"`
	Session  string `json:"session,omitempty"`
	Tier     string `json:"tier"`
	Provider string `json:"provider,omitempty"`
	TTL      string `json:"ttl,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type unpinReq struct {
	Scope   string `json:"scope,omitempty"`
	Project string `json:"project,omitempty"`
	Session string `json:"session,omitempty"`
	All     bool   `json:"all,omitempty"`
}

// scopeIDFor returns the canonical (scope, scopeID) pair from a wire-shape
// (scope, project, session) triple. Validation order: explicit scope must
// match the supplied id; for "session"/"project" the matching id MUST be
// non-empty; for "global" both ids MUST be empty.
//
// Returns (scope, scopeID, ok). On !ok, the caller MUST emit 400.
func scopeIDFor(scope, project, session string) (string, string, bool) {
	switch scope {
	case "global":
		if project != "" || session != "" {
			return "", "", false
		}
		return "global", "", true
	case "project":
		if project == "" {
			return "", "", false
		}
		return "project", project, true
	case "session":
		if session == "" {
			return "", "", false
		}
		return "session", session, true
	}
	return "", "", false
}

func OrchestratorStatus(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := resolveOrchestratorAccessor(s)
		out := orchestratorStatusResp{
			Tiers: []tierStateRow{},
			Pins:  []pinSummary{},
			Costs: []costRow{},
		}
		if acc != nil {
			out.Tiers = collectTierStates(acc.CircuitBreaker(), acc.Tiers())
			out.Pins = collectPins(acc.PinOverrides())
			out.Costs = collect30dCosts(acc.CostCounters(), acc.Tiers())
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func collectTierStates(cb *orchestrator.CircuitBreaker, tiers []providers.TierBackend) []tierStateRow {
	out := make([]tierStateRow, 0, len(tiers))
	for _, t := range tiers {
		state := orchestrator.StateClosed.String()
		if cb != nil {

			state = cb.State(t.Name()).String()
		}
		out = append(out, tierStateRow{
			Provider: t.Name(),
			Tier:     t.Tier().String(),
			State:    state,
		})
	}
	return out
}

func collectPins(p *orchestrator.PinOverrides) []pinSummary {
	if p == nil {
		return []pinSummary{}
	}
	rows, err := p.ListAll()
	if err != nil {
		return []pinSummary{}
	}
	out := make([]pinSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, pinSummary{
			ID:        r.ID,
			Scope:     r.Scope,
			ScopeID:   r.ScopeID,
			Tier:      r.Tier,
			Provider:  r.Provider,
			SetAt:     r.SetAt,
			ExpiresAt: r.ExpiresAt,
			Reason:    r.Reason,
		})
	}
	return out
}

func collect30dCosts(counters *orchestrator.CostCounters, tiers []providers.TierBackend) []costRow {
	out := make([]costRow, 0, len(tiers))
	if counters == nil {
		return out
	}

	totals := make(map[string]float64, len(tiers))
	for _, k := range counters.AllKeys() {
		totals[k.Tier] += counters.ProjectProfileTierTotal(k.Project, k.Profile, k.Tier, 30*24*time.Hour)
	}
	for _, t := range tiers {
		name := t.Tier().String()
		out = append(out, costRow{
			Tier:   name,
			Total:  totals[name],
			Window: "30d",
		})
	}
	return out
}

func OrchestratorPin(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body pinReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}

		if body.Tier == "" {
			http.Error(w, "tier is required", http.StatusBadRequest)
			return
		}

		if _, err := providers.ParseTier(body.Tier); err != nil {
			http.Error(w, "invalid tier: "+err.Error(), http.StatusBadRequest)
			return
		}
		scope, scopeID, ok := scopeIDFor(body.Scope, body.Project, body.Session)
		if !ok {
			http.Error(w, "invalid scope/project/session combination", http.StatusBadRequest)
			return
		}
		var ttl time.Duration
		if body.TTL != "" {
			d, err := time.ParseDuration(body.TTL)
			if err != nil {
				http.Error(w, "bad ttl: "+err.Error(), http.StatusBadRequest)
				return
			}
			ttl = d
		}
		acc := resolveOrchestratorAccessor(s)
		if acc == nil || acc.PinOverrides() == nil {
			http.Error(w, "pin overrides not configured", http.StatusServiceUnavailable)
			return
		}
		if err := acc.PinOverrides().Set(scope, scopeID, body.Tier, body.Provider, ttl, body.Reason); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func OrchestratorUnpin(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body unpinReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		acc := resolveOrchestratorAccessor(s)
		if acc == nil || acc.PinOverrides() == nil {
			http.Error(w, "pin overrides not configured", http.StatusServiceUnavailable)
			return
		}

		hasScope := body.Scope != ""
		if body.All && hasScope {
			http.Error(w, "all and scope are mutually exclusive", http.StatusBadRequest)
			return
		}
		if body.All {
			if _, err := acc.PinOverrides().UnpinAll(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		scope, scopeID, ok := scopeIDFor(body.Scope, body.Project, body.Session)
		if !ok {
			http.Error(w, "invalid scope/project/session combination", http.StatusBadRequest)
			return
		}
		if err := acc.PinOverrides().Unset(scope, scopeID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func OrchestratorPins(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := resolveOrchestratorAccessor(s)
		var out struct {
			Pins []pinSummary `json:"pins"`
		}
		out.Pins = []pinSummary{}
		if acc != nil {
			out.Pins = collectPins(acc.PinOverrides())
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func OrchestratorProbe(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := resolveOrchestratorAccessor(s)
		var out struct {
			Tiers []tierStateRow `json:"tiers"`
		}
		out.Tiers = []tierStateRow{}
		if acc == nil {
			writeJSON(w, http.StatusOK, out)
			return
		}
		cb := acc.CircuitBreaker()
		tiers := acc.Tiers()
		if cb == nil || len(tiers) == 0 {
			writeJSON(w, http.StatusOK, out)
			return
		}
		ctx := r.Context()
		for _, t := range tiers {

			cb.AttemptRecovery(ctx, t)

			out.Tiers = append(out.Tiers, tierStateRow{
				Provider: t.Name(),
				Tier:     t.Tier().String(),
				State:    cb.State(t.Name()).String(),
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func OrchestratorHistory(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := resolveOrchestratorAccessor(s)
		var out struct {
			Tiers []tierStateRow `json:"tiers"`
			Note  string         `json:"note"`
		}
		out.Tiers = []tierStateRow{}
		out.Note = "post-rescope: CircuitBreaker does not track state-transition history; rendering current state per tier"
		if acc != nil {
			out.Tiers = collectTierStates(acc.CircuitBreaker(), acc.Tiers())
		}
		writeJSON(w, http.StatusOK, out)
	}
}
