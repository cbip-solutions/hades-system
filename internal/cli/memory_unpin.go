// SPDX-License-Identifier: MIT
// Package cli — memory_unpin.go.
//
// `hades memory unpin <note-id>` reverses a prior pin/promote by calling
// MemoryUnpin (aggregator unpromote endpoint).
//
// invariant historically applies; the daemon-side handler enforces a
// non-empty --reason. The CLI passes the operator's reason through so
// the audit chain captures the cause of removal.
package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MemoryUnpinFlags struct {
	NoteID     string
	Reason     string
	OperatorID string
}

func newMemoryUnpinCmd() *cobra.Command {
	flags := MemoryUnpinFlags{}
	cmd := &cobra.Command{
		Use:   "unpin <note-id>",
		Short: "Reverse a pin/promote — remove a note from the global pin index",
		Long:  "unpin removes a note from the global aggregator pin index. The\noperator's reason is anchored on the HADES design audit chain so the removal is\ntraceable.\n\ninvariant (daemon-side): the aggregator unpromote endpoint may reject\nempty reasons. The CLI does NOT cobra-force --reason for unpin (some\noperator workflows un-pin programmatically without rationale) — pass\n--reason explicitly when audit clarity matters.",

		Example: `  hades memory unpin internal-platform-x/M0-doctrine \
    --reason "superseded by N0-revision-2026-05"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.NoteID = args[0]
			c := memoryClientFactory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), memoryMutateTimeout)
			defer cancel()
			return RunMemoryUnpin(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Reason, "reason", "", "operator rationale (optional; surfaced in audit chain)")
	cmd.Flags().StringVar(&flags.OperatorID, "operator", "", "operator identifier (default: daemon-resolved)")
	return cmd
}

func RunMemoryUnpin(ctx context.Context, c MemoryClient, flags MemoryUnpinFlags, w io.Writer) error {
	if strings.TrimSpace(flags.NoteID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory unpin: <note-id> is required"))
	}
	if err := c.MemoryUnpin(ctx, flags.NoteID); err != nil {
		return classifyMemoryMutateError(err, "unpin")
	}
	fmt.Fprintf(w, "unpinned: note_id=%s\n", flags.NoteID)
	return nil
}
