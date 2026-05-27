// SPDX-License-Identifier: MIT
// Package views — crossproject.go.
//
// Cross-project switcher. Live data from /v1/projects.
//
// The struct name `CrossProjectView` (rather than `Projects` or
// similar) was chosen to distinguish the F11 panel from the
// `client.Project` DTO it consumes; the name also documents the
// federated cross-project intent the panel represents — capa-firewall
// projects are filtered server-side per inv-hades-163, F11 renders
// whatever the daemon returns.
package views

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
)

type crossProjectDataMsg struct {
	projects []client.Project
	err      error
}

type CrossProjectView struct {
	c        *client.Client
	projects []client.Project
	lastErr  error
}

func NewCrossProjectView(c *client.Client) *CrossProjectView {
	return &CrossProjectView{c: c}
}

func (v *CrossProjectView) Init() tea.Cmd { return v.Refetch() }

func (v *CrossProjectView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(crossProjectDataMsg); ok {
		v.projects = m.projects
		v.lastErr = m.err
	}
	return v, nil
}

func (v *CrossProjectView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ps, err := v.c.ProjectsListAll(ctx)
		if err != nil {
			return crossProjectDataMsg{err: err}
		}
		return crossProjectDataMsg{projects: ps}
	}
}

func (v *CrossProjectView) View() string {
	header := panelHeader("CROSS-PROJECT SWITCHER")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if len(v.projects) == 0 {
		return header + "\n" + mutedStyle.Render("(no projects registered)")
	}
	tab := components.Table{
		Headers: []string{"ALIAS", "PATH", "STATE", "LAST ACTIVE"},
	}
	for _, p := range v.projects {
		last := ""
		if !p.LastActivatedAt.IsZero() {
			last = time.Since(p.LastActivatedAt).Round(time.Minute).String() + " ago"
		}
		tab.Rows = append(tab.Rows, []string{
			p.Alias, truncateView(p.Path, 40), p.AutonomousState, last,
		})
	}
	return header + "\n" + tab.Render() + "\n  " +
		mutedStyle.Render(fmt.Sprintf("%d project(s)  [Enter] activate", len(v.projects)))
}
