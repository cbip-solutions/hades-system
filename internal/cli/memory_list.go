// SPDX-License-Identifier: MIT
// Package cli — memory_list.go (Plan 14 Phase F Task F-4).
//
// `zen memory list` enumerates pinned notes from the global pin index
// (Plan 9 D aggregator). Render as text|json.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
)

type MemoryListFlags struct {
	Limit  int
	Offset int
	Format string
}

var validMemoryListFormats = map[string]bool{"text": true, "json": true}

const defaultMemoryListLimit = 25

func newMemoryListCmd() *cobra.Command {
	flags := MemoryListFlags{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pinned notes from the global pin index",
		Long: `Enumerate notes pinned to the Plan 9 D aggregator's global pin index.

Output formats:
  text  (default) — tabwriter table with note-id / PROJECT / TITLE / PROMOTED-BY / PROMOTED-AT
  json            — array of AggPinNote objects (wire-faithful)`,
		Example: `  zen memory list
  zen memory list --limit 50 --format json | jq '.[] | .note_id'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := memoryClientFactory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), memoryListTimeout)
			defer cancel()
			return RunMemoryList(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().IntVar(&flags.Limit, "limit", 0, "result limit (default 25; 0 = default)")
	cmd.Flags().IntVar(&flags.Offset, "offset", 0, "skip the first N results")
	cmd.Flags().StringVar(&flags.Format, "format", "text", "output format: text|json")
	return cmd
}

func RunMemoryList(ctx context.Context, c MemoryClient, flags MemoryListFlags, w io.Writer) error {
	format := strings.TrimSpace(flags.Format)
	if format == "" {
		format = "text"
	}
	if !validMemoryListFormats[format] {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory list: --format %q must be one of text|json", format))
	}
	limit := flags.Limit
	if limit == 0 {
		limit = defaultMemoryListLimit
	}
	if limit < 0 {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory list: --limit must be non-negative"))
	}
	if flags.Offset < 0 {
		return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), recoverable("memory list: --offset must be non-negative"))
	}

	resp, err := c.MemoryList(ctx, limit, flags.Offset)
	if err != nil {
		return ierrors.Wrap(ierrors.Code("daemon.unreachable"), fmt.Errorf("memory list: %w", err))
	}
	notes := resp.Notes

	switch format {
	case "json":

		if notes == nil {
			notes = []client.AggPinNote{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(notes)
	default:
		return writeMemoryListText(w, notes)
	}
}

func writeMemoryListText(w io.Writer, notes []client.AggPinNote) error {
	if len(notes) == 0 {
		_, err := fmt.Fprintln(w, "(no pinned notes)")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "note-id\tPROJECT\tTITLE\tBY\tAT")
	for _, n := range notes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			truncateKnowledge(n.NoteID, 40),
			truncateKnowledge(n.ProjectID, 16),
			truncateKnowledge(n.Title, 40),
			truncateKnowledge(n.PromotedBy, 12),
			formatMemoryTime(n.PromotedAt),
		)
	}
	return tw.Flush()
}

func formatMemoryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04Z")
}
