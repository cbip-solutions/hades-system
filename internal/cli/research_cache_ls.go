// SPDX-License-Identifier: MIT
// Package cli — research_cache_ls.go.
//
// NEW leaf: `zen research cache ls` — browse cache entries with
// filters (--source URL prefix, --project). Distinct's
// `cache list` which is project-agnostic and pagination-oriented.
//
// Deviation from plan-file: plan-file sketched client.ResearchCacheLsEntry
// and client.ResearchCacheLs(). H-9 actually shipped:
//
// ResearchCacheEntryP9{Hash, BytesSize, CreatedAt, TTLUnix, SourceURL, ContentHash}
// ResearchCacheListP9(ctx, projectID, sourcePrefix string) ([]ResearchCacheEntryP9, error)
//
// Renders a table with HASH (truncated), SOURCE_URL, PROJECT, BYTES, EXPIRES.
// The route called is GET /v1/research/cache/list but
// with query params; the ls command is the CLI-side alias per spec §6.5.
package cli

import (
	"context"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func researchCacheLsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "ls",
		Short: "Browse cache entries (Plan 9 filters: --source, --project)",
		Long: `ls returns cache entries with Plan 9-specific filtering by source
URL prefix or project. Distinct from Plan 4's cache list (which uses
limit/offset pagination); ls uses Plan 9 semantic filters.

Route: GET /v1/research/cache/list with project_id and source query params.`,
		Example: `  zen research cache ls
  zen research cache ls --project zen-swarm
  zen research cache ls --source https://arxiv.org/
  zen research cache ls --project zen-swarm --source https://arxiv.org/ --format json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			source, _ := cmd.Flags().GetString("source")

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).ResearchCacheListP9(ctx, project, source)
			if err != nil {
				return err
			}

			cols := []format.Column{
				{Header: "HASH", Field: func(r any) string { return shortHash(r.(client.ResearchCacheEntryP9).Hash) }},
				{Header: "SOURCE_URL", Field: func(r any) string { return r.(client.ResearchCacheEntryP9).SourceURL }},
				{Header: "BYTES", Field: func(r any) string {
					return strconv.FormatInt(r.(client.ResearchCacheEntryP9).BytesSize, 10)
				}},
				{Header: "CREATED", Field: func(r any) string {
					return client.FormatUnix(r.(client.ResearchCacheEntryP9).CreatedAt)
				}},
				{Header: "EXPIRES", Field: func(r any) string {
					return client.FormatUnix(r.(client.ResearchCacheEntryP9).TTLUnix)
				}},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	c.Flags().String("project", "", "Filter by project ID")
	c.Flags().String("source", "", "Filter by source URL prefix")
	return c
}
