// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewRescueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rescue",
		Short: "Diagnostic + recovery utilities",
	}
	cmd.AddCommand(notImplementedSubcommand("[--swarm <id>] [--task <id>]", "Full system diagnostic or scoped", 11, "Notifications + error handling"))
	return cmd
}
