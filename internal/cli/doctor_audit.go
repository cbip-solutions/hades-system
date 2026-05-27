// SPDX-License-Identifier: MIT
// Package cli — doctor_audit.go.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/spf13/cobra"
)

func doctorAuditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit",
		Short: "Audit pipeline health (events, family-disjoint pool, criteria)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Audit (Plan 4)", runAuditChecks)
		},
	}
}

func runAuditChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkAuditEventsReachable,
		checkAuditFamilyDisjointSize,
		checkAuditCriteriaLoaded,
	}
	out := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out = append(out, fn(cctx, c))
		cancel()
	}
	return out
}

func checkAuditEventsReachable(ctx context.Context, c *client.Client) CheckResult {
	if _, err := c.AuditEvents(ctx, "", "", 0, 1); err != nil {
		return CheckResult{Name: "audit.events.reachable", Status: "fail", Detail: err.Error(),
			Hint: "daemon /v1/audit/events unreachable"}
	}
	return CheckResult{Name: "audit.events.reachable", Status: "ok"}
}

func checkAuditFamilyDisjointSize(ctx context.Context, c *client.Client) CheckResult {

	fams, err := c.AuditFamiliesResolve(ctx)
	if err != nil || len(fams) == 0 {
		fams = client.AuditFamilies()
	}
	defaults := 0
	for _, f := range fams {
		if f.Default {
			defaults++
		}
	}
	if defaults < 2 {
		return CheckResult{Name: "audit.family_disjoint.size", Status: "fail",
			Detail: fmt.Sprintf("active family pool size = %d; inv-hades-080 requires ≥2", defaults),
			Hint:   "review doctrine.reviewer.family_disjoint_pool config"}
	}
	return CheckResult{Name: "audit.family_disjoint.size", Status: "ok",
		Detail: fmt.Sprintf("active pool size = %d (inv-hades-080)", defaults)}
}

func checkAuditCriteriaLoaded(_ context.Context, _ *client.Client) CheckResult {
	crits := client.AuditCriteria()
	if len(crits) < 1 {
		return CheckResult{Name: "audit.criteria.loaded", Status: "fail",
			Detail: "no criteria templates"}
	}
	return CheckResult{Name: "audit.criteria.loaded", Status: "ok",
		Detail: fmt.Sprintf("%d criteria templates", len(crits))}
}
