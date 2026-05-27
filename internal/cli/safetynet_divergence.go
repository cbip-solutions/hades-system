// SPDX-License-Identifier: MIT
// Package cli — safetynet_divergence.go.
//
// `hades safetynet divergence {run,history}` triggers and inspects the
// config-divergence audit (Q2 C element 2). Run is on-demand (POST);
// history pulls recent reports from the daemon's persistence.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func safetynetDivergenceCmd() *cobra.Command {
	div := &cobra.Command{
		Use:   "divergence",
		Short: "Config-divergence audit (Q2 C element 2)",
	}
	div.AddCommand(safetynetDivergenceRunCmd())
	div.AddCommand(safetynetDivergenceHistoryCmd())
	return div
}

func safetynetDivergenceRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run divergence audit now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			rep, err := plan5ClientFromCmd(cmd).SafetynetDivergenceRun(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if rep.Clean {
				fmt.Fprintln(out, "clean (0 differences)")
				return nil
			}
			fmt.Fprintf(out, "DIRTY: %d differences\n", len(rep.Differences))
			for _, d := range rep.Differences {
				fmt.Fprintf(out, "  - %s\n", d)
			}
			return nil
		},
	}
}

func safetynetDivergenceHistoryCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Recent divergence reports",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rows, err := plan5ClientFromCmd(cmd).SafetynetDivergenceHistory(ctx, since)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(rows) == 0 {
				fmt.Fprintln(out, "no divergence reports in window")
				return nil
			}
			for _, r := range rows {
				fmt.Fprintf(out, "%d clean=%v diffs=%d\n", r.RanAt, r.Clean, len(r.Differences))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h", "lookback duration")
	return cmd
}
