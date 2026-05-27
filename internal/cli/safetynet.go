// SPDX-License-Identifier: MIT
// Package cli — safetynet.go.
//
// `hades safetynet` exposes the 4 self-hosting safety-net elements from
// Q2 C (Anthropic Apr 23 substrate-regression evidence):
//
// hades safetynet status # 4-element overview
// hades safetynet prev {install,show,exec <argv>} # bin/hades-prev mgmt
// hades safetynet divergence {run,history --since <d>} # config-divergence
// hades safetynet regression {show,query --author --since}
// hades safetynet drift {run,history --since <d>} # doctrine-drift
//
// Constructor lives here; nested sub-namespaces split into sibling
// files for readability (safetynet_prev.go etc.).
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func NewSafetynetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "safetynet",
		Short: "Self-hosting safety-net (Q2 C — Apr 23 substrate-regression evidence)",
	}
	attachPlan5DaemonURLFlag(cmd)
	cmd.AddCommand(safetynetStatusCmd())
	cmd.AddCommand(safetynetPrevCmd())
	cmd.AddCommand(safetynetDivergenceCmd())
	cmd.AddCommand(safetynetRegressionCmd())
	cmd.AddCommand(safetynetDriftCmd())
	return cmd
}

func safetynetStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Single-screen safety-net overview (4 elements)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := plan5ClientFromCmd(cmd).SafetynetStatusCall(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "prev binary:    installed=%v version=%s path=%s\n",
				s.PrevBinaryInstalled, s.PrevBinaryVersion, s.PrevBinaryPath)
			fmt.Fprintf(out, "divergence:     last_clean=%v last_at=%d\n",
				s.LastDivergenceClean, s.LastDivergenceAt)
			fmt.Fprintf(out, "regression-by-self (7d): substrate=%.1f%%  operator=%.1f%%\n",
				s.SubstratePassRate7d*100, s.OperatorPassRate7d*100)
			fmt.Fprintf(out, "drift incidents (24h): %d\n", s.DriftIncidents24h)
			return nil
		},
	}
}
