// SPDX-License-Identifier: MIT
// Package cli — audit_chain_checkpoint.go.
//
// `zen audit-chain checkpoint --reason "<X>"` is the capa-firewall manual
// checkpoint surface (spec §6.1 Q4 B). Operators trigger an immediate
// Tessera batch flush + STH co-signature publication before sensitive
// operations (release tag, doctrine amendment, large migration). The reason
// string is stored in the chain anchor for forensic traceability.
//
// invariant: --reason is MANDATORY. cobra MarkFlagRequired checks that
// the flag is present but does NOT enforce non-empty; RunE performs the
// second guard (strings.TrimSpace) so an explicit --reason "" is also
// rejected before the HTTP call.
//
// Plan deviation (implementer brief): the plan-file sketched a struct-based
// AuditChainCheckpointReq / AuditChainCheckpointResp. H-7 actually shipped:
// - client.AuditCheckpoint(ctx, reason, doctrine string) AuditCheckpointResp
// - AuditCheckpointResp{CheckpointID, TesseraSTH, AnchoredAt}
//
// This file uses the H-7 actuals.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func newAuditChainCheckpointCmd() *cobra.Command {
	var reason, doctrine string
	c := &cobra.Command{
		Use:   "checkpoint",
		Short: "capa-firewall manual checkpoint — Tessera flush + STH co-sign (Q4 B)",
		Long: `Force an immediate Tessera batch flush + STH co-signature publication
on the daemon-global checkpoint log. Useful on capa-firewall doctrine before
sensitive operations (release tag, doctrine amendment, large migration) to
create an explicit chain anchor that ` + "`zen audit-chain verify-chain`" + ` and
` + "`zen audit-chain history`" + ` can reference for forensic traceability.

The --reason flag is MANDATORY (inv-zen-146): the operator's rationale is
stored in the chain anchor. An empty --reason is also rejected (cobra
MarkFlagRequired checks presence; RunE checks non-empty).`,
		Example: `  zen audit-chain checkpoint --reason "pre-release v0.9.0 audit anchor"
  zen audit-chain checkpoint --reason "operator decision per Plan 9 brainstorm Q4 B"
  zen audit-chain checkpoint --reason "pre-migrate anchor" --doctrine capa-firewall`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(reason) == "" {

				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason required and must be non-empty (inv-zen-146: auto-checkpoint forbidden)"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).AuditCheckpoint(ctx, reason, doctrine)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "checkpoint_id=%s\n", resp.CheckpointID)
			fmt.Fprintf(out, "tessera_sth=%s\n", resp.TesseraSTH)
			fmt.Fprintf(out, "anchored_at=%s\n", client.FormatUnix(resp.AnchoredAt))
			return nil
		},
	}
	c.Flags().StringVar(&reason, "reason", "", "Operator rationale (required, inv-zen-146)")
	c.Flags().StringVar(&doctrine, "doctrine", "", "Doctrine override (capa-firewall|max-scope|default); defaults to active doctrine")
	_ = c.MarkFlagRequired("reason")
	return c
}
