// SPDX-License-Identifier: MIT
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func NewStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Snapshot text-only daemon and project status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			status, err := c.RuntimeStatus(ctx)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.not-running"), err)
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "daemon: %s (version %s, uptime %ds, pid %d)\n",
				status.Daemon.Status,
				status.Daemon.Version,
				status.Daemon.UptimeSeconds,
				status.Daemon.PID,
			)
			fmt.Fprintf(cmd.OutOrStdout(), "socket: %s\n", status.Config.SocketPath)
			fmt.Fprintf(cmd.OutOrStdout(), "project: %s\n", valueOrUnknown(status.Project.CWD))
			fmt.Fprintf(cmd.OutOrStdout(), "provider: %s (%d configured)\n",
				valueOrUnknown(status.Provider.ActiveModel),
				status.Provider.ProviderCount,
			)
			fmt.Fprintf(cmd.OutOrStdout(), "cascade: tier %d %s (%d providers)\n",
				status.Cascade.ActiveTier,
				valueOrUnknown(status.Cascade.TierName),
				status.Cascade.ProviderCount,
			)
			fmt.Fprintf(cmd.OutOrStdout(), "cost: $%.2f / 24h", status.Cost.Spend24hUSD)
			if status.Cost.SpendSessionUSD != nil {
				fmt.Fprintf(cmd.OutOrStdout(), " ($%.2f session)", *status.Cost.SpendSessionUSD)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			if status.Context != nil && status.Context.MaxTokens > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "context: %d / %d tokens\n",
					status.Context.UsedTokens,
					status.Context.MaxTokens,
				)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: %s (%s)\n",
				valueOrUnknown(status.Profile.ProfileName),
				valueOrUnknown(status.Profile.Kind),
			)
			if status.Caronte != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "caronte: %s%s\n",
					valueOrUnknown(status.Caronte.Status),
					detailSuffix(status.Caronte.Detail),
				)
			}
			if status.Federation != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "federation: %s%s\n",
					valueOrUnknown(status.Federation.Status),
					detailSuffix(status.Federation.Detail),
				)
			}
			for _, action := range status.NextActions {
				fmt.Fprintf(cmd.OutOrStdout(), "next: %s\n", action)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON status")
	return cmd
}

func valueOrUnknown(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " - " + detail
}
