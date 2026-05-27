// SPDX-License-Identifier: MIT
// Package cli — knowledge_p9_promote.go.
//
// `zen knowledge-p9 promote <note-id>` — operator-gated pin to global index.
//
// invariant: --reason is MANDATORY. Two gates:
// 1. cobra MarkFlagRequired("reason") — rejects absence at parse time.
// 2. strings.TrimSpace check in RunE — rejects whitespace-only values.
//
// Wire method: KnowledgePromoteP9(ctx, noteID, reason) → error (204 No Content).
// The daemon emits a vault.note_promoted_to_global release audit event anchored
// on the release chain; the CLI does not observe the event directly.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

func knowledge9PromoteCmd() *cobra.Command {
	var reason string
	var project string

	cmd := &cobra.Command{
		Use:   "promote <note-id>",
		Short: "Operator-gated promote to global pin index (Q6 C; inv-zen-146)",
		Args:  cobra.ExactArgs(1),
		Long: `promote copies a per-project note to the global aggregator pin index.
The note's frontmatter receives promoted-at, promoted-by, and reason fields.

inv-zen-146: --reason MANDATORY. Empty or whitespace-only reasons are rejected
before any network call. The promotion event is anchored on the Plan 9 chain
and visible via ` + "`zen audit-chain history`" + `.`,
		Example: `  zen knowledge-p9 promote internal-platform-x/M0-pattern-vault-format \
    --reason "applies to all max-scope projects"`,
		RunE: func(cmd *cobra.Command, args []string) error {

			if strings.TrimSpace(reason) == "" {
				return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--reason required and must be non-empty (inv-zen-146)"))
			}

			noteID := args[0]
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			if err := newClientFromCmd(cmd).KnowledgePromoteProjectP9(ctx, noteID, project, reason); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "note_id=%s promoted=true reason=%q\n", noteID, reason)
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Operator rationale (required; inv-zen-146)")
	cmd.Flags().StringVar(&project, "project", "", "Source project ID when note_id is not globally unique")

	_ = cmd.MarkFlagRequired("reason")

	return cmd
}
