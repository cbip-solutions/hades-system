// SPDX-License-Identifier: MIT
// Package views — confirmations.go.
//
// Pending confirmations. The
// daemon's /v1/doctrine/propose-list returns the queue of pending
// doctrine proposals — these are the operator-facing confirmations
// the F6 panel surfaces.
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

type confirmationsDataMsg struct {
	list *client.DoctrineProposalList
	err  error
}

type ConfirmationsView struct {
	c       *client.Client
	list    *client.DoctrineProposalList
	lastErr error
}

func NewConfirmationsView(c *client.Client) *ConfirmationsView {
	return &ConfirmationsView{c: c}
}

func (v *ConfirmationsView) Init() tea.Cmd { return v.Refetch() }

func (v *ConfirmationsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(confirmationsDataMsg); ok {
		v.list = m.list
		v.lastErr = m.err
	}
	return v, nil
}

func (v *ConfirmationsView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, err := v.c.DoctrineProposeList(ctx)
		if err != nil {
			return confirmationsDataMsg{err: err}
		}
		return confirmationsDataMsg{list: list}
	}
}

func (v *ConfirmationsView) View() string {
	header := panelHeader("PENDING CONFIRMATIONS")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if v.list == nil || len(v.list.Proposals) == 0 {
		return header + "\n" + lipgloss.NewStyle().
			Foreground(palette.ColorOK).
			Render("(empty — all clear)")
	}
	tab := components.Table{
		Headers: []string{"ID", "STATUS", "AGE", "TITLE"},
	}
	for _, it := range v.list.Proposals {
		age := time.Since(time.Unix(it.ProposedAt, 0)).Round(time.Second).String()
		tab.Rows = append(tab.Rows, []string{
			truncateView(it.ID, 12), it.Status, age, truncateView(it.Title, 40),
		})
	}
	return header + "\n" + tab.Render() + "\n  " +
		mutedStyle.Render(fmt.Sprintf("%d pending  [Enter] open  [A] ack  [D] deny", len(v.list.Proposals)))
}
