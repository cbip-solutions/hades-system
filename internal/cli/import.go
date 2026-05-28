// SPDX-License-Identifier: MIT
package cli

import "github.com/spf13/cobra"

func NewImportCmd() *cobra.Command {
	return notImplementedCmd("import <path>", "Import daemon state from a tarball", 15, "Migration tooling + distribution tooling")
}
