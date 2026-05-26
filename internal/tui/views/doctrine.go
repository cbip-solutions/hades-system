// SPDX-License-Identifier: MIT
// Package views — doctrine.go (Plan 12 Phase C Task C-7, F10 panel).
//
// Active doctrine + amendments (proposals). Live data from
// /v1/doctrine/active + /v1/doctrine/list (Plan 8 substrate) +
// /v1/doctrine/propose-list (Plan 5 — pending amendments).
package views

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
)

type doctrineDataMsg struct {
	active     *client.DoctrineV2ActiveResp
	list       *client.DoctrineV2ListResp
	amendments *client.DoctrineProposalList
	err        error
}

type DoctrineView struct {
	c          *client.Client
	active     *client.DoctrineV2ActiveResp
	list       *client.DoctrineV2ListResp
	amendments *client.DoctrineProposalList
	lastErr    error
}

func NewDoctrineView(c *client.Client) *DoctrineView { return &DoctrineView{c: c} }

func (v *DoctrineView) Init() tea.Cmd { return v.Refetch() }

func (v *DoctrineView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(doctrineDataMsg); ok {
		v.active = m.active
		v.list = m.list
		v.amendments = m.amendments
		v.lastErr = m.err
	}
	return v, nil
}

func (v *DoctrineView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		active, err := v.c.DoctrineV2ActiveCall(ctx)
		if err != nil {
			return doctrineDataMsg{err: err}
		}
		list, _ := v.c.DoctrineV2ListCall(ctx, "")
		am, _ := v.c.DoctrineProposeList(ctx)
		return doctrineDataMsg{active: active, list: list, amendments: am}
	}
}

func (v *DoctrineView) View() string {
	header := panelHeader("DOCTRINE")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if v.active == nil {
		return header + "\n" + mutedStyle.Render("(loading…)")
	}
	body := "  active doctrine:  " + v.active.Name + "\n"
	body += "  schema version:   " + v.active.SchemaVersion + "\n"
	body += "  source:           " + v.active.Source + "\n\n"

	if v.list != nil && len(v.list.Items) > 0 {
		body += "  available doctrines:\n"
		tab := components.Table{
			Headers: []string{"NAME", "SOURCE", "VERSION"},
		}
		for _, it := range v.list.Items {
			tab.Rows = append(tab.Rows, []string{
				it.Name, it.Source, it.SchemaVersion,
			})
		}
		body += tab.Render() + "\n\n"
	}

	body += "  amendments:\n"
	if v.amendments == nil || len(v.amendments.Proposals) == 0 {
		body += mutedStyle.Render("    (none active)") + "\n"
	} else {
		tab := components.Table{
			Headers: []string{"ID", "STATUS", "TITLE"},
		}
		for _, a := range v.amendments.Proposals {
			tab.Rows = append(tab.Rows, []string{
				a.ID, a.Status, truncateView(a.Title, 60),
			})
		}
		body += tab.Render()
	}
	return header + "\n" + body
}
