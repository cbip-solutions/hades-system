// SPDX-License-Identifier: MIT
// Package recovery — doctor.go
//
// substrates that the audit chain depends on:
//
// - audit.backup reports per-project backup substrate freshness
// (litestream replica age, tessera rsync age,
// cold archive age, S3 reachability) per spec
// §6.2 lines 1527-1540.
// - audit.chain-integrity reports per-project chain integrity history
// (last verify-chain age + 7d tamper event count)
// per spec §1 Q10 line 657.
//
// The doctor LOGIC lives here in package recovery so it can be unit-
// tested via stubbed BackupStatus / ChainStatus interfaces; the cobra
// surface lives in internal/cli/doctor_audit_{backup,chain_integrity}.go
// and delegates to RunDoctorAuditBackup / RunDoctorAuditChainIntegrity
// via a daemon HTTP round-trip.
//
// invariant: this file does NOT import internal/store. Backup status
// fields are read through a small extension on Manager / RsyncScheduler
// and ChainStatus reads from the in-memory cache
// populated by tamper.scheduler. The doctor returns CheckResult
// (a CLI-shape value type) so internal/cli can adapt without importing
// recovery's full surface.
package recovery

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/litestream"
)

type CheckResult struct {
	Name   string
	Status string
	Detail string
	Hint   string
}

type BackupStatus interface {
	LitestreamLastAt(projectID string) time.Time
	RsyncLastSuccess(projectID string) time.Time
	RsyncLastError(projectID string) string
	ColdArchiveLastAt(ctx context.Context, projectID string) (time.Time, error)
	S3Reachable(ctx context.Context, projectID string) bool
}

type ChainStatus interface {
	LastVerifyChain(projectID string) (VerifyResult, bool)
	TamperCount7d(ctx context.Context, projectID string) (int, error)
}

func RunDoctorAuditBackup(ctx context.Context, projectID, doctrine string, st BackupStatus) CheckResult {
	now := time.Now()
	if !st.S3Reachable(ctx, projectID) {
		return CheckResult{
			Name:   "audit.backup",
			Status: "fail",
			Detail: fmt.Sprintf("S3 unreachable for project %s", projectID),
			Hint:   "verify AWS credentials via `zen audit configure-s3 --project " + projectID + "`",
		}
	}

	lastLS := st.LitestreamLastAt(projectID)
	lastRsync := st.RsyncLastSuccess(projectID)
	lastCold, _ := st.ColdArchiveLastAt(ctx, projectID)

	rsyncCadence := litestream.RsyncCadenceForDoctrine(doctrine)

	rsyncLag := now.Sub(lastRsync)
	lsLag := now.Sub(lastLS)

	status := "ok"
	hint := ""
	detail := fmt.Sprintf("litestream_lag=%s rsync_lag=%s cold_age=%s",
		humanDuration(lsLag), humanDuration(rsyncLag), humanDuration(now.Sub(lastCold)))

	if !lastLS.IsZero() && lsLag > 1*time.Hour {
		status = worse(status, "warn")
		hint = "litestream replication lag > 1h"
	}
	if !lastLS.IsZero() && lsLag > 6*time.Hour {
		status = worse(status, "fail")
		hint = "litestream replication > 6h stale; check S3 credentials + bucket"
	}
	if !lastRsync.IsZero() && rsyncLag > time.Duration(float64(rsyncCadence)*1.5) {
		status = worse(status, "warn")
		hint = "tessera rsync overdue (cadence " + rsyncCadence.String() + ")"
	}
	if !lastRsync.IsZero() && rsyncLag > 3*rsyncCadence {
		status = worse(status, "fail")
		hint = "tessera rsync > 3× cadence; investigate via daemon logs"
	}
	if !lastCold.IsZero() && now.Sub(lastCold) > 35*24*time.Hour {
		status = worse(status, "warn")
		hint = "cold archive > 35d old (month-end seal expected); investigate seal worker"
	}

	if errMsg := st.RsyncLastError(projectID); errMsg != "" && status == "ok" {
		status = "warn"
		hint = "last rsync error: " + truncate(errMsg, 80)
	}

	return CheckResult{Name: "audit.backup", Status: status, Detail: detail, Hint: hint}
}

func RunDoctorAuditChainIntegrity(ctx context.Context, projectID, doctrine string, st ChainStatus) CheckResult {
	tamperN, _ := st.TamperCount7d(ctx, projectID)
	verify, present := st.LastVerifyChain(projectID)
	if !present {
		return CheckResult{
			Name:   "audit.chain-integrity",
			Status: "fail",
			Detail: fmt.Sprintf("verify-chain never run for project %s", projectID),
			Hint:   "run `zen audit verify-chain --project " + projectID + "`",
		}
	}

	cadence := chainVerifyCadenceFor(doctrine)
	lag := time.Since(verify.StartedAt)

	status := "ok"
	hint := ""
	detail := fmt.Sprintf("verify_age=%s records_checked=%d seals_checked=%d tamper_count_7d=%d",
		humanDuration(lag), verify.RecordsChecked, verify.PartitionSealsChecked, tamperN)

	if !verify.Clean {
		status = "fail"
		hint = fmt.Sprintf("last verify-chain detected tamper at record %d (path %s)",
			verify.FirstTamperRecordID, verify.FirstTamperPath)
	}
	if tamperN > 0 {
		status = worse(status, "fail")
		hint = fmt.Sprintf("%d tamper events in last 7d; run `zen audit recover --project %s`", tamperN, projectID)
	}
	if lag > time.Duration(float64(cadence)*1.5) {
		status = worse(status, "warn")
		if hint == "" {
			hint = "verify-chain overdue (cadence " + cadence.String() + ")"
		}
	}
	if lag > 3*cadence {
		status = worse(status, "fail")
		hint = "verify-chain > 3× cadence stale; tamper.scheduler may be stuck"
	}

	return CheckResult{Name: "audit.chain-integrity", Status: status, Detail: detail, Hint: hint}
}

func chainVerifyCadenceFor(doctrine string) time.Duration {
	switch doctrine {
	case "default":
		return 7 * 24 * time.Hour
	case "capa-firewall":
		return 24 * time.Hour
	case "max-scope", "":
		return 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func worse(a, b string) string {
	rank := map[string]int{"ok": 0, "warn": 1, "fail": 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Second).String()
	}
	if d < 24*time.Hour {
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Hour).String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
