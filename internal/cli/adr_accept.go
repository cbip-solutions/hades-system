// SPDX-License-Identifier: MIT
// Package cli — adr_accept.go.
//
// `hades adr accept <id> --reason <X>` calls POST /v1/adr/accept.
// --reason is mandatory per invariant; cobra MarkFlagRequired enforces
// presence; RunE enforces non-empty (whitespace-only is also rejected).
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func adrAcceptCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "accept <id>",
		Short: "Mark ADR as accepted (--reason mandatory; inv-hades-146)",
		Args:  cobra.ExactArgs(1),
		Long: `accept transitions the ADR from proposed → accepted.
Emits an adr.accepted event anchored on the Plan 9 audit chain.
--reason is mandatory per inv-hades-146 (cannot be empty or whitespace-only).`,
		Example: `  hades adr accept ADR-0070 --reason "Q4 B approved in operator review"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason must not be empty (inv-hades-146)"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			if err := newClientFromCmd(cmd).ADRAccept(ctx, args[0], reason); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id=%s status=accepted\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Rationale for acceptance (mandatory; inv-hades-146)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
