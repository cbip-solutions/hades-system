// SPDX-License-Identifier: MIT
// Package cli — state.go.
//
// `zen state` is the operator surface for the persistence state model.
//
// show — render system-state.toml manifest table
// regenerate — rebuild manifest from authoritative sources
// verify — regenerate-and-diff CI gate
// pin — set a manual field with --reason
// history — walk chain showing manual field changes
// list — enumerate XDG state entries
// cleanup — apply retention policy to XDG state
//
// Wire types from internal/client/state.go (H-9 final shapes):
//
// StateManifest, StateRegenerateReq, StateRegenerateResp,
// StateDiff, StatePinReq, StateChange.
//
// Plan-file fictitious type names (StateShowResp, StateField,
// StateVerifyResp, StateHistoryEntry, StatePinResp) are NOT used; the
// implementation uses the actual shipped types above.
package cli

import "github.com/spf13/cobra"

func NewStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Persistence state surface (Plan 9 manifest + Plan 13 XDG retention)",
		Long: `zen state — persistence state model surface.

Plan 9 leaves (system-state.toml manifest, spec §6.1 Q9 E):
  show       — render manifest table (TomlContent + metadata)
  regenerate — rebuild manifest from authoritative sources (--dry-run)
  verify     — regenerate-and-diff CI gate (non-zero exit on drift;
               integrated with make verify-invariants)
  pin        — set a manual field with --reason (inv-zen-146)
  history    — walk Plan 9 chain showing manual field changes

Plan 13 Phase F7 leaves (XDG-backup retention per Q12=D + inv-zen-187):
  list       — enumerate state dirs (sizes + ages + paths) [--json]
  cleanup    — apply retention policy [--dry-run] [--keep=ID]`,
		Example: `  zen state show
  zen state regenerate --dry-run
  zen state verify
  zen state pin substrate_min_version 0.7.1 --reason "Hermes 0.7.0 has CVE-2026-X"
  zen state history --field substrate_min_version
  zen state list
  zen state list --json
  zen state cleanup --dry-run
  zen state cleanup --keep 20260513T120000Z`,
	}
	cmd.AddCommand(newStateShowCmd())
	cmd.AddCommand(newStateRegenerateCmd())
	cmd.AddCommand(newStateVerifyCmd())
	cmd.AddCommand(newStatePinCmd())
	cmd.AddCommand(newStateHistoryCmd())
	cmd.AddCommand(newStateListCmd())
	cmd.AddCommand(newStateCleanupCmd())
	return cmd
}
