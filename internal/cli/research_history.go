// SPDX-License-Identifier: MIT
// Package cli — research_history.go.
//
// NEW release leaf: `zen research history` — query the research dispatch event
// log (GET /v1/research/history) and render a table with cache outcome.
//
// Deviation from plan-file: plan-file sketched ResearchHistoryEntry with
// ID/Type/EmittedAt/QueryHash fields. H-9 actually shipped:
//
// ResearchHistoryEntry{Query, DispatchedAt, FindingsCount, Source}
// ResearchHistory(ctx, ResearchHistoryFilter) ([]ResearchHistoryEntry, error)
//
// This file uses H-9 actuals. ResearchHistoryFilter.Filter is the event-type
// prefix filter passed to the daemon.
//
// attachPlan9ResearchSubs is exported (lowercase-first within package; cobra
// parent is the same package) so research.go can call it with one line.
package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli/format"
	"github.com/cbip-solutions/hades-system/internal/client"
	ierrors "github.com/cbip-solutions/hades-system/internal/errors"
	"github.com/spf13/cobra"
)

func attachPlan9ResearchSubs(parent *cobra.Command) {
	parent.AddCommand(researchHistoryCmd())
	for _, child := range parent.Commands() {
		if child.Name() == "cache" {
			child.AddCommand(researchCacheInvalidateCmd())
			child.AddCommand(researchCacheLsCmd())
			return
		}
	}

	panic("plan-9 phase-i i-9: research 'cache' parent missing; cannot attach invalidate/ls")
}

func researchHistoryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "history",
		Short: "Show research dispatch event history (cache hits, misses, etc.)",
		Long: `history queries the research dispatch log. Use --type to scope
to a specific cache-outcome source (e.g. cache_hit_exact, fresh_dispatch).
Use --since to limit results to recent events.`,
		Example: `  zen research history
  zen research history --type cache_hit_exact
  zen research history --since 24h --format json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := format.ValidateExclusive(cmd); err != nil {
				return err
			}
			filterStr, _ := cmd.Flags().GetString("type")
			sinceStr, _ := cmd.Flags().GetString("since")

			var sinceUnix int64
			if sinceStr != "" {
				ts, err := format.ParseSince(sinceStr)
				if err != nil {
					return ierrors.Wrap(ierrors.Code("cli.arg-validation-fail"), fmt.Errorf("--since: %w", err))
				}
				if !ts.IsZero() {
					sinceUnix = ts.Unix()
				}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			items, err := newClientFromCmd(cmd).ResearchHistory(ctx, client.ResearchHistoryFilter{
				Filter: filterStr,
				Since:  sinceUnix,
			})
			if err != nil {
				return err
			}

			cols := []format.Column{
				{Header: "QUERY", Field: func(r any) string { return r.(client.ResearchHistoryEntry).Query }},
				{Header: "SOURCE", Field: func(r any) string { return r.(client.ResearchHistoryEntry).Source }},
				{Header: "FINDINGS", Field: func(r any) string {
					return strconv.Itoa(r.(client.ResearchHistoryEntry).FindingsCount)
				}},
				{Header: "DISPATCHED", Field: func(r any) string {
					return client.FormatUnix(r.(client.ResearchHistoryEntry).DispatchedAt)
				}},
			}
			opts := format.OptionsFromFlags(cmd)
			return format.Render(cmd.OutOrStdout(), opts, items, cols)
		},
	}
	c.Flags().String("type", "", "Filter by cache outcome source (e.g. cache_hit_exact, fresh_dispatch)")
	c.Flags().String("since", "", "Lower bound for dispatched_at (e.g. 24h, 7d)")
	return c
}
