// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewPostmortemCmd() *cobra.Command {
	return notImplementedCmd("postmortem <swarm-id>", "Generate structured postmortem", 11, "Notifications + error handling")
}
