// SPDX-License-Identifier: MIT
// Package cli — doctor_orchestrator_checks.go (Plan 3 Phase F K-6).
//
// 4 orchestrator-level health checks for `zen doctor`. Mirrors the
// runBypassChecks pattern from doctor_checks.go. Probes the daemon
// over existing /v1/orchestrator/* and /v1/budget endpoints using the
// typed client surface added in K-3 and K-4.
//
// Checks (in display order):
//  1. orchestrator.daemon-route-reachable — GET /v1/orchestrator/status returns 200
//  2. orchestrator.tier-states-clean      — all tiers in StateClosed; WARN if any non-closed
//  3. orchestrator.pin-overrides-reachable— GET /v1/orchestrator/pins returns 200
//  4. orchestrator.budget-reachable       — GET /v1/budget returns 200
package cli

import (
	"context"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func runOrchestratorChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkOrchestratorReachable,
		checkTierStatesClean,
		checkPinOverridesReachable,
		checkBudgetReachable,
	}
	results := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		results = append(results, fn(cctx, c))
		cancel()
	}
	return results
}

func checkOrchestratorReachable(ctx context.Context, c *client.Client) CheckResult {
	const name = "orchestrator.daemon-route-reachable"
	_, err := c.OrchestratorStatus(ctx)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: "fail",
			Detail: err.Error(),
			Hint:   "Ensure daemon is running: zen daemon start",
		}
	}
	return CheckResult{Name: name, Status: "ok"}
}

func checkTierStatesClean(ctx context.Context, c *client.Client) CheckResult {
	const name = "orchestrator.tier-states-clean"
	s, err := c.OrchestratorStatus(ctx)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: "fail",
			Detail: err.Error(),
			Hint:   "Run zen orchestrator status to investigate",
		}
	}
	var nonClosed []string
	for _, t := range s.Tiers {
		if t.State != "closed" {
			nonClosed = append(nonClosed, t.Tier+"="+t.State)
		}
	}
	if len(nonClosed) > 0 {
		return CheckResult{
			Name:   name,
			Status: "warn",
			Detail: "non-closed tiers: " + strings.Join(nonClosed, ", "),
			Hint:   "Tiers in suspect/open state recover via probe scheduler. Run zen orchestrator probe to force a recovery attempt.",
		}
	}
	return CheckResult{Name: name, Status: "ok"}
}

func checkPinOverridesReachable(ctx context.Context, c *client.Client) CheckResult {
	const name = "orchestrator.pin-overrides-reachable"
	_, err := c.OrchestratorPins(ctx)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: "fail",
			Detail: err.Error(),
			Hint:   "Restart daemon: zen daemon start",
		}
	}
	return CheckResult{Name: name, Status: "ok"}
}

func checkBudgetReachable(ctx context.Context, c *client.Client) CheckResult {
	const name = "orchestrator.budget-reachable"
	_, err := c.BudgetSummaryRollup(ctx, "24h")
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: "fail",
			Detail: err.Error(),
			Hint:   "Restart daemon: zen daemon start",
		}
	}
	return CheckResult{Name: name, Status: "ok"}
}
