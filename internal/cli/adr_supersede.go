// SPDX-License-Identifier: MIT
// Package cli — adr_supersede.go (Plan 9 Phase I Task I-7).
//
// `zen adr supersede <old-id> <new-id> --reason <X>` calls
// POST /v1/adr/supersede. --reason is mandatory per inv-zen-146.
// Two positional args are required (cobra.ExactArgs(2)).
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func adrSupersedeCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "supersede <old-id> <new-id>",
		Short: "Link old→new supersede chain (--reason mandatory; inv-zen-146)",
		Args:  cobra.ExactArgs(2),
		Long: `supersede transitions oldID from accepted → superseded and links
it to newID. Emits an adr.superseded event anchored on the Plan 9 audit
chain. Both IDs and --reason are mandatory per inv-zen-146.`,
		Example: `  zen adr supersede ADR-0042 ADR-0070 --reason "design drift post Plan 9"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason must not be empty (inv-zen-146)"))
			}
			oldID, newID := args[0], args[1]
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			if err := newClientFromCmd(cmd).ADRSupersede(ctx, oldID, newID, reason); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id=%s superseded by=%s status=superseded\n", oldID, newID)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "Rationale for supersession (mandatory; inv-zen-146)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
