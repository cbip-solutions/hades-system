// SPDX-License-Identifier: MIT
// Package cli — docs_status.go.
//
// `hades docs status` renders the per-ecosystem corpus snapshot:
//
// ECOSYSTEM CHUNKS SYMBOLS STORAGE LAST_POLLED LAST_INDEXED
// go 1234 567 10.0 MB 2025-01-09T... 2025-01-09T...
// python 890 234 5.0 MB 2025-01-10T... 2025-01-10T...
//
// Layout via text/tabwriter so columns auto-align. Timestamps render via
// formatDocsUnixTime (RFC3339 UTC; zero -> "(never)" sentinel).
//
// Output is plain-text only; JSON / machine-readable formats land in
// alongside the daemon-side handler so wire shape and rendering
// stay co-evolved.
//
// Exit codes (per spec §6.2):
//
// 0 success
// 1 recoverable: daemon 404 / 422 (rare for a read-only GET)
// 2 unrecoverable: transport, decode, daemon 5xx
package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const docsStatusTimeout = 5 * time.Second

func NewDocsStatusCmd(factory DocsClientFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show per-ecosystem corpus chunk/symbol/storage snapshot",
		Long: `Render a table with one row per registered ecosystem: chunk
count, symbol count, storage usage, last-poll timestamp, and last-index
timestamp.

Surfaces enough information to drive operator decisions about pruning
candidates, stale sources, and retention-window adherence.`,
		Example: `  hades docs status`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), docsStatusTimeout)
			defer cancel()
			return RunDocsStatus(ctx, c, cmd.OutOrStdout())
		},
	}
	return cmd
}

func RunDocsStatus(ctx context.Context, c DocsClient, w io.Writer) error {
	resp, err := c.DocsStatus(ctx)
	if err != nil {
		return classifyDocsError(err, "status")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ECOSYSTEM\tCHUNKS\tSYMBOLS\tSTORAGE\tLAST_POLLED\tLAST_INDEXED")
	for _, eco := range resp.Ecosystems {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\t%s\n",
			eco.Ecosystem,
			eco.ChunkCount,
			eco.SymbolCount,
			formatBytes(eco.StorageBytes),
			formatDocsUnixTime(eco.LastPolled),
			formatDocsUnixTime(eco.LastIndexed),
		)
	}
	return tw.Flush()
}
