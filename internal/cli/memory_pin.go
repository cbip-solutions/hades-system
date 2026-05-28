// SPDX-License-Identifier: MIT
// Package cli — memory_pin.go.
//
// `hades memory pin` is the operator-ergonomics alias for `hades memory promote`
// at the CLI surface. Both call MemoryPromote (aggregator promote endpoint).
//
// invariant gates apply identically:
// 1. cobra MarkFlagRequired("reason") — rejects absence at parse time.
// 2. strings.TrimSpace check in RunE — rejects whitespace-only values.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MemoryPinFlags struct {
	NoteID     string
	Reason     string
	OperatorID string
}

func newMemoryPinCmd() *cobra.Command {
	flags := MemoryPinFlags{}
	cmd := &cobra.Command{
		Use:   "pin <note-id>",
		Short: "Pin a note to the global pin index (alias for promote)",
		Long: `pin records that a note is operator-flagged as cross-project relevant
and copies it to the global aggregator pin index. It is the CLI-ergonomic
alias for ` + "`hades memory promote`" + " — both call the same daemon endpoint.\n\ninvariant: --reason MANDATORY. Empty or whitespace-only reasons are rejected\nbefore any network call. The pin event is anchored on the HADES design audit chain.",

		Example: `  hades memory pin internal-platform-x/M0-doctrine \
    --reason "load-bearing for max-scope across all projects"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.NoteID = args[0]
			c := memoryClientFactory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), memoryMutateTimeout)
			defer cancel()
			return RunMemoryPin(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Reason, "reason", "", "operator rationale (required; invariant)")
	cmd.Flags().StringVar(&flags.OperatorID, "operator", "", "operator identifier (default: daemon-resolved)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func RunMemoryPin(ctx context.Context, c MemoryClient, flags MemoryPinFlags, w io.Writer) error {
	if strings.TrimSpace(flags.NoteID) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory pin: <note-id> is required"))
	}
	if strings.TrimSpace(flags.Reason) == "" {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory pin: --reason required (invariant)"))
	}
	req := client.AggPromoteRequest{
		NoteID:     flags.NoteID,
		OperatorID: flags.OperatorID,
		Reason:     flags.Reason,
	}
	if err := c.MemoryPromote(ctx, req); err != nil {
		return classifyMemoryMutateError(err, "pin")
	}
	fmt.Fprintf(w, "pinned: note_id=%s reason=%q\n", flags.NoteID, flags.Reason)
	return nil
}

func classifyMemoryMutateError(err error, op string) error {
	if err == nil {
		return nil
	}
	if client.IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverableWrap(err, fmt.Sprintf("memory %s: daemon rejected input", op)))
	}
	return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("memory %s: %w", op, err))
}
