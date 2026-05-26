// SPDX-License-Identifier: MIT
package cli

import (
	"github.com/spf13/cobra"
)

func NewMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate state between schema versions or import from external systems",
		Long:  "Migrate state between schema versions or import from external systems.",
	}
	cmd.SetHelpTemplate(migrateHelpTemplate)
	cmd.SetUsageTemplate(migrateUsageTemplate)
	cmd.AddCommand(newMigrateClaudeCodeCommand())
	cmd.AddCommand(newMigratePlan18Command())

	return cmd
}

func NewMigrateCommand() *cobra.Command {
	return NewMigrateCmd()
}
