// SPDX-License-Identifier: MIT
// Package cli — adr_reject.go.
//
// `hades adr reject <id> --reason <X>` calls POST /v1/adr/reject.
// --reason is mandatory per invariant (mirrors adr_accept.go pattern).
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func adrRejectCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Mark ADR as rejected (--reason mandatory; invariant)",
		Args:  cobra.ExactArgs(1),
		Long:  "reject transitions the ADR from proposed → rejected.\nEmits an adr.rejected event anchored on the HADES design audit chain.\n--reason is mandatory per invariant (cannot be empty or whitespace-only).",

		Example: `  hades adr reject ADR-0070 --reason "superseded by simpler design"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason must not be empty (invariant)"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			if err := newClientFromCmd(cmd).ADRReject(ctx, args[0], reason); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id=%s status=rejected\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Rationale for rejection (mandatory; invariant)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
