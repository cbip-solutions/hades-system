// SPDX-License-Identifier: MIT
// Package cli — orchestrator_state.go.
//
// `hades orchestrator state` shows the current state-machine snapshot
// (session id, state, mode, background goroutine count, recent
// transitions). Routes via the daemon HTTP surface from
// internal/daemon/handlers/orchestrator_plan5.go.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func orchStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state",
		Short: "Show current orchestrator state-machine state + transition history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			info, err := plan5ClientFromCmd(cmd).OrchestratorState(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Session: %s\n", info.SessionID)
			fmt.Fprintf(out, "State: %s\n", info.State)
			fmt.Fprintf(out, "Mode: %s\n", info.Mode)
			fmt.Fprintf(out, "Background goroutines: %d\n", info.BackgroundGoroutines)
			if len(info.RecentTransitions) > 0 {
				fmt.Fprintln(out, "Recent transitions:")
				for _, t := range info.RecentTransitions {
					fmt.Fprintf(out, "  %s -> %s (%s)\n", t.From, t.To, t.Reason)
				}
			}
			return nil
		},
	}
}
