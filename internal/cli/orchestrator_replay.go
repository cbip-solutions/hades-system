// SPDX-License-Identifier: MIT
// Package cli — orchestrator_replay.go (Plan 5 Phase N N-2).
//
// `zen orchestrator replay <FILE>` re-runs a captured fixture against
// an in-memory orchestrator and reports any divergence (Q14 C tier).
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func orchReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "replay <FILE>",
		Short: "Replay a captured fixture and report divergence (Q14 C)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			res, err := plan5ClientFromCmd(cmd).OrchestratorReplay(ctx, client.ReplayRequest{
				InputPath: args[0],
			})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out,
				"replayed %d events; deterministic=%v; divergences=%d\n",
				res.EventsReplayed, res.Deterministic, len(res.Divergences))
			for _, d := range res.Divergences {
				fmt.Fprintf(out, "  - %s\n", d)
			}
			return nil
		},
	}
}
