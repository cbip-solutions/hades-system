// SPDX-License-Identifier: MIT
// Package cli — caronte.go (Plan v0.20.0 Phase C Task C-3; inv-zen-273).
//
// `zen caronte` is the operator-facing Caronte code-graph engine
// subcommand group. Plan v0.20.0 Phase C ships `reindex` as the first
// leaf (closing the dangling reference in doctor_caronte.go:52); future
// phases may add `health`, `query`, `stats` etc. as direct caronte ops
// (current `zen codegraph` / `zen impact` / `zen context` / `zen wiki`
// route via the mcpgateway; the `caronte` group is for operations that
// do not need a Hermes session).
//
// Subcommand registration is in caronte_reindex.go (the only Phase C
// leaf); the group constructor is here for the canonical
// "<group>.go + <group>_<verb>.go" pattern (mirrors audit_chain.go /
// audit_chain_verify.go).
package cli

import (
	"github.com/spf13/cobra"
)

func NewCaronteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "caronte",
		Short: "Caronte code-graph engine operator surface (Plan 19; in-daemon, Apache-2.0)",
		Long: `Operator-facing entry point for the in-daemon Caronte engine
(Plan 19 sovereign code-graph). Subcommands bypass the mcpgateway and
route directly to the daemon's /v1/caronte/* endpoints — useful for
` + "`zen doctor caronte`" + ` follow-ups (e.g. operator-triggered reindex)
without a running Hermes session.

Subcommands:
  reindex     trigger initial / full reindex of a project's caronte graph`,
		Example: `  # Reindex the project anchored at cwd
  zen caronte reindex

  # Reindex an explicit alias
  zen caronte reindex <alias>

  # Reindex every registered project (sequential)
  zen caronte reindex --all`,
	}
	cmd.AddCommand(NewCaronteReindexCmdProd())
	return cmd
}
