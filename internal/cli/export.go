// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewExportCmd() *cobra.Command {
	return notImplementedCmd("export <path>", "Export daemon state to a tarball", 15, "Migration tooling + distribution tooling")
}
