// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewTraceCmd() *cobra.Command {
	return notImplementedCmd("trace <feature>", "Replay feature execution timeline", 9, "Persistencia + memoria + trace + continuity")
}
