// SPDX-License-Identifier: MIT
// Package views — workforce.go (Plan 12 Phase C Task C-7, F2 panel).
//
// Workforce hierarchy — active workers, status, in-flight task IDs.
// Live data from /v1/workforce/workers (Plan 4 + 5 + 7 substrate).
package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
)

type workforceWorker struct {
	ID        string
	SpecID    string
	Status    string
	TaskID    string
	StartedAt time.Time
}

type workforceDataMsg struct {
	workers []workforceWorker
	err     error
}

type WorkforceView struct {
	c       *client.Client
	workers []workforceWorker
	lastErr error
}

func NewWorkforceView(c *client.Client) *WorkforceView {
	return &WorkforceView{c: c}
}

func (v *WorkforceView) Init() tea.Cmd { return v.Refetch() }

func (v *WorkforceView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(workforceDataMsg); ok {
		v.workers = m.workers
		v.lastErr = m.err
	}
	return v, nil
}

func (v *WorkforceView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		workers, err := v.c.WorkforceWorkers(ctx, "", 100, 0)
		if err != nil {
			return workforceDataMsg{err: err}
		}
		out := make([]workforceWorker, 0, len(workers))
		for _, w := range workers {
			out = append(out, workforceWorker{
				ID:        w.ID,
				SpecID:    w.SpecID,
				Status:    w.Status,
				TaskID:    w.TaskID,
				StartedAt: time.Unix(w.StartedAt, 0),
			})
		}
		return workforceDataMsg{workers: out}
	}
}

func (v *WorkforceView) View() string {
	header := panelHeader("WORKFORCE")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if len(v.workers) == 0 {
		return header + "\n" + mutedStyle.Render("(no active workers)")
	}
	tab := components.Table{
		Headers: []string{"WORKER", "SPEC", "STATUS", "TASK", "AGE"},
	}
	for _, w := range v.workers {
		age := time.Since(w.StartedAt).Round(time.Second).String()
		tab.Rows = append(tab.Rows, []string{
			truncateView(w.ID, 12), truncateView(w.SpecID, 12),
			w.Status, truncateView(w.TaskID, 12), age,
		})
	}
	return header + "\n" + tab.Render() + "\n  " +
		mutedStyle.Render(fmt.Sprintf("%d worker(s)", len(v.workers)))
}

func truncateView(s string, w int) string {
	if w <= 0 || len(s) <= w {
		return s
	}
	if w <= 1 {
		return string([]rune(s)[:w])
	}
	return s[:w-1] + "…"
}
