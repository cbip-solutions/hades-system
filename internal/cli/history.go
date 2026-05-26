// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewHistoryCmd() *cobra.Command {
	return notImplementedCmd("history [--project X --range Yd]", "Aggregate history view", 9, "Persistencia + memoria + trace + continuity")
}
