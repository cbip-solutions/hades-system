// SPDX-License-Identifier: MIT
// Package cli — audit_chain_witness.go.
//
// `zen audit-chain witness` exposes daemon witness key management:
//
// rotate Rotate daemon ECDSA P-256 keypair; --reason MANDATORY.
// Old key kept for overlap window; rotation event anchored on chain.
// pubkey Print the current daemon witness pubkey + fingerprint.
// Read-only; used by external auditors verifying daemon-global
// STH co-signatures.
//
// Plan deviation (implementer): plan-file sketched AuditChainWitnessRotateReq
// struct and AuditChainWitnessRotateResp / AuditChainWitnessPubkeyResp types.
// H-7 actually shipped:
//
// client.AuditWitnessRotate(ctx, reason string) (AuditRotateResult, error)
// AuditRotateResult{NewKeyFingerprint, OldKeyFingerprint, RotatedAt}
// (NO OverlapWindowDays in the H-7 struct)
//
// client.AuditWitnessPubkey(ctx) (AuditWitnessPubkey, error)
// AuditWitnessPubkey{PubkeyPEM, Fingerprint, CreatedAt, RotationCount}
// (NOT HexPub / IssuedAt / NotAfterAt as plan-file proposed)
//
// This file uses the H-7 actuals throughout.
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

func auditChainWitnessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "witness",
		Short: "Daemon witness key management (rotate | pubkey)",
		Long: `Manage the daemon-level ECDSA P-256 witness key used to sign Tessera
tree heads (Signed Tree Heads, STH). rotate performs an overlap-signature
rotation: the outgoing key signs the new key's first tree-head so external
auditors can verify key continuity without a trust anchor reset. pubkey
prints the current public key in PEM format for distribution to external
auditors.

Rotation cadence: 90d under max-scope doctrine (default 365d).`,
	}
	cmd.AddCommand(auditChainWitnessRotateCmd())
	cmd.AddCommand(auditChainWitnessPubkeyCmd())
	return cmd
}

func auditChainWitnessRotateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate the daemon ECDSA P-256 witness key (overlap signature window)",
		Long: `Rotate the daemon-level ECDSA P-256 witness key with overlap semantics:
the outgoing key signs the new key's first Tessera tree-head so external
auditors can verify key continuity without a trust anchor reset.

--reason is MANDATORY (inv-zen-146): the operator's rationale is anchored on
the Plan 9 chain. An empty --reason is also rejected (cobra MarkFlagRequired
checks presence; RunE checks non-empty). Rotation events are visible via
` + "`zen audit-chain history`" + `.`,
		Example: `  zen audit-chain witness rotate --reason "scheduled 90d max-scope rotation"
  zen audit-chain witness rotate --reason "incident response — suspected key exposure #123"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reason, _ := cmd.Flags().GetString("reason")
			if strings.TrimSpace(reason) == "" {

				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason required and must be non-empty (inv-zen-146)"))
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).AuditWitnessRotate(ctx, reason)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Rotated daemon witness key.")
			fmt.Fprintf(out, "  Old fingerprint: %s\n", resp.OldKeyFingerprint)
			fmt.Fprintf(out, "  New fingerprint: %s\n", resp.NewKeyFingerprint)
			fmt.Fprintf(out, "  Rotated at:      %s\n", client.FormatUnix(resp.RotatedAt))
			fmt.Fprintln(out, "  daemon.witness_rotated event anchored on Plan 9 chain.")
			return nil
		},
	}
	c.Flags().String("reason", "", "Operator rationale (required, inv-zen-146)")
	_ = c.MarkFlagRequired("reason")
	return c
}

func auditChainWitnessPubkeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pubkey",
		Short: "Print daemon witness pubkey (PEM) for external auditors",
		Long: `Print the current daemon ECDSA P-256 witness public key in PEM format.
Distribute this key to external auditors so they can independently verify
Tessera tree-head signatures without daemon access.

Read-only: no chain anchor side effect; no --reason required.`,
		Example: `  zen audit-chain witness pubkey
  zen audit-chain witness pubkey --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			resp, err := newClientFromCmd(cmd).AuditWitnessPubkey(ctx)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Fingerprint:     %s\n", resp.Fingerprint)
			fmt.Fprintf(out, "Rotation count:  %d\n", resp.RotationCount)
			fmt.Fprintf(out, "Created at:      %s\n", client.FormatUnix(resp.CreatedAt))
			fmt.Fprintln(out, "Public key (PEM):")
			fmt.Fprintln(out, resp.PubkeyPEM)
			return nil
		},
	}
}
