// SPDX-License-Identifier: MIT
// Package cli — docs_sources.go (Plan 14 Phase F Task F-6).
//
// `zen docs sources --list` renders the per-source corpus health table:
//
//	NAME         ECOSYSTEM  TYPE      URL                    TTL  LAST_INDEXED      STATUS
//	pkg.go.dev   go         registry  https://pkg.go.dev/    24h  2025-01-09T13...  ok
//	pypi         python     registry  https://pypi.org/      24h  2025-01-08T22...  stale
//
// Why an explicit --list flag: keeps the bare `zen docs sources`
// invocation safely no-op-ish (prints a usage hint) so a typo doesn't
// trigger a potentially large API call. The flag also leaves room for
// future subcommands like `zen docs sources add/remove` without
// reshuffling the verb semantics.
//
// Status values are daemon-supplied strings: "ok" (last index within
// TTL), "stale" (older than TTL; next poll refreshes), "error" (last
// poll failed; details in audit-events).
//
// Exit codes (per spec §6.2):
//
//	0  success (table render OR hint print)
//	1  recoverable: daemon 404 / 422
//	2  unrecoverable: transport, decode, daemon 5xx
package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const docsSourcesTimeout = 5 * time.Second

type DocsSourcesFlags struct {
	List bool
}

func NewDocsSourcesCmd(factory DocsClientFactory) *cobra.Command {
	flags := DocsSourcesFlags{}
	cmd := &cobra.Command{
		Use:   "sources",
		Short: "List registered docs sources with TTL + status",
		Long: `Show every registered docs source with its URL, TTL, last-indexed
timestamp, and status (ok | stale | error).

Requires --list for explicit invocation; bare 'zen docs sources' prints
a usage hint so a typo doesn't trigger an unintended API call.`,
		Example: `  zen docs sources --list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), docsSourcesTimeout)
			defer cancel()
			return RunDocsSources(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&flags.List, "list", false, "render the per-source table (required; bare invocation prints a hint)")
	return cmd
}

func RunDocsSources(ctx context.Context, c DocsClient, flags DocsSourcesFlags, w io.Writer) error {
	if !flags.List {
		fmt.Fprintln(w, "(use --list to see registered docs sources)")
		return nil
	}
	resp, err := c.DocsSources(ctx)
	if err != nil {
		return classifyDocsError(err, "sources")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tECOSYSTEM\tTYPE\tURL\tTTL\tLAST_INDEXED\tSTATUS")
	for _, s := range resp.Sources {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%dh\t%s\t%s\n",
			s.Name,
			s.Ecosystem,
			s.SourceType,
			s.URL,
			s.TTLHours,
			formatDocsUnixTime(s.LastIndexed),
			s.Status,
		)
	}
	return tw.Flush()
}
