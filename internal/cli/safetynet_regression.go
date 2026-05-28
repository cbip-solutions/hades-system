// SPDX-License-Identifier: MIT
// Package cli — safetynet_regression.go.
//
// `hades safetynet regression {show,query}` reads the substrate_health
// table via the daemon (design choice C element 3 — regression-by-self detector
// from the Apr 23 substrate-regression incident). The adapter at
// internal/daemon/orchestratoradapter is the persistence
// boundary; safetynet.Recent on the orchestrator side is the consumer
// surface; this CLI is the operator surface.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func safetynetRegressionCmd() *cobra.Command {
	reg := &cobra.Command{
		Use:   "regression",
		Short: "Regression-by-self metric (design choice C element 3)",
	}
	reg.AddCommand(safetynetRegressionShowCmd())
	reg.AddCommand(safetynetRegressionQueryCmd())
	return reg
}

func safetynetRegressionShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show latest regression metric per author (substrate default, 7d)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rows, err := plan5ClientFromCmd(cmd).SafetynetRegressionQuery(ctx, "substrate", "7d")
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(rows) == 0 {
				fmt.Fprintln(out, "no substrate health rows in 7d window")
				return nil
			}
			for _, r := range rows {
				sha := r.CommitSHA
				if len(sha) > 8 {
					sha = sha[:8]
				}
				fmt.Fprintf(out, "%s [%s] %.1f%% (%d/%d)\n",
					sha, r.AuthoredBy, r.TestPassRate*100, r.TestPassed, r.TestTotal)
			}
			return nil
		},
	}
}

func safetynetRegressionQueryCmd() *cobra.Command {
	var author, since string
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query regression metric by author + duration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rows, err := plan5ClientFromCmd(cmd).SafetynetRegressionQuery(ctx, author, since)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, r := range rows {
				sha := r.CommitSHA
				if len(sha) > 8 {
					sha = sha[:8]
				}
				fmt.Fprintf(out, "%s [%s] %.1f%%\n", sha, r.AuthoredBy, r.TestPassRate*100)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&author, "author", "substrate", "filter by author (substrate|operator|manual)")
	cmd.Flags().StringVar(&since, "since", "7d", "lookback duration")
	return cmd
}
