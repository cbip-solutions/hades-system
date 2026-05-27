// SPDX-License-Identifier: MIT
// Package cli — doctor_audit_p9.go
//
// - audit.tessera — tessera tile-log: witness key + STH age + daemon-global checkpoint
// - audit.backup — Litestream replication lag + cold archive age + S3 reachability
// - audit.chain-integrity — last verify-chain age + tamper events count + last recovery action
//
// # Architecture
//
// RunAuditTesseraProbe, RunAuditBackupProbe, RunAuditChainIntegrityProbe are
// the exported orchestration functions consumed by NewDoctorAuditCmd's RunE
// closures and (future) RunFullProbe integration. Each function delegates to
// the typed prober interfaces declared in probe.go (TesseraProber,
// LitestreamProber, ChainProber, RecoveryProber) injected via DoctorDeps.
//
// Boundary (invariant):
//
// This file imports only cli-internal types + cobra + context. It does NOT
// import internal/store, internal/audit/tessera, internal/audit/chain,
// internal/audit/litestream, or internal/audit/recovery concrete types.
// Substrate adapters are wired by cmd/hades-ctld/main.go via the
// DoctorDeps fields declared in probe.go.
//
// Naming note:
//
// The release doctor_audit.go declares `doctorAuditCmd()` with Use="audit"
// . This file
// exports NewDoctorAuditCmd() as a SEPARATE exportable group builder that
// callers (cmd/hades) may register under a parent command. The two commands do
// not collide when registered at different levels of the cobra tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

// RunAuditTesseraProbe orchestrates the audit.tessera doctor check.
//
// Delegates to DoctorDeps.TesseraProber.Probe(ctx) and returns the
// resulting ProbeResult slice unchanged. Returns a non-nil error if
// TesseraProber is nil (mis-wired deps) or ctx is already cancelled.
//
// Typical result count: 3 (witness_key_health + last_sth_age_per_project +
// daemon_global_checkpoint_freshness). Callers MUST NOT branch on
// len(results) == 3; the contract is "any non-zero count of valid
// ProbeResults" so a future probe addition does not break callers.
func RunAuditTesseraProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.TesseraProber == nil {
		return nil, errors.New("RunAuditTesseraProbe: TesseraProber is nil — wire DoctorDeps.TesseraProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return deps.TesseraProber.Probe(pctx), nil
}

func RunAuditBackupProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.LitestreamProber == nil {
		return nil, errors.New("RunAuditBackupProbe: LitestreamProber is nil — wire DoctorDeps.LitestreamProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return deps.LitestreamProber.Probe(pctx), nil
}

// RunAuditChainIntegrityProbe orchestrates the audit.chain-integrity doctor
// check.
//
// Combines results from ChainProber (last_verify_age + tamper_events_count)
// and RecoveryProber (last_dispatch_status) into a single slice. Both probers
// must be non-nil. Returns a non-nil error if either is nil or ctx is
// already cancelled.
//
// Typical result count: 3 (2 from ChainProber + 1 from RecoveryProber).
// Callers MUST NOT branch on len(results) == 3; future probe additions
// on either prober will increase the count without breakage.
func RunAuditChainIntegrityProbe(ctx context.Context, deps DoctorDeps) ([]ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.ChainProber == nil {
		return nil, errors.New("RunAuditChainIntegrityProbe: ChainProber is nil — wire DoctorDeps.ChainProber")
	}
	if deps.RecoveryProber == nil {
		return nil, errors.New("RunAuditChainIntegrityProbe: RecoveryProber is nil — wire DoctorDeps.RecoveryProber")
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results := make([]ProbeResult, 0, 4)
	results = append(results, deps.ChainProber.Probe(pctx)...)
	results = append(results, deps.RecoveryProber.Probe(pctx)...)
	return results, nil
}

func NewDoctorAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit substrate health (Plan 9: tessera tile-log + Litestream backup + chain integrity)",
		Long: `Audit substrate health checks per Plan 9 Phase J spec §6.2.

Sub-commands:
  tessera         — daemon witness key health + per-project STH age + daemon-global checkpoint freshness
  backup          — Litestream replication lag + cold archive age + S3 reachability
  chain-integrity — last verify-chain age + tamper events count + last recovery action

Each check returns OK / WARN / FAIL severity with doctrine-tuned thresholds.
Use --strict (inherited from parent) to promote WARN to FAIL-equivalent for CI gating.

Prober interfaces are declared in internal/cli/probe.go; production wiring in
cmd/hades-ctld/main.go. Until wired, invoking a sub-command returns an error.`,
	}
	cmd.AddCommand(newDoctorAuditTesseraCmd())
	cmd.AddCommand(newDoctorAuditBackupCmd())
	cmd.AddCommand(newDoctorAuditChainIntegrityCmd())
	return cmd
}

func newDoctorAuditTesseraCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tessera",
		Short: "Tessera tile-log health (witness key + STH age + daemon-global checkpoint)",
		Long: `Run 3 tessera tile-log checks (spec §6.2):

  audit.tessera.witness_key_health              ECDSA P-256 witness keypair vs doctrine rotation cadence
  audit.tessera.last_sth_age_per_project        per-project STH age vs batch_max_age × 5/10 thresholds
  audit.tessera.daemon_global_checkpoint_freshness  last co-sign age vs 5/15 min thresholds

Exit codes:
  0  every check OK (or only WARNs without --strict)
  1  any check FAIL OR (any WARN AND --strict)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := RunAuditTesseraProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("audit.tessera probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Audit tessera:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

func newDoctorAuditBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Backup health (Litestream lag + cold archive age + S3 reachability)",
		Long: `Run 3 Litestream backup checks (spec §6.2):

  audit.backup.litestream_lag_per_project  per-project replication lag vs doctrine threshold
  audit.backup.cold_archive_last_rsync     per-project cold-archive rsync age vs doctrine cadence
  audit.backup.s3_reachability             HEAD probe to per-project bucket prefix

Exit codes:
  0  every check OK (or only WARNs without --strict)
  1  any check FAIL OR (any WARN AND --strict)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := RunAuditBackupProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("audit.backup probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Audit backup:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

func newDoctorAuditChainIntegrityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chain-integrity",
		Short: "Chain integrity (last verify-chain age + tamper events count + last recovery action)",
		Long: `Run 3 chain-integrity checks (spec §6.2):

  audit.chain.last_verify_age       per-project verify-chain run age vs doctrine cadence
  audit.chain.tamper_events_count   tamper_detected events in last 7d (OK=0 / WARN=1-3 / FAIL>3)
  audit.recovery.last_dispatch_status  last recovery action status (OK=none-in-30d / WARN=initiated-not-completed / FAIL=failed-in-24h)

Exit codes:
  0  every check OK (or only WARNs without --strict)
  1  any check FAIL OR (any WARN AND --strict)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			udsPath, strict := resolveDoctorFlags(cmd)
			deps, err := buildDoctorDepsFunc(udsPath, strict)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			probes, err := RunAuditChainIntegrityProbe(ctx, deps)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("audit.chain-integrity probe: %w", err))
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Audit chain integrity:")
			fmt.Fprint(out, RenderProbes(probes))
			code := ExitCode(probes, strict)
			if code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}
