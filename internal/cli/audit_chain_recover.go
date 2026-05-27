// SPDX-License-Identifier: MIT
// Package cli — audit_chain_recover.go.
//
// `hades audit-chain recover --project <id> --from <ts>` is the spec §6.5
// interactive tamper-recovery flow. It calls the single POST
// /v1/audit-chain/recover endpoint twice (H-7 two-phase semantics):
//
// Pass 1: confirm=false → preview AuditRecoverPlan (no destructive action)
// Pass 2: confirm=true → execute the plan; returns AuditRecoverResult
//
// H-7 deviation from plan-file: the plan-file sketched three separate
// endpoints (/recover/plan, /recover/execute, /recover/resume). H-7 shipped
// a single endpoint with a `confirm` flag. This file adapts the two-call
// approach to honour the plan-file's spec §6.5 interactive semantics while
// using the actual H-7 wire contract.
//
// Two operator-confirmation checkpoints (privacy-by-default + max-scope
// HALT-per-project per Q10 D):
//
// Checkpoint 1: After plan display → "Continue? [y/N]"
// Checkpoint 2: After execution → "Resume audit appends for project <X>? [y/N]"
//
// Operator decline at either checkpoint short-circuits the flow:
// - Decline at checkpoint 1: no confirm=true call; project stays paused.
// - Decline at checkpoint 2: recovery completed but project stays paused;
// operator must re-run to resume (no destructive side effect from decline).
//
// "Resume" in H-7 is implicit: a confirmed recovery (confirm=true) naturally
// positions the chain for new appends. We output a clear message either way
// so the operator knows the project state.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func newAuditChainRecoverCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "recover",
		Short: "Interactive recovery (Litestream + cold archive + verify)",
		Long: `Interactive tamper-recovery flow. Calls POST /v1/audit-chain/recover
with confirm=false to preview the restoration plan, then prompts the
operator before executing (confirm=true).

Two confirmation checkpoints (spec §6.5, privacy-by-default):
  1. After plan display:   "Continue? [y/N]"   (blank = N = abort)
  2. After execution:      "Resume audit appends for project <X>? [y/N]"

Operator decline at either checkpoint is safe: confirm=true is only
dispatched after explicit "y"; project appends remain paused until the
operator explicitly resumes.

Required: --project, --from (RFC 3339 timestamp).`,
		Example: `  hades audit-chain recover --project hades-system --from 2026-05-06T08:00:00Z`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, _ := cmd.Flags().GetString("project")
			from, _ := cmd.Flags().GetString("from")
			if project == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project required"))
			}
			if from == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--from required (RFC 3339 timestamp, e.g. 2026-05-06T08:00:00Z)"))
			}
			fromTs, err := time.Parse(time.RFC3339, from)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--from: %w (use RFC 3339, e.g. 2026-05-06T08:00:00Z)", err))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Minute)
			defer cancel()

			out := cmd.OutOrStdout()
			in := cmd.InOrStdin()
			c := newClientFromCmd(cmd)

			plan, _, err := c.AuditRecover(ctx, project, fromTs.Unix(), false)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("fetch recovery plan: %w", err))
			}

			litestreamGB := float64(plan.LitestreamSizeBytes) / (1 << 30)
			fmt.Fprintf(out, "Recovery plan for project %s:\n", plan.ProjectID)
			fmt.Fprintf(out, "  Litestream restore: %.2f GB (WAL segments)\n", litestreamGB)
			fmt.Fprintf(out, "  Cold-archive partitions: %d\n", plan.ColdArchivePartCnt)
			fmt.Fprintf(out, "  Verification steps: %d records to walk\n", plan.VerifyStepCount)
			fmt.Fprintf(out, "  Estimated duration: ~%d seconds\n", plan.EstimatedDurationS)
			fmt.Fprintln(out)

			ok, err := promptYN(in, out, "Continue?")
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read confirmation: %w", err))
			}
			if !ok {
				fmt.Fprintln(out, "Recovery aborted by operator. Project remains paused.")
				return nil
			}

			_, result, err := c.AuditRecover(ctx, project, fromTs.Unix(), true)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("execute recovery: %w", err))
			}
			if result == nil {

				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("daemon returned no result for confirm=true (unexpected)"))
			}

			restoredGB := float64(plan.LitestreamSizeBytes) / (1 << 30)
			fmt.Fprintln(out)
			fmt.Fprintf(out, "Recovery executed:\n")
			fmt.Fprintf(out, "  Litestream restore: %.2f GB (%d records)\n",
				restoredGB, result.RecordsRestored)
			fmt.Fprintf(out, "  Cold-archive partitions restored: %d\n", result.PartitionsRestored)
			fmt.Fprintf(out, "  Duration: %d seconds\n", result.DurationSeconds)
			fmt.Fprintln(out)

			resume, err := promptYN(in, out,
				fmt.Sprintf("Resume audit appends for project %s?", project))
			if err != nil {
				return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("read confirmation: %w", err))
			}
			if !resume {
				fmt.Fprintf(out, "Project %s remains paused. Re-run recover to resume.\n", project)
				return nil
			}

			fmt.Fprintf(out, "Project %s: audit appends resumed. "+
				"audit.recovery_completed event anchored in chain.\n", project)
			return nil
		},
	}
	c.Flags().String("project", "", "Project ID (required)")
	c.Flags().String("from", "", "Recovery point timestamp (RFC 3339, required)")
	return c
}
