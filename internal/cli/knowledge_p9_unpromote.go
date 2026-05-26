// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9_unpromote.go (Plan 9 Phase I Task I-5).
//
// `zen knowledge-p9 unpromote <note-id>` — reverse a prior promote.
//
// Mirror of promote with inverse semantics. inv-zen-146 applies equally:
//  1. cobra MarkFlagRequired("reason") — rejects absence at parse time.
//  2. strings.TrimSpace check in RunE — rejects whitespace-only values.
//
// Wire method: KnowledgeUnpromoteP9(ctx, noteID, reason) → error (204 No Content).
// The daemon emits a vault.note_unpromoted_from_global Plan 8 audit event
// anchored on the Plan 9 chain.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func knowledge9UnpromoteCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "unpromote <note-id>",
		Short: "Reverse a prior promote (operator-gated; inv-zen-146)",
		Args:  cobra.ExactArgs(1),
		Long: `unpromote removes a note from the global aggregator pin index.
Operator-gated parallel of promote; same inv-zen-146 mandatory reason.

The unpromote event is anchored on the Plan 9 chain and visible via
` + "`zen audit-chain history`" + `.`,
		Example: `  zen knowledge-p9 unpromote internal-platform-x/old-pattern \
    --reason "superseded by ADR-0072"`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason required and must be non-empty (inv-zen-146)"))
			}

			noteID := args[0]
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if err := newClientFromCmd(cmd).KnowledgeUnpromoteP9(ctx, noteID, reason); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "note_id=%s unpromoted=true reason=%q\n", noteID, reason)
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Operator rationale (required; inv-zen-146)")

	_ = cmd.MarkFlagRequired("reason")

	return cmd
}
