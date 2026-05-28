// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task-level intervention (kill/retry/accept-as-is/reroll)",
	}
	cmd.AddCommand(notImplementedSubcommand("kill <task-id>", "Kill an in-flight task", 5, "Worktree + apply phase"))
	cmd.AddCommand(notImplementedSubcommand("retry <task-id>", "Retry a task (optionally with --provider)", 5, "Worktree + apply phase"))
	cmd.AddCommand(notImplementedSubcommand("accept-as-is <task-id>", "Mark task as accepted despite failures (logs decision)", 5, "Worktree + apply phase"))
	cmd.AddCommand(notImplementedSubcommand("reroll <task-id>", "Spawn fresh subagent for task with prior context", 5, "Worktree + apply phase"))
	return cmd
}
