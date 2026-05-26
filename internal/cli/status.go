// SPDX-License-Identifier: MIT
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Snapshot text-only daemon and project status",
		RunE: func(cmd *cobra.Command, args []string) error {
			udsPath, _ := cmd.Flags().GetString("uds")
			c := client.New(udsPath)
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			h, err := c.Health(ctx)
			if err != nil {
				fmt.Println("daemon: down")
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			fmt.Printf("daemon: up (version %s, uptime %ds)\n", h.Version, h.UptimeSeconds)

			return nil
		},
	}
}
