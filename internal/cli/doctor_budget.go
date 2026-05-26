// SPDX-License-Identifier: MIT
// Package cli — doctor_budget.go (Plan 4 Phase N Task N-8).
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func doctorBudgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "budget",
		Short: "Budget engine health (events, anomaly, axes)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Budget (Plan 4)", runBudgetChecks)
		},
	}
}

func runBudgetChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkBudgetEventsReachable,
		checkBudgetCapStatusReachable,
	}
	out := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out = append(out, fn(cctx, c))
		cancel()
	}
	return out
}

func checkBudgetEventsReachable(ctx context.Context, c *client.Client) CheckResult {
	events, err := c.BudgetEvents(ctx, 0, 1)
	if err != nil {
		return CheckResult{Name: "budget.events.reachable", Status: "fail", Detail: err.Error(),
			Hint: "daemon /v1/budget/events unreachable"}
	}
	return CheckResult{Name: "budget.events.reachable", Status: "ok",
		Detail: fmt.Sprintf("%d recent events", len(events))}
}

func checkBudgetCapStatusReachable(ctx context.Context, c *client.Client) CheckResult {

	_, err := c.BudgetCapStatusCall(ctx, "stage", "design")
	if err != nil {
		return CheckResult{Name: "budget.cap_status.reachable", Status: "fail", Detail: err.Error(),
			Hint: "daemon /v1/budget/cap_status unreachable"}
	}
	return CheckResult{Name: "budget.cap_status.reachable", Status: "ok"}
}
