// SPDX-License-Identifier: MIT
// Package cli — audit_chain.go.
//
// `hades audit-chain` is the HADES design umbrella for chain-integrity, backup,
// witness, and recovery operations. Distinct's `hades audit`
// event-emit/query surface (which lives in audit.go and serves the
// audit_events_raw row-level read API).
//
// Spec §6.1 enumerates 7 direct subcommands; cobra registers them here:
// - verify-chain: walk the tile-log Merkle + record_hash + witness sig [I-2]
// - history: query HADES design eventlog with chain proofs [I-2]
// - recover: interactive recovery (Litestream + cold archive + verify) [I-2]
// - checkpoint: capa-firewall manual checkpoint (invariant §1 design choice B) [I-3]
// - cold-archive: nested group → ls | restore [I-3]
// - configure-s3: interactive Keychain-integrated S3 credential setup [I-4]
// - witness: nested group → rotate | pubkey [I-4]
//
// Cobra layout:
//
// hades audit-chain verify-chain --project <id>
// hades audit-chain history [--filter <type>] [--since <ts>] [--project <id>]
// hades audit-chain recover --project <id> --from <ts>
// hades audit-chain checkpoint --reason "<X>"
// hades audit-chain cold-archive ls [--project <id>]
// hades audit-chain cold-archive restore --partition <YYYY_MM> [--project <id>]
// hades audit-chain configure-s3 --project <id>
// hades audit-chain witness rotate --reason "<X>"
// hades audit-chain witness pubkey
//
// invariant (--reason mandatory) is enforced per leaf inside the individual
// constructors (checkpoint, witness rotate); see Tasks I-3, I-4.
// Cross-cutting reason-flag enforcement test lives in reason_flag_test.go.
//
// Wiring I-1 shipped scaffolds. I-2 wires verify-chain, history, recover
// by delegating to newAuditChainVerifyCmd / newAuditChainHistoryCmd /
// newAuditChainRecoverCmd (audit_chain_verify.go, audit_chain_history.go,
// audit_chain_recover.go). I-3/I-4 replace the remaining notWiredYet stubs.
package cli

import (
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func NewAuditChainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit-chain",
		Short: "Audit chain integrity, backup, witness, recovery (HADES design)",
		Long: "HADES design operator surface for the per-project tile-log + Litestream +\ncold archive + ECDSA witness substrate. Use this group to verify chain\nintegrity, browse historical events with chain proofs, recover from\ntamper events interactively, manually checkpoint capa-firewall projects,\nmanage Tessera cold archives, configure per-project S3 backup\ncredentials, and rotate the daemon witness key.\n\nHADES design " +
			"`hades audit`" + " group remains for raw event emit/query.\nHADES design " +
			"`hades audit-chain`" + ` group is for chain operations.`,
		Example: " # Verify integrity for one project\n  hades audit-chain verify-chain --project hades-system\n\n # Manually checkpoint capa-firewall project before sensitive batch\n  hades audit-chain checkpoint --reason \"pre-merge audit anchor for v0.9.0\"\n\n # Recover from detected tamper interactively\n  hades audit-chain recover --project hades-system --from 2026-05-06T08:00:00Z\n\n # Rotate the daemon-level ECDSA witness key\n  hades audit-chain witness rotate --reason \"scheduled 90d rotation\"",
	}
	format.AttachFlags(cmd)
	cmd.AddCommand(auditChainVerifyCmd())
	cmd.AddCommand(auditChainHistoryCmd())
	cmd.AddCommand(auditChainRecoverCmd())
	cmd.AddCommand(auditChainCheckpointCmd())
	cmd.AddCommand(auditChainColdArchiveCmd())
	cmd.AddCommand(auditChainConfigureS3Cmd())
	cmd.AddCommand(auditChainWitnessCmd())
	return cmd
}

func notWiredYet(taskRef string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		return ierrors.Wrap(ierrors.Code("internal-uncaught"), fmt.Errorf("stage I task %s: leaf unavailable (see HADES design)", taskRef))
	}
}

func auditChainVerifyCmd() *cobra.Command {
	return newAuditChainVerifyCmd()
}

func auditChainHistoryCmd() *cobra.Command {
	return newAuditChainHistoryCmd()
}

func auditChainRecoverCmd() *cobra.Command {
	return newAuditChainRecoverCmd()
}

func auditChainCheckpointCmd() *cobra.Command {
	return newAuditChainCheckpointCmd()
}

func auditChainColdArchiveCmd() *cobra.Command {
	return newAuditChainColdArchiveCmd()
}
