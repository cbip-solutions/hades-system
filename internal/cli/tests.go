// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewTestsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tests",
		Short: "Test analytics",
	}
	cmd.AddCommand(notImplementedSubcommand("flaky [--project X]", "List flaky tests by frequency", 9, "Persistencia + memoria + trace + continuity"))
	cmd.AddCommand(notImplementedSubcommand("coverage <feature>", "Coverage delta vs baseline", 9, "Persistencia + memoria + trace + continuity"))
	return cmd
}
