// SPDX-License-Identifier: MIT
// Package cli — memory_promote.go.
//
// `hades memory promote <note-id>` pins a note to the global aggregator pin
// index. promote is the release D canonical term; `hades memory pin` is the
// CLI-ergonomic alias for the same daemon endpoint.
//
// invariant gates (same as pin):
// 1. cobra MarkFlagRequired("reason") — rejects absence at parse time.
// 2. strings.TrimSpace check in RunE — rejects whitespace-only values.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MemoryPromoteFlags struct {
	NoteID     string
	Reason     string
	OperatorID string
}

func newMemoryPromoteCmd() *cobra.Command {
	flags := MemoryPromoteFlags{}
	cmd := &cobra.Command{
		Use:   "promote <note-id>",
		Short: "Promote a note to the global pin index (Plan 9 D canonical term)",
		Long: `promote copies a per-project note to the global aggregator pin index.
The note's frontmatter receives promoted-at, promoted-by, and reason fields.

promote and pin are aliases at the CLI surface; both call the same daemon
endpoint (POST /v1/knowledge/aggregator/promote). promote is the Plan 9 D
term; pin is the operator-ergonomics shorthand.

inv-hades-146: --reason MANDATORY. Empty or whitespace-only reasons are rejected
before any network call. The promotion event is anchored on the Plan 9 audit
chain and visible via ` + "`hades audit-chain history`" + `.`,
		Example: `  hades memory promote internal-platform-x/M0-doctrine \
    --reason "applies to all max-scope projects"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.NoteID = args[0]
			c := memoryClientFactory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), memoryMutateTimeout)
			defer cancel()
			return RunMemoryPromote(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Reason, "reason", "", "operator rationale (required; inv-hades-146)")
	cmd.Flags().StringVar(&flags.OperatorID, "operator", "", "operator identifier (default: daemon-resolved)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func RunMemoryPromote(ctx context.Context, c MemoryClient, flags MemoryPromoteFlags, w io.Writer) error {
	if strings.TrimSpace(flags.NoteID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory promote: <note-id> is required"))
	}
	if strings.TrimSpace(flags.Reason) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory promote: --reason required (inv-hades-146)"))
	}
	req := client.AggPromoteRequest{
		NoteID:     flags.NoteID,
		OperatorID: flags.OperatorID,
		Reason:     flags.Reason,
	}
	if err := c.MemoryPromote(ctx, req); err != nil {
		return classifyMemoryMutateError(err, "promote")
	}
	fmt.Fprintf(w, "promoted: note_id=%s reason=%q\n", flags.NoteID, flags.Reason)
	return nil
}
