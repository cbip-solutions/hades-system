// SPDX-License-Identifier: MIT
// Package cli — doctor_audit_backup.go
//
// Reports per-project backup-substrate health (litestream replica age,
// Tessera rsync age, cold archive age, S3 reachability) per spec §6.2
// lines 1527-1540. Status escalation logic lives in
// internal/audit/recovery/doctor.go::RunDoctorAuditBackup so it can be
// unit-tested under -race -count=2 against stub BackupStatus
// implementations; the CLI just adapts the daemon response into
// CheckResult rows for the existing renderCheck pipeline.
//
// The surface ships the call-sites in
// internal/client.AuditDoctorBackup; wires the daemon-side
// HTTP handler to invoke RunDoctorAuditBackup with the production
// BackupStatus adapter (Manager + RsyncScheduler +
// auditadapter.PartitionSealStore + AWS-CLI prober). Until then the
// stub returns an empty slice and the CLI falls through to the
// "no projects configured" warn row so operators get a stable signal
// instead of a 5xx panic.
package cli

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/audit/recovery"
	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/spf13/cobra"
)

func doctorAuditBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit.backup",
		Short: "audit backup substrate health (litestream + tessera rsync + cold archive)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Audit backup (Plan 9)", runAuditBackupChecks)
		},
	}
}

func runAuditBackupChecks(ctx context.Context, c *client.Client) []CheckResult {
	resp, err := c.AuditDoctorBackup(ctx)
	if err != nil {
		return []CheckResult{{
			Name:   "audit.backup",
			Status: "fail",
			Detail: err.Error(),
			Hint:   "daemon /v1/audit-chain/doctor/backup unreachable",
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
			Name:   "audit.backup",
			Status: "warn",
			Detail: "no projects configured",
			Hint:   "run `zen audit configure-s3 --project <id>` then restart daemon",
		})
	}
	return out
}

func adaptCheckResult(r recovery.CheckResult) CheckResult {
	return CheckResult{
		Name:   r.Name,
		Status: r.Status,
		Detail: r.Detail,
		Hint:   r.Hint,
	}
}
