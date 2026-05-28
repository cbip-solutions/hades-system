// SPDX-License-Identifier: MIT
// Package cli — caronte.go.
//
// `hades caronte` is the operator-facing Caronte code-graph engine
// subcommand group. Plan v0.20.0 ships `reindex` as the first
// leaf (closing the dangling reference in doctor_caronte.go:52); future
// phases may add `health`, `query`, `stats` etc. as direct caronte ops
// (current `hades codegraph` / `hades impact` / `hades context` / `hades wiki`
// route via the mcpgateway; the `caronte` group is for operations that
// do not need a Hermes session).
//
// Subcommand registration is in caronte_reindex.go (the only
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
		Short: "Caronte code-graph engine operator surface (HADES design; in-daemon, Apache-2.0)",
		Long: "Operator-facing entry point for the in-daemon Caronte engine\n(HADES design sovereign code-graph). Subcommands bypass the mcpgateway and\nroute directly to the daemon's /v1/caronte/* endpoints — useful for\n" +
			"`hades doctor caronte`" + ` follow-ups (e.g. operator-triggered reindex)
without a running Hermes session.

Subcommands:
  reindex     trigger initial / full reindex of a project's caronte graph`,
		Example: " # Reindex the project anchored at cwd\n  hades caronte reindex\n\n # Reindex an explicit alias\n  hades caronte reindex <alias>\n\n # Reindex every registered project (sequential)\n  hades caronte reindex --all",
	}
	cmd.AddCommand(NewCaronteReindexCmdProd())
	return cmd
}
