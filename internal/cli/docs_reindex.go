// SPDX-License-Identifier: MIT
// Package cli — docs_reindex.go.
//
// `zen docs reindex` rebuilds or refreshes the ecosystem-docs corpus.
// By default the daemon performs a delta sweep (only docs that changed
// since the last successful poll). --full forces a complete reindex
// across the configured ecosystems / versions.
//
// # Flags
//
// --ecosystem string restrict to a single ecosystem (go|python|...; empty = all)
// --version string restrict to a single version (empty = current + 2 prior)
// --full force a complete reindex (default = delta-only)
//
// Output is a compact summary line with counts of ingested packages /
// chunks / symbols + elapsed wall-clock. The format is grep-able for
// CI gates; JSON output is intentionally deferred until surfaces
// progress streaming.
//
// Exit codes (per spec §6.2):
//
// 0 success
// 1 operator-recoverable: 404 (unknown ecosystem) / 422 (validation)
// 2 unrecoverable: transport, decode, daemon 5xx
package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/cbip-solutions/hades-system/internal/client"
)

const docsReindexTimeout = 10 * time.Minute

type DocsReindexFlags struct {
	Ecosystem string
	Version   string
	Full      bool
}

func NewDocsReindexCmd(factory DocsClientFactory) *cobra.Command {
	flags := DocsReindexFlags{}
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild/refresh the ecosystem docs corpus",
		Long: `Re-ingest ecosystem documentation into the daemon's RAG corpus.

By default performs a delta sweep (only docs that changed since last poll).
Use --full to force a complete reindex.`,
		Example: `  # Delta sweep across all ecosystems
  zen docs reindex

  # Full reindex of Go ecosystem only
  zen docs reindex --ecosystem go --full

  # Pin to a specific version
  zen docs reindex --ecosystem python --version 3.12.0`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := factory(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), docsReindexTimeout)
			defer cancel()
			return RunDocsReindex(ctx, c, flags, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&flags.Ecosystem, "ecosystem", "", "restrict to a single ecosystem (go|python|typescript|rust; empty = all)")
	cmd.Flags().StringVar(&flags.Version, "version", "", "restrict to a single version (empty = current + 2 prior)")
	cmd.Flags().BoolVar(&flags.Full, "full", false, "force a complete reindex (default = delta-only)")
	return cmd
}

func RunDocsReindex(ctx context.Context, c DocsClient, flags DocsReindexFlags, w io.Writer) error {
	req := client.DocsReindexRequest{
		Ecosystem: flags.Ecosystem,
		Version:   flags.Version,
		DeltaOnly: !flags.Full,
	}
	resp, err := c.DocsReindex(ctx, req)
	if err != nil {
		return classifyDocsError(err, "reindex")
	}
	fmt.Fprintf(w, "docs reindex ok: packages_ingested=%d chunks_ingested=%d symbols_registered=%d change_nodes=%d elapsed=%dms\n",
		resp.PackagesIngested, resp.ChunksIngested, resp.SymbolsRegistered,
		resp.ChangeNodesCreated, resp.ElapsedMs)
	return nil
}
