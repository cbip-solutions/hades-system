// SPDX-License-Identifier: MIT
// Package cli — safetynet_drift.go.
//
// `zen safetynet drift {run,history}` triggers and inspects the
// doctrine-drift detector.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func safetynetDriftCmd() *cobra.Command {
	drift := &cobra.Command{
		Use:   "drift",
		Short: "Doctrine-drift detector (Q2 C element 4)",
	}
	drift.AddCommand(safetynetDriftRunCmd())
	drift.AddCommand(safetynetDriftHistoryCmd())
	return drift
}

func safetynetDriftRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run drift detector now (reports doctrine violations in HEAD)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			findings, err := plan5ClientFromCmd(cmd).SafetynetDriftRun(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(findings) == 0 {
				fmt.Fprintln(out, "no drift detected")
				return nil
			}
			for _, f := range findings {
				fmt.Fprintf(out, "%s [%s] %s\n", f.CommitSHA, f.Rule, f.Description)
			}
			return nil
		},
	}
}

func safetynetDriftHistoryCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Recent drift findings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rows, err := plan5ClientFromCmd(cmd).SafetynetDriftHistory(ctx, since)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(rows) == 0 {
				fmt.Fprintln(out, "no drift findings in window")
				return nil
			}
			for _, f := range rows {
				fmt.Fprintf(out, "%s [%s] %s\n", f.CommitSHA, f.Rule, f.Description)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "24h", "lookback duration")
	return cmd
}
