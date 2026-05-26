// SPDX-License-Identifier: MIT
// Package views — inbox.go (Plan 12 Phase C Task C-7, F12 panel).
//
// Notifications inbox (Plan 7 + Hermes routing). Live data from
// /v1/inbox/list.
package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

type inboxDataMsg struct {
	rows []client.InboxCacheRow
	err  error
}

type InboxView struct {
	c       *client.Client
	rows    []client.InboxCacheRow
	lastErr error
}

func NewInboxView(c *client.Client) *InboxView { return &InboxView{c: c} }

func (v *InboxView) Init() tea.Cmd { return v.Refetch() }

func (v *InboxView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(inboxDataMsg); ok {
		v.rows = m.rows
		v.lastErr = m.err
	}
	return v, nil
}

func (v *InboxView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rows, err := v.c.InboxList(ctx, client.InboxListRequest{Limit: 100})
		if err != nil {
			return inboxDataMsg{err: err}
		}
		return inboxDataMsg{rows: rows}
	}
}

func (v *InboxView) View() string {
	header := panelHeader("NOTIFICATIONS INBOX")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if len(v.rows) == 0 {
		return header + "\n" + lipgloss.NewStyle().
			Foreground(palette.ColorOK).
			Render("(empty — no unread notifications)")
	}
	tab := components.Table{
		Headers: []string{"WHEN", "SEVERITY", "PROJECT", "EVENT", "ACK"},
	}
	unread := 0
	for _, r := range v.rows {
		ackMark := ""
		if r.AckedAt != nil {
			ackMark = "✓"
		} else {
			unread++
		}
		tab.Rows = append(tab.Rows, []string{
			r.CreatedAt.Format("15:04:05"),
			r.Severity,
			truncateView(r.ProjectAlias, 20),
			truncateView(r.EventType, 30),
			ackMark,
		})
	}
	return header + "\n" + tab.Render() + "\n  " +
		mutedStyle.Render(fmt.Sprintf("%d unread of %d  [A] ack selected  [R] refresh",
			unread, len(v.rows)))
}
