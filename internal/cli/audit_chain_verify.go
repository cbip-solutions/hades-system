// SPDX-License-Identifier: MIT
// Package cli — audit_chain_verify.go.
//
// `hades audit-chain verify-chain --project <id>` walks the per-project audit
// chain end-to-end via POST /v1/audit-chain/verify-chain:
// 1. Per-record hash chain integrity (audit_events_raw.record_hash)
// 2. Tessera Merkle inclusion proofs (PartitionSeals count)
// 3. Daemon ECDSA P-256 witness signature on every partition seal (WitnessChecks)
// 4. Global VerifiedAtUnix timestamp confirms the walk completed
//
// Status OK iff TamperedRecords is empty (len == 0). Any tampered record
// causes the walk to surface the offending record_id + reason so the operator
// can immediately invoke `hades audit-chain recover`.
//
// Used by:
// - `hades doctor audit.chain-integrity`
// - `hades autonomy --check` (system-design §10.2.5 prereqs)
// - `hades day` morning brief audit section
// - operators investigating tamper events
//
// Plan deviation: the plan-file sketched client.AuditChainVerify / AuditChainVerifyResp;
// H-7 actually shipped client.AuditVerifyChain / client.AuditVerifyResp. This
// file uses the H-7 actuals (see implementer brief).
package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func newAuditChainVerifyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "verify-chain",
		Short: "Walk chain (record_hash + Tessera Merkle + witness sig + daemon-global cross-ref)",
		Long: `Walk every chain position for a project: verify each event's
record_hash, validate Merkle inclusion proofs against the Tessera tile-log,
confirm the ECDSA P-256 witness signature on the tree head, and cross-reference
the global root held by the daemon. Reports the first tampered position and
exits non-zero on any inconsistency.

Status OK iff TamperedRecords is empty. On FAIL, run:
  hades audit-chain recover --project <id> --from <ts>

Required: --project.`,
		Example: `  hades audit-chain verify-chain --project hades-system
  hades audit-chain verify-chain --project internal-platform-x --format json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, _ := cmd.Flags().GetString("project")
			if project == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--project required"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).AuditVerifyChain(ctx, project, 0)
			if err != nil {
				return err
			}

			opts := format.OptionsFromFlags(cmd)
			out := cmd.OutOrStdout()

			if opts.Format != "table" {

				return format.Render(out, opts, []client.AuditVerifyResp{resp}, nil)
			}

			status := "OK"
			if len(resp.TamperedRecords) > 0 {
				status = fmt.Sprintf("FAIL (%d tampered record(s))", len(resp.TamperedRecords))
			}

			fmt.Fprintf(out, "Project:        %s\n", resp.ProjectID)
			fmt.Fprintf(out, "Records valid:  %s\n", strconv.FormatInt(resp.RecordsValid, 10))
			fmt.Fprintf(out, "Partitions:     %s\n", strconv.Itoa(resp.PartitionSeals))
			fmt.Fprintf(out, "Witness checks: %s\n", strconv.Itoa(resp.WitnessChecks))
			fmt.Fprintf(out, "Last verify:    %s\n", client.FormatUnix(resp.VerifiedAtUnix))
			fmt.Fprintf(out, "Status:         %s\n", status)

			if len(resp.TamperedRecords) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, "Tampered records:")
				for _, tr := range resp.TamperedRecords {
					fmt.Fprintf(out, "  record_id=%d  reason=%s\n", tr.RecordID, tr.Reason)
				}
				fmt.Fprintf(out, "\nRun: hades audit-chain recover --project %s --from <ts>\n", resp.ProjectID)
			}
			return nil
		},
	}
	c.Flags().String("project", "", "Project ID (required)")
	return c
}
