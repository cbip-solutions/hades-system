// SPDX-License-Identifier: MIT
// Package handlers — budget.go.
//
// Replaces the stub trio (BudgetAll/Project/Raise → 501). K-4
// retains ONE real route: GET /v1/budget?range=<window>, a read-only
// per-(project, profile, tier) spend rollup backed by CostCounters.
//
// The + wire path (BudgetProject + BudgetRaise) stays at 501
// pending (ProfileResolver) + cost_cap_changes table. Mutations
// require a write path that does not exist post-rescope; deferring is
// load-bearing per max-scope-meta doctrine — building a stub today
// becomes retrofit debt tomorrow when the real cap source lands.
//
// Boundary: consumes the Server pointer as `any` and
// reuses orchestratorAccessor (defined in handlers/orchestrator.go) so
// this file shares the K-3 nil-safe accessor wiring without re-defining
// the interface. Production *daemon.Server satisfies it structurally.
//
// Window restriction: CostCounters registers WindowCounter only for
// 24h and 30d. parseRange accepts those plus shorter Go-duration values
// (1h, 2h,...) that ProjectProfileTierTotal MUST reject (panics on
// unsupported durations). To keep handler responses friendly, parseRange
// PRE-VALIDATES against the allowed set — operators see "unsupported
// range" 400 rather than a 500 from the panic.
package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type budgetTierSpend struct {
	Project  string  `json:"project"`
	Profile  string  `json:"profile"`
	Tier     string  `json:"tier"`
	SpendUSD float64 `json:"spend_usd"`
}

type budgetSummaryResp struct {
	Range    string            `json:"range"`
	TotalUSD float64           `json:"total_usd"`
	ByTier   []budgetTierSpend `json:"by_tier"`
}

func parseRange(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("range is required")
	}
	if strings.HasSuffix(s, "d") && !strings.ContainsAny(s, ".eE") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid range %q: %w", s, err)
		}
		if n <= 0 {
			return 0, fmt.Errorf("invalid range %q: must be positive", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid range %q: %w", s, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid range %q: must be positive", s)
	}
	return d, nil
}

// supportedBudgetWindow gates parseRange output against the windows
// CostCounters actually registers. Restricted set lets us avoid the
// internal panic path AND lets future plans extend the WindowCounter
// table without changing this code (extend the switch + add the
// CostCounters constant simultaneously).
//
// Returns the duration unchanged on success; an error otherwise. The
// allowed set is intentionally small: ships 24h + 30d; future
// plans MUST add ledger-side support before exposing more windows.
func supportedBudgetWindow(d time.Duration) error {
	switch d {
	case 24 * time.Hour, 30 * 24 * time.Hour:
		return nil
	}
	return fmt.Errorf("unsupported range %v: only 24h and 30d are registered (Plan 3 v0.3.0)", d)
}

func BudgetSummary(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rng := r.URL.Query().Get("range")
		if rng == "" {
			rng = "24h"
		}
		d, err := parseRange(rng)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := supportedBudgetWindow(d); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := budgetSummaryResp{
			Range:    rng,
			TotalUSD: 0,
			ByTier:   []budgetTierSpend{},
		}
		acc := resolveOrchestratorAccessor(s)
		if acc == nil {
			writeJSON(w, http.StatusOK, out)
			return
		}
		cc := acc.CostCounters()
		if cc == nil {
			writeJSON(w, http.StatusOK, out)
			return
		}
		keys := cc.AllKeys()
		rows := make([]budgetTierSpend, 0, len(keys))
		var total float64
		for _, k := range keys {
			usd := cc.ProjectProfileTierTotal(k.Project, k.Profile, k.Tier, d)
			total += usd
			rows = append(rows, budgetTierSpend{
				Project:  k.Project,
				Profile:  k.Profile,
				Tier:     k.Tier,
				SpendUSD: usd,
			})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Tier != rows[j].Tier {
				return rows[i].Tier < rows[j].Tier
			}
			if rows[i].Project != rows[j].Project {
				return rows[i].Project < rows[j].Project
			}
			return rows[i].Profile < rows[j].Profile
		})
		out.ByTier = rows
		out.TotalUSD = total
		writeJSON(w, http.StatusOK, out)
	}
}

func BudgetProject(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 4, "Workforce + MCPs implementations")
	}
}

func BudgetRaise(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notImplemented(w, 4, "Workforce + MCPs implementations")
	}
}
