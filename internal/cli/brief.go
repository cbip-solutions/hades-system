// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewBriefCmd() *cobra.Command {
	return notImplementedCmd("brief [--range Xd]", "Daily / weekly summary across projects", 9, "Persistencia + memoria + trace + continuity")
}
