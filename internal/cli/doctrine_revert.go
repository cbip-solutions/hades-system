// SPDX-License-Identifier: MIT
// Package cli — doctrine_revert.go.
//
// `hades doctrine revert <ADR-NNNN>` rolls back a previously applied
// amendment via the daemon's amendment.Reverter wiring. Optional
// --reason is recorded into the eventlog (invariant operator-override
// audit) when supplied.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func doctrineRevertCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "revert <ADR-NNNN>",
		Short: "Roll back a previously applied amendment (calls amendment.Reverter)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := client.DoctrineDecision{ID: args[0], Reason: reason}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := plan5ClientFromCmd(cmd).DoctrineRevert(ctx, req); err != nil {
				return ierrors.Wrap(ierrors.Code("daemon.unreachable"), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s reverted\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reason for revert (audited if provided)")
	return cmd
}
