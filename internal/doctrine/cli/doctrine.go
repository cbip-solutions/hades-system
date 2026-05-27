// SPDX-License-Identifier: MIT
// Package cli — doctrine.go.
//
// `zen doctrine` (registered as `zen doctrine-v2` during + K parity
// window; quality promotes to `zen doctrine`) exposes 15 subcommands
// organized via cobra.Group annotations:
//
// Read group → list, show, status, history, diff, validate
// Write group → init, migrate, override edit, reload
// Amendment group → propose-list, ack, deny, revert ( populates;
// registers group only)
// Debug group → reinforce
//
// Q14 C: flat invocation pattern — `zen doctrine show max-scope`, NOT
// `zen doctrine read show max-scope`. cobra.Group annotation organizes
// `--help` into visual groups WITHOUT introducing intermediate command
// nodes.
//
// Q15 A: `migrate` operator-explicit write-back; daemon never auto-writes
// .
//
// Q12 C: `reinforce` previews template render output for operator inspection
// of what worker subprocess will receive.
//
// Help text + error messages in español per project instructions operator language §6.6.
//
// invariant: this package imports zero internal/store; all stateful reads
// route via internal/client (typed HTTP daemon client) which itself respects
// invariant.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
)

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "doctrine",
		Short:         "Gestión de la doctrina activa (lectura, escritura, enmiendas, depuración)",
		Long:          rootLongHelp,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	registerGroups(root)
	format.AttachFlags(root)

	root.AddCommand(newListCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newHistoryCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newValidateCmd())

	root.AddCommand(newInitCmd())
	root.AddCommand(newMigrateCmd())
	root.AddCommand(newOverrideCmd())
	root.AddCommand(newReloadCmd())

	root.AddCommand(newReinforceCmd())

	root.AddCommand(newProposeListCmd())
	root.AddCommand(newAckCmd())
	root.AddCommand(newDenyCmd())
	root.AddCommand(newRevertCmd())
	root.AddCommand(newProposeCmd())
	return root
}

const rootLongHelp = `Gestiona el sistema de doctrina del daemon zen-swarm.

La doctrina es un conjunto declarativo de límites (TOML) que rige cómo el
daemon coordina workers, presupuestos, revisiones, fusiones y políticas
operativas para cada proyecto. Los comandos están agrupados visualmente
para facilitar la consulta de --help; la invocación es siempre plana
(p.ej. ` + "`zen doctrine show max-scope`" + `), no anidada por grupo.

Grupos:
  Lectura    — list, show, status, history, diff, validate
  Escritura  — init, migrate, override edit, reload
  Enmienda   — propose-list, ack, deny, revert
  Depuración — reinforce

Use ` + "`zen doctrine <comando> --help`" + ` para ver opciones de cada uno.`

func registerGroups(root *cobra.Command) {
	root.AddGroup(
		&cobra.Group{ID: "read", Title: "Comandos de lectura:"},
		&cobra.Group{ID: "write", Title: "Comandos de escritura:"},
		&cobra.Group{ID: "amendment", Title: "Comandos de enmienda (Plan 5 + Plan 8 Phase K):"},
		&cobra.Group{ID: "debug", Title: "Comandos de depuración:"},
	)
}

var TestOnlyClientFactory func() *Client

func clientFromCmd(cmd *cobra.Command) *Client {
	if TestOnlyClientFactory != nil {
		return TestOnlyClientFactory()
	}
	uds := "/tmp/zen-swarm.sock"
	if root := cmd.Root(); root != nil {
		if v, _ := root.PersistentFlags().GetString("uds"); v != "" {
			uds = v
		}
	}
	return NewClient("http://unix").withUDS(uds)
}

func newListCmd() *cobra.Command   { return listCmd() }
func newShowCmd() *cobra.Command   { return showCmd() }
func newStatusCmd() *cobra.Command { return statusCmd() }

func newHistoryCmd() *cobra.Command  { return historyCmd() }
func newDiffCmd() *cobra.Command     { return diffCmd() }
func newValidateCmd() *cobra.Command { return validateCmd() }

func newInitCmd() *cobra.Command     { return initCmd() }
func newMigrateCmd() *cobra.Command  { return migrateCmd() }
func newOverrideCmd() *cobra.Command { return overrideCmd() }
func newReloadCmd() *cobra.Command   { return reloadCmd() }

func newReinforceCmd() *cobra.Command { return reinforceCmd() }

func newProposeListCmd() *cobra.Command { return proposeListCmd() }
func newAckCmd() *cobra.Command         { return ackCmd() }
func newDenyCmd() *cobra.Command        { return denyCmd() }
func newRevertCmd() *cobra.Command      { return revertCmd() }
func newProposeCmd() *cobra.Command     { return proposeCmd() }
