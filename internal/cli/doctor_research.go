// SPDX-License-Identifier: MIT
// Package cli — doctor_research.go (Plan 4 Phase N Task N-8).
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/cbip-solutions/hades-system/internal/client"
)

func doctorResearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "research",
		Short: "Research cache + sources health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOneSection(cmd, "Research (Plan 4)", runResearchChecks)
		},
	}
}

func runResearchChecks(ctx context.Context, c *client.Client) []CheckResult {
	checks := []func(context.Context, *client.Client) CheckResult{
		checkResearchCacheReachable,
		checkResearchCacheSize,
		checkResearchExpiredCount,
	}
	out := make([]CheckResult, 0, len(checks))
	for _, fn := range checks {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out = append(out, fn(cctx, c))
		cancel()
	}
	return out
}

func checkResearchCacheReachable(ctx context.Context, c *client.Client) CheckResult {
	if _, err := c.ResearchCacheStatsCall(ctx); err != nil {
		return CheckResult{Name: "research.cache.reachable", Status: "fail", Detail: err.Error(),
			Hint: "daemon /v1/research/cache/stats unreachable"}
	}
	return CheckResult{Name: "research.cache.reachable", Status: "ok"}
}

func checkResearchCacheSize(ctx context.Context, c *client.Client) CheckResult {
	stats, err := c.ResearchCacheStatsCall(ctx)
	if err != nil {
		return CheckResult{Name: "research.cache.size", Status: "fail", Detail: err.Error()}
	}
	const mb = 1024 * 1024
	if stats.TotalBytes > 500*mb {
		return CheckResult{Name: "research.cache.size", Status: "warn",
			Detail: fmt.Sprintf("%d bytes (%d MiB) > 500 MiB", stats.TotalBytes, stats.TotalBytes/mb),
			Hint:   "consider zen research cache clear --older-than=7d"}
	}
	return CheckResult{Name: "research.cache.size", Status: "ok",
		Detail: fmt.Sprintf("%d entries / %d MiB", stats.TotalEntries, stats.TotalBytes/mb)}
}

func checkResearchExpiredCount(ctx context.Context, c *client.Client) CheckResult {
	stats, err := c.ResearchCacheStatsCall(ctx)
	if err != nil {
		return CheckResult{Name: "research.cache.expired", Status: "fail", Detail: err.Error()}
	}
	if stats.TotalEntries > 0 && stats.ExpiredCount*2 > stats.TotalEntries {
		return CheckResult{Name: "research.cache.expired", Status: "warn",
			Detail: fmt.Sprintf("%d/%d entries expired (>50%%)", stats.ExpiredCount, stats.TotalEntries),
			Hint:   "evictor not running? check daemon logs"}
	}
	return CheckResult{Name: "research.cache.expired", Status: "ok",
		Detail: fmt.Sprintf("%d expired (eviction goroutine handles)", stats.ExpiredCount)}
}
