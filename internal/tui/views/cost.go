// SPDX-License-Identifier: MIT
// Package views — cost.go (Plan 12 Phase C Task C-7, F3 panel).
//
// 4-axis cost dashboard + augmentation cache stats. Live data from
// /v1/budget?range=24h (Plan 3+4) + /v1/augment/summary (Plan 11) via
// the AugmentCache helper.
package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type costDataMsg struct {
	totalUSD   float64
	byTier     []client.BudgetTierSpend
	augHitRate float64
	augQueries int64
	augBytes   int64
	err        error
}

type CostView struct {
	c          *client.Client
	totalUSD   float64
	byTier     []client.BudgetTierSpend
	augHitRate float64
	augQueries int64
	augBytes   int64
	lastErr    error
}

func NewCostView(c *client.Client) *CostView {
	return &CostView{c: c}
}

func (v *CostView) Init() tea.Cmd { return v.Refetch() }

func (v *CostView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(costDataMsg); ok {
		v.totalUSD = m.totalUSD
		v.byTier = m.byTier
		v.augHitRate = m.augHitRate
		v.augQueries = m.augQueries
		v.augBytes = m.augBytes
		v.lastErr = m.err
	}
	return v, nil
}

func (v *CostView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		summary, err := v.c.BudgetSummaryRollup(ctx, "24h")
		if err != nil {
			return costDataMsg{err: err}
		}
		var augHit float64
		var augQ int64
		var augBytes int64
		augResp, augErr := v.c.AugmentCache(ctx)
		if augErr == nil && augResp != nil {
			augHit = augResp.HitRate
			augQ = augResp.TotalQueries
			augBytes = augResp.BytesCached
		}
		return costDataMsg{
			totalUSD:   summary.TotalUSD,
			byTier:     summary.ByTier,
			augHitRate: augHit,
			augQueries: augQ,
			augBytes:   augBytes,
		}
	}
}

func (v *CostView) View() string {
	header := panelHeader("COST DASHBOARD (24h)")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	body := fmt.Sprintf("  total spend (24h):   $%.2f\n", v.totalUSD)
	body += "  by tier:\n"
	if len(v.byTier) == 0 {
		body += mutedStyle.Render("    (no spend recorded)") + "\n"
	} else {
		for _, t := range v.byTier {
			body += fmt.Sprintf("    %-20s  %-12s  $%.4f\n",
				truncateView(t.Project+"/"+t.Profile, 20), t.Tier, t.SpendUSD)
		}
	}
	body += "\n  augmentation cache:\n"
	body += fmt.Sprintf("    hit rate:          %.0f%%\n", v.augHitRate*100)
	body += fmt.Sprintf("    queries (24h):     %d\n", v.augQueries)
	body += fmt.Sprintf("    cached on disk:    %s\n", humanBytes(v.augBytes))
	return header + "\n" + body
}

func humanBytes(n int64) string {
	const KB, MB, GB = 1024, 1024 * 1024, 1024 * 1024 * 1024
	switch {
	case n >= GB:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
