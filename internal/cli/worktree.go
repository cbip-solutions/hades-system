// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Worktree management",
	}
	cmd.AddCommand(notImplementedSubcommand("list", "List active worktrees", 5, "Worktree + apply stage"))
	cmd.AddCommand(notImplementedSubcommand("clean [--older-than Nd]", "Clean up stale worktrees", 5, "Worktree + apply stage"))
	return cmd
}
