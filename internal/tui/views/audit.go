// SPDX-License-Identifier: MIT
// Package views — audit.go (Plan 12 Phase C Task C-7, F4 panel).
//
// Audit chain stream with Tessera anchoring (Plan 9). Live data from
// /v1/audit/events (recent rolling window).
package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
)

type auditDataMsg struct {
	events []client.AuditEvent
	err    error
}

type AuditView struct {
	c       *client.Client
	events  []client.AuditEvent
	lastErr error
}

func NewAuditView(c *client.Client) *AuditView { return &AuditView{c: c} }

func (v *AuditView) Init() tea.Cmd { return v.Refetch() }

func (v *AuditView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(auditDataMsg); ok {
		v.events = m.events
		v.lastErr = m.err
	}
	return v, nil
}

func (v *AuditView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		evs, err := v.c.AuditEvents(ctx, "", "", 0, 100)
		if err != nil {
			return auditDataMsg{err: err}
		}
		return auditDataMsg{events: evs}
	}
}

func (v *AuditView) View() string {
	header := panelHeader("AUDIT CHAIN (last 100 events; Tessera-anchored)")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if len(v.events) == 0 {
		return header + "\n" + mutedStyle.Render("(no events)")
	}
	tab := components.Table{
		Headers: []string{"TIMESTAMP", "TYPE", "PROJECT", "ID"},
	}
	for _, e := range v.events {
		ts := time.Unix(e.EmittedAt, 0).UTC().Format("15:04:05")
		tab.Rows = append(tab.Rows, []string{
			ts, e.Type, truncateView(e.ProjectID, 20), truncateView(e.ID, 12),
		})
	}
	return header + "\n" + tab.Render() + "\n  " +
		mutedStyle.Render(fmt.Sprintf("[Enter] inspect by ID  [J/K] scroll"))
}
