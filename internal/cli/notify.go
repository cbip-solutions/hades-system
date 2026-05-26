// SPDX-License-Identifier: MIT
// Package cli — notify.go (Plan 2 Phase L Task L-4).
//
// notification ledger. Plan 11 will widen this surface to multi-channel
// routing (mute, rules, history per-channel).
package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func NewNotifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Notification controls (Plan 2: bypass events; Plan 11: full router)",
	}
	cmd.AddCommand(notifyListCmd())
	cmd.AddCommand(notifyAckCmd())
	return cmd
}

func notifyListCmd() *cobra.Command {
	var unacked bool
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "List notifications, newest first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			rows, err := bypassNewClient(cmd).NotificationsList(ctx, limit, unacked)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Println("(no notifications)")
				return nil
			}
			fmt.Printf("%-5s %-9s %-20s %-30s %s\n", "ID", "SEVERITY", "WHEN", "SOURCE", "TITLE")
			for _, n := range rows {
				ack := ""
				if n.Acknowledged {
					ack = " (acked)"
				}
				fmt.Printf("%-5d %-9s %-20s %-30s %s%s\n",
					n.ID, n.Severity,
					n.TS.Format("2006-01-02 15:04:05"),
					n.Source, n.Title, ack)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&unacked, "unacked", false, "show only unacknowledged")
	c.Flags().IntVar(&limit, "limit", 50, "max rows")
	return c
}

func notifyAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack <id>",
		Short: "Acknowledge a notification (stops 1h CRITICAL repeat)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("id must be integer: %w", err))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := bypassNewClient(cmd).NotificationsDismiss(ctx, id); err != nil {
				return err
			}
			fmt.Printf("acked: %d\n", id)
			return nil
		},
	}
}
