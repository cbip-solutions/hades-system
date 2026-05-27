// SPDX-License-Identifier: MIT
// Package cli — plan5_client.go.
//
// Centralised client builder sub-commands. All CLI
// constructors call plan5ClientFromCmd(cmd) to obtain a *client.Client
// bound to either the production UDS path (--uds persistent flag) or
// a hidden --daemon-url test seam.
//
// The seam is registered once per cobra root via attachPlan5Flags so
// every namespace inherits the same flag without each constructor
// re-declaring it. Production callers leave the flag empty.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

const plan5DaemonURLFlag = "daemon-url"

func attachPlan5DaemonURLFlag(cmd *cobra.Command) {
	if cmd.PersistentFlags().Lookup(plan5DaemonURLFlag) != nil {
		return
	}
	cmd.PersistentFlags().String(plan5DaemonURLFlag, "",
		"override daemon URL (test seam; production uses --uds)")
	_ = cmd.PersistentFlags().MarkHidden(plan5DaemonURLFlag)
}

func plan5ClientFromCmd(cmd *cobra.Command) *client.Client {
	for c := cmd; c != nil; c = c.Parent() {
		f := c.PersistentFlags().Lookup(plan5DaemonURLFlag)
		if f == nil {
			f = c.Flags().Lookup(plan5DaemonURLFlag)
		}
		if f != nil && f.Value.String() != "" {
			return client.NewWithBaseURL(f.Value.String())
		}
	}
	return bypassNewClient(cmd)
}

func plan5BaseURLFromCmd(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		f := c.PersistentFlags().Lookup(plan5DaemonURLFlag)
		if f == nil {
			f = c.Flags().Lookup(plan5DaemonURLFlag)
		}
		if f != nil && f.Value.String() != "" {
			return f.Value.String()
		}
	}
	uds, _ := cmd.Flags().GetString("uds")
	if uds != "" {
		return "http+unix://" + uds
	}
	return ""
}
