// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewSwarmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "swarm",
		Short: "Swarm-level lifecycle (pause/resume/abort)",
	}
	cmd.AddCommand(notImplementedSubcommand("pause <swarm-id>", "Pause a swarm", 5, "Worktree + apply stage"))
	cmd.AddCommand(notImplementedSubcommand("resume <swarm-id>", "Resume a paused swarm", 5, "Worktree + apply stage"))
	cmd.AddCommand(notImplementedSubcommand("abort <swarm-id>", "Abort a swarm (worktrees preserved for diagnosis)", 5, "Worktree + apply stage"))
	return cmd
}
