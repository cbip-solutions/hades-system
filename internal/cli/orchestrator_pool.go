// SPDX-License-Identifier: MIT
// Package cli — orchestrator_pool.go.
//
// `zen orchestrator pool {status,prune}` exposes the worktree pool
// surface from Q3 + Q4 — pool counts, leased / elastic / orphan
// snapshot, and an explicit GC trigger via prune.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func orchPoolCmd() *cobra.Command {
	pool := &cobra.Command{
		Use:   "pool",
		Short: "Worktree pool operations (status / prune)",
	}
	pool.AddCommand(orchPoolStatusCmd())
	pool.AddCommand(orchPoolPruneCmd())
	return pool
}

func orchPoolStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show pool counts (floor, max, leased, elastic, orphans cleaned)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			st, err := plan5ClientFromCmd(cmd).OrchestratorPool(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Floor: %d   Maximum: %d\n", st.Floor, st.Maximum)
			fmt.Fprintf(out, "Leased: %d   Elastic in-use: %d\n", st.CurrentLeased, st.ElasticInUse)
			fmt.Fprintf(out, "Orphans cleaned: %d\n", st.OrphansCleaned)
			fmt.Fprintf(out, "Health: %v\n", st.HealthOK)
			return nil
		},
	}
}

func orchPoolPruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Force-prune orphan worktrees (defense-in-depth GC trigger)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			n, err := plan5ClientFromCmd(cmd).OrchestratorPoolPrune(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "orphans_pruned: %d\n", n)
			return nil
		},
	}
}
