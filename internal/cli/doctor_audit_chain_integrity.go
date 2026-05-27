// SPDX-License-Identifier: MIT
// Package cli — doctor_audit_chain_integrity.go
//
// surface.
//
// Reports per-project chain-integrity history (last verify-chain age +
// 7d tamper event count) per spec §1 Q10 line 657. Status escalation
// logic lives in
// internal/audit/recovery/doctor.go::RunDoctorAuditChainIntegrity so
// it can be unit-tested under -race -count=2 against stub ChainStatus
// implementations; the CLI just adapts the daemon response.
//
// As with audit.backup, the surface ships the call-site in
// internal/client.AuditDoctorChainIntegrity; wires the
// daemon-side handler that invokes RunDoctorAuditChainIntegrity with
// the production ChainStatus adapter (in-memory verify-cache populated
// by tamper.scheduler + release eventlog tamper-count query).
package cli

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/spf13/cobra"
)

func doctorAuditChainIntegrityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit.chain-integrity",
		Short: "audit chain integrity health (last verify-chain + tamper history)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Audit chain integrity (Plan 9)", runAuditChainIntegrityChecks)
		},
	}
}

func runAuditChainIntegrityChecks(ctx context.Context, c *client.Client) []CheckResult {
	resp, err := c.AuditDoctorChainIntegrity(ctx)
	if err != nil {
		return []CheckResult{{
			Name:   "audit.chain-integrity",
			Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/audit-chain/doctor/chain-integrity unreachable",
		}}
	}
	out := make([]CheckResult, 0, len(resp))
	for _, perProject := range resp {
		out = append(out, CheckResult{
			Name:   perProject.Name,
			Status: perProject.Status,
			Detail: perProject.Detail,
			Hint:   perProject.Hint,
		})
	}
	if len(out) == 0 {
		out = append(out, CheckResult{
			Name:   "audit.chain-integrity",
			Status: "warn",
			Detail: "no projects configured",
			Hint:   "run `hades audit verify-chain --project <id>` to bootstrap a baseline",
		})
	}
	return out
}
