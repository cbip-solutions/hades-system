// SPDX-License-Identifier: MIT
// Package views — hra.go (Plan 12 Phase C Task C-7, F5 panel).
//
// Human-Review-Attention queue per Plan 5. The daemon does not expose
// a dedicated /v1/orchestrator/hra endpoint; the closest live data is
// /v1/orchestrator/state (Plan 5) which carries the active session's
// state-machine snapshot + recent transitions. F5 renders the
// transitions that match the HRA-attention pattern (state==hra_*) +
// degrades cleanly when no session is active.
package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
)

type hraDataMsg struct {
	session *client.SessionInfo
	err     error
}

type HRAView struct {
	c       *client.Client
	session *client.SessionInfo
	lastErr error
}

func NewHRAView(c *client.Client) *HRAView { return &HRAView{c: c} }

func (v *HRAView) Init() tea.Cmd { return v.Refetch() }

func (v *HRAView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(hraDataMsg); ok {
		v.session = m.session
		v.lastErr = m.err
	}
	return v, nil
}

func (v *HRAView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sess, err := v.c.OrchestratorState(ctx)
		if err != nil {
			return hraDataMsg{err: err}
		}
		return hraDataMsg{session: sess}
	}
}

func (v *HRAView) View() string {
	header := panelHeader("HRA QUEUE (L1-L4)")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if v.session == nil {
		return header + "\n" + mutedStyle.Render("(loading…)")
	}

	body := fmt.Sprintf("  active session:  %s\n", truncateView(v.session.SessionID, 32))
	body += fmt.Sprintf("  state:           %s\n", v.session.State)
	body += fmt.Sprintf("  mode:            %s\n\n", v.session.Mode)

	body += "  recent transitions:\n"
	if len(v.session.RecentTransitions) == 0 {
		body += mutedStyle.Render("    (none)") + "\n"
	} else {
		tab := components.Table{
			Headers: []string{"TIME", "FROM", "TO", "REASON"},
		}
		for _, tr := range v.session.RecentTransitions {
			ts := time.Unix(tr.Timestamp, 0).UTC().Format("15:04:05")
			tab.Rows = append(tab.Rows, []string{
				ts, tr.From, tr.To, truncateView(tr.Reason, 40),
			})
		}
		body += tab.Render() + "\n"
	}

	body += "\n  " + mutedStyle.Render(fmt.Sprintf("%d recent transition(s)  [Enter] inspect  [A] approve",
		len(v.session.RecentTransitions)))
	return header + "\n" + body
}
