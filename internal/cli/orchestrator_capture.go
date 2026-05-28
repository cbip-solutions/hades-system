// SPDX-License-Identifier: MIT
// Package cli — orchestrator_capture.go.
//
// `hades orchestrator capture` writes a deterministic JSONL of one
// session's events for offline replay (consumed by replay
// tier + future debugging). Both --session-id and --output are
// required; the daemon performs the actual streaming write.
package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func orchCaptureCmd() *cobra.Command {
	var sessionID, output string
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Capture a session's events to a JSONL fixture (stage replay)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" || output == "" {
				return errors.New("--session-id and --output are required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			res, err := plan5ClientFromCmd(cmd).OrchestratorCapture(ctx, client.CaptureRequest{
				SessionID:  sessionID,
				OutputPath: output,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"captured %d events -> %s (%d bytes)\n",
				res.EventCount, res.OutputPath, res.BytesWritten)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session-id", "", "session id to capture (required)")
	cmd.Flags().StringVar(&output, "output", "", "output JSONL path (required)")
	return cmd
}
