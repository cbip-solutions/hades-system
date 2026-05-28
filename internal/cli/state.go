// SPDX-License-Identifier: MIT
// Package cli — state.go.
//
// `hades state` is the operator surface for the persistence state model.
//
// show — render system-state.toml manifest table
// regenerate — rebuild manifest from authoritative sources
// verify — regenerate-and-diff CI gate
// pin — set a manual field with --reason
// history — walk HADES design chain showing manual field changes
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
		Short: "Persistence state surface (HADES design manifest + HADES design XDG retention)",
		Long:  "hades state — persistence state model surface.\n\nHADES design leaves (system-state.toml manifest, spec §6.1 design choice E):\n  show       — render manifest table (TomlContent + metadata)\n  regenerate — rebuild manifest from authoritative sources (--dry-run)\n  verify     — regenerate-and-diff CI gate (non-zero exit on drift;\n               integrated with make verify-invariants)\n  pin        — set a manual field with --reason (invariant)\n  history    — walk HADES design chain showing manual field changes\n\nHADES design stage leaves (XDG-backup retention per design choice + invariant):\n  list       — enumerate state dirs (sizes + ages + paths) [--json]\n  cleanup    — apply retention policy [--dry-run] [--keep=ID]",

		Example: `  hades state show
  hades state regenerate --dry-run
  hades state verify
  hades state pin substrate_min_version 0.7.1 --reason "Hermes 0.7.0 has CVE-2026-X"
  hades state history --field substrate_min_version
  hades state list
  hades state list --json
  hades state cleanup --dry-run
  hades state cleanup --keep 20260513T120000Z`,
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
