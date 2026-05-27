// SPDX-License-Identifier: MIT
// Package cli — specs.go.
//
// `hades specs <subcommand>` is the operator-facing entry point for the
// read-only OpenSpec management surface (spec §0.2). Specs are read-only
// at the CLI in release; write-back is deferred to post-v0.14.0.
//
// Four leaves under one root:
//
// hades specs list [--format text|json]
// hades specs show <spec-id> [--format text|md]
// hades specs diff <change-id> [--v <from>..<to>]
// hades specs sync [--full] [--specs-dir <path>]
//
// list / show / diff are pure filesystem reads of openspec/specs/ +
// openspec/changes/<id>/deltas/ — no daemon call.
//
// sync calls POST /v1/knowledge/ecosystem/specs-sync to re-index specs
// into ecosystem.db (the daemon handler is wired in ; calling
// against a daemon without the route returns ErrRecoverable with a
// roadmap pointer).
//
// Path resolution: list/show resolve specs/ via specsDirResolver (cwd +
// `openspec/specs/` by default; `--root` flag overrides the cwd). diff
// uses specsChangesDirResolver (cwd + `openspec/changes/`).
package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type specsDirResolver func(cmd *cobra.Command) string

type specsChangesDirResolver func(cmd *cobra.Command) string

type SpecsDaemonClientFactory func(cmd *cobra.Command) SpecsDaemonClient

func NewSpecsCmd(factory interface{}) *cobra.Command {
	root := &cobra.Command{
		Use:   "specs",
		Short: "OpenSpec read-only management (list, show, diff, sync)",
		Long: `Operator-facing entry point for OpenSpec contracts (spec §0.2).

Four subcommands:
  list   walk openspec/specs/ and print each spec's ID + title
  show   render openspec/specs/<id>.md to stdout
  diff   render openspec/changes/<change-id>/deltas/ (the pending diff)
  sync   re-index openspec/specs/ into ecosystem.db (calls daemon)

Specs are read-only at this CLI surface in Plan 14. Write-back is
deferred to post-v0.14.0 per spec §0.2.`,
		Example: `  hades specs list
  hades specs show adr-0001
  hades specs diff hades-system-bootstrap
  hades specs sync --full`,
	}

	root.PersistentFlags().String("root", "", "Project root override (defaults to cwd)")

	specsDirFn := func(cmd *cobra.Command) string {
		return resolveSpecsSubdir(cmd, "specs")
	}
	changesDirFn := func(cmd *cobra.Command) string {
		return resolveSpecsSubdir(cmd, "changes")
	}

	root.AddCommand(newSpecsListCmd(specsDirFn))
	root.AddCommand(newSpecsShowCmd(specsDirFn))
	root.AddCommand(newSpecsDiffCmd(changesDirFn))

	syncFactory, _ := factory.(SpecsDaemonClientFactory)
	if syncFactory == nil {
		syncFactory = func(_ *cobra.Command) SpecsDaemonClient {

			return nil
		}
	}
	root.AddCommand(newSpecsSyncCmd(syncFactory))

	return root
}

func NewSpecsCmdProd() *cobra.Command {
	return NewSpecsCmd(SpecsDaemonClientFactory(func(cmd *cobra.Command) SpecsDaemonClient {
		return &productionSpecsDaemonClient{c: newClientFromCmd(cmd)}
	}))
}

func resolveSpecsSubdir(cmd *cobra.Command, sub string) string {

	root, _ := cmd.Flags().GetString("root")
	if root == "" && cmd.Parent() != nil {

		root, _ = cmd.Parent().Flags().GetString("root")
		if root == "" {
			root, _ = cmd.Parent().PersistentFlags().GetString("root")
		}
	}
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return filepath.Join(".", "openspec", sub)
		}
		root = cwd
	}
	return filepath.Join(root, "openspec", sub)
}
