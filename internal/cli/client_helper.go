// SPDX-License-Identifier: MIT
// Package cli — client_helper.go (Plan 4 Phase O).
//
// bypassNewClientWithURL allows tests to inject a custom daemon URL
// without touching the real UDS socket path. Production callers pass
// url="" to fall back to the Unix socket dial set by --uds.
package cli

import (
	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func bypassNewClientWithURL(cmd *cobra.Command, url string) *client.Client {
	if url != "" {
		return client.NewWithBaseURL(url)
	}
	return bypassNewClient(cmd)
}
