// SPDX-License-Identifier: MIT
// Package cli — adr_history.go.
//
// `zen adr history <id>` calls GET /v1/adr/history?id=... and renders
// the transition log for one ADR. Wire type: []client.ADRTransition.
package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func adrHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "history <id>",
		Short:   "Show transition log for one ADR",
		Args:    cobra.ExactArgs(1),
		Example: `  zen adr history ADR-0042`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).ADRHistory(ctx, args[0])
			if err != nil {
				return err
			}
			renderAdrHistory(cmd, items)
			return nil
		},
	}
}

func renderAdrHistory(cmd *cobra.Command, items []client.ADRTransition) {
	out := cmd.OutOrStdout()
	if len(items) == 0 {
		fmt.Fprintln(out, "(no transitions)")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tREASON\tAT")
	for _, t := range items {
		at := client.FormatUnix(t.At)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.ID, t.Status, t.Reason, at)
	}
	_ = tw.Flush()
}
