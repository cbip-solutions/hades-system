// SPDX-License-Identifier: MIT
// Package cli — research_cache_invalidate.go.
//
// NEW leaf: `zen research cache invalidate <query>` — operator
// force-stale. Marks cache entries matching the query string as stale so the
// next dispatch repopulates from source (T9 mitigation).
//
// Deviation from plan-file: plan-file sketched ResearchCacheInvalidate
// returning a ResearchCacheInvalidateResp struct with Query/Invalidated/
// EmittedAt fields. H-9 actually shipped:
//
// ResearchCacheInvalidate(ctx, query string) (int, error)
//
// Returns only the count of invalidated entries. This file uses H-9 actuals.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func researchCacheInvalidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "invalidate <query>",
		Short: "Force-stale cache entries matching query (T9 mitigation)",
		Long: `invalidate marks cache entries matching <query> as stale; the
next research dispatch repopulates from source. Use when upstream
sources have changed but TTL has not yet elapsed.`,
		Example: `  zen research cache invalidate "audit chain integrity"`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			n, err := newClientFromCmd(cmd).ResearchCacheInvalidate(ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Invalidated %d cache entries for query %q\n", n, args[0])
			return nil
		},
	}
}
