// SPDX-License-Identifier: MIT
// Package views — memory.go (Plan 12 Phase C Task C-7, F8 panel).
//
// Memory inspector — knowledge index entries (recent + per-type
// counts) via /v1/knowledge/stats.
package views

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type memoryDataMsg struct {
	stats *client.KnowledgeStatsResponse
	err   error
}

type MemoryView struct {
	c       *client.Client
	stats   *client.KnowledgeStatsResponse
	lastErr error
}

func NewMemoryView(c *client.Client) *MemoryView { return &MemoryView{c: c} }

func (v *MemoryView) Init() tea.Cmd { return v.Refetch() }

func (v *MemoryView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(memoryDataMsg); ok {
		v.stats = m.stats
		v.lastErr = m.err
	}
	return v, nil
}

func (v *MemoryView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stats, err := v.c.KnowledgeStats(ctx)
		if err != nil {
			return memoryDataMsg{err: err}
		}
		return memoryDataMsg{stats: stats}
	}
}

func (v *MemoryView) View() string {
	header := panelHeader("MEMORY INSPECTOR")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if v.stats == nil {
		return header + "\n" + mutedStyle.Render("(loading…)")
	}
	body := fmt.Sprintf("  total docs:    %d\n", v.stats.TotalDocs)
	body += "  by type:\n"

	keys := make([]string, 0, len(v.stats.ByType))
	for k := range v.stats.ByType {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		body += fmt.Sprintf("    %-30s  %d\n", k, v.stats.ByType[k])
	}
	if v.stats.LastIndexedUnix > 0 {
		body += fmt.Sprintf("\n  last indexed:  %s\n",
			time.Unix(v.stats.LastIndexedUnix, 0).UTC().Format(time.RFC3339))
	}
	return header + "\n" + body
}
