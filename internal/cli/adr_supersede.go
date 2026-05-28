// SPDX-License-Identifier: MIT
// Package cli — adr_supersede.go.
//
// `hades adr supersede <old-id> <new-id> --reason <X>` calls
// POST /v1/adr/supersede. --reason is mandatory per invariant.
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
		Short: "Link old→new supersede chain (--reason mandatory; invariant)",
		Args:  cobra.ExactArgs(2),
		Long:  "supersede transitions oldID from accepted → superseded and links\nit to newID. Emits an adr.superseded event anchored on the HADES design audit\nchain. Both IDs and --reason are mandatory per invariant.",

		Example: "  hades adr supersede ADR-0042 ADR-0070 --reason \"design drift post HADES design\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason must not be empty (invariant)"))
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
	cmd.Flags().StringVar(&reason, "reason", "", "Rationale for supersession (mandatory; invariant)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
