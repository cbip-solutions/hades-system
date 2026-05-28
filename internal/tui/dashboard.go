// SPDX-License-Identifier: MIT
// Package tui — dashboard.go.
//
// Bubbletea root model for the HADES TUI. Hosts the 12 F-key panels
// (spec §7.3) as tea.Model children; routes key + tick + data
// messages to the active panel; renders header (with Bident corner
// glyph when terminal width ≥ 50) + active panel body + footer with
// F1..F12 labels (active wrapped in brackets).
//
// Panel registry is materialised at NewModel time; per-panel cadence
// driven by panelTickMsg + scheduleTick (panels.go); per-panel data
// pulled via the panel's Refetch tea.Cmd contract.
//
// The earlier healthMsg + ViewName + ViewProjects/etc. constants
// are retired — the active-panel state machine is the canonical
// view dispatch.
package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/components"
	"github.com/cbip-solutions/hades-system/internal/tui/views"
)

type doctrineModeAppliedMsg struct {
	mode string
}

var doctrineModeFetchTimeout = 1500 * time.Millisecond

type Model struct {
	c           *client.Client
	width       int
	height      int
	pollEvery   time.Duration
	activePanel panelKey

	help          *views.HelpView
	workforce     *views.WorkforceView
	cost          *views.CostView
	audit         *views.AuditView
	hra           *views.HRAView
	confirmations *views.ConfirmationsView
	codegraph     *views.CodegraphView
	memory        *views.MemoryView
	skills        *views.SkillsView
	doctrine      *views.DoctrineView
	crossProject  *views.CrossProjectView
	inbox         *views.InboxView
}

type Options struct {
	InitialPanel string
}

func NewModel(udsPath string, pollEvery time.Duration) Model {
	m, _ := NewModelWithOptions(udsPath, pollEvery, Options{})
	return m
}

func NewModelWithOptions(udsPath string, pollEvery time.Duration, opts Options) (Model, error) {
	if pollEvery == 0 {
		pollEvery = 1 * time.Second
	}
	activePanel := panelHelp
	if opts.InitialPanel != "" {
		var ok bool
		activePanel, ok = parsePanelName(opts.InitialPanel)
		if !ok {
			return Model{}, validatePanelName(opts.InitialPanel)
		}
	}
	c := client.New(udsPath)
	return Model{
		c:             c,
		pollEvery:     pollEvery,
		activePanel:   activePanel,
		help:          views.NewHelpView(),
		workforce:     views.NewWorkforceView(c),
		cost:          views.NewCostView(c),
		audit:         views.NewAuditView(c),
		hra:           views.NewHRAView(c),
		confirmations: views.NewConfirmationsView(c),
		codegraph:     views.NewCodegraphView(c),
		memory:        views.NewMemoryView(c),
		skills:        views.NewSkillsView(c),
		doctrine:      views.NewDoctrineView(c),
		crossProject:  views.NewCrossProjectView(c),
		inbox:         views.NewInboxView(c),
	}, nil
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		scheduleTick(m.activePanel),
		m.refetchActivePanel(),
		m.fetchDoctrineMode(),
	)
}

func (m Model) fetchDoctrineMode() tea.Cmd {
	c := m.c
	return func() tea.Msg {

		mode := "capa-firewall"
		if c == nil {
			return doctrineModeAppliedMsg{mode: mode}
		}
		ctx, cancel := context.WithTimeout(context.Background(), doctrineModeFetchTimeout)
		defer cancel()
		resp, err := c.DoctrineV2ActiveCall(ctx)
		if err != nil || resp == nil || resp.Name == "" {
			return doctrineModeAppliedMsg{mode: mode}
		}

		return doctrineModeAppliedMsg{mode: resp.Name}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:

		if s := msg.String(); s == "ctrl+c" || s == "q" || s == "esc" {

			if m.activePanel != panelCodegraph || s == "ctrl+c" {
				return m, tea.Quit
			}

			return m.routeToActivePanel(msg)
		}

		if pk, ok := parseFKey(msg); ok {
			oldKey := m.activePanel
			m.activePanel = pk
			cmds := []tea.Cmd{m.refetchActivePanel()}
			if oldKey != pk {
				cmds = append(cmds, scheduleTick(pk))
			}
			return m, tea.Batch(cmds...)
		}

		return m.routeToActivePanel(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case panelTickMsg:
		if msg.key != m.activePanel {

			return m, nil
		}
		return m, tea.Batch(scheduleTick(msg.key), m.refetchActivePanel())

	case doctrineModeAppliedMsg:

		m.codegraph.SetDoctrineMode(msg.mode)
		return m, nil
	}

	return m.routeToActivePanel(msg)
}

func (m Model) routeToActivePanel(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activePanel {
	case panelHelp:
		_, cmd = m.help.Update(msg)
	case panelWorkforce:
		_, cmd = m.workforce.Update(msg)
	case panelCost:
		_, cmd = m.cost.Update(msg)
	case panelAudit:
		_, cmd = m.audit.Update(msg)
	case panelHRA:
		_, cmd = m.hra.Update(msg)
	case panelConfirmations:
		_, cmd = m.confirmations.Update(msg)
	case panelCodegraph:
		_, cmd = m.codegraph.Update(msg)
	case panelMemory:
		_, cmd = m.memory.Update(msg)
	case panelSkills:
		_, cmd = m.skills.Update(msg)
	case panelDoctrine:
		_, cmd = m.doctrine.Update(msg)
	case panelCrossProject:
		_, cmd = m.crossProject.Update(msg)
	case panelInbox:
		_, cmd = m.inbox.Update(msg)
	}
	return m, cmd
}

func (m Model) refetchActivePanel() tea.Cmd {
	switch m.activePanel {
	case panelHelp:
		return m.help.Refetch()
	case panelWorkforce:
		return m.workforce.Refetch()
	case panelCost:
		return m.cost.Refetch()
	case panelAudit:
		return m.audit.Refetch()
	case panelHRA:
		return m.hra.Refetch()
	case panelConfirmations:
		return m.confirmations.Refetch()
	case panelCodegraph:
		return m.codegraph.Refetch()
	case panelMemory:
		return m.memory.Refetch()
	case panelSkills:
		return m.skills.Refetch()
	case panelDoctrine:
		return m.doctrine.Refetch()
	case panelCrossProject:
		return m.crossProject.Refetch()
	case panelInbox:
		return m.inbox.Refetch()
	}
	return nil
}

func (m Model) View() string {
	title := HeaderStyle.Render(TitleStyle.Render("HADES dashboard — F1..F12 panels"))
	var header string
	if m.width >= 50 {
		glyph := components.BidentCornerGlyph()
		header = lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", glyph)
	} else {
		header = title
	}
	body := m.renderActivePanelBody()
	footer := MutedStyle.Render(m.renderFooter())
	return header + "\n\n" + body + "\n\n" + footer
}

func (m Model) renderActivePanelBody() string {
	switch m.activePanel {
	case panelHelp:
		return m.help.View()
	case panelWorkforce:
		return m.workforce.View()
	case panelCost:
		return m.cost.View()
	case panelAudit:
		return m.audit.View()
	case panelHRA:
		return m.hra.View()
	case panelConfirmations:
		return m.confirmations.View()
	case panelCodegraph:
		return m.codegraph.View()
	case panelMemory:
		return m.memory.View()
	case panelSkills:
		return m.skills.View()
	case panelDoctrine:
		return m.doctrine.View()
	case panelCrossProject:
		return m.crossProject.View()
	case panelInbox:
		return m.inbox.View()
	}
	return MutedStyle.Render("(no panel)")
}

func (m Model) renderFooter() string {
	labels := []struct {
		key  panelKey
		name string
	}{
		{panelHelp, "F1 help"},
		{panelWorkforce, "F2 workforce"},
		{panelCost, "F3 cost"},
		{panelAudit, "F4 audit"},
		{panelHRA, "F5 hra"},
		{panelConfirmations, "F6 confirm"},
		{panelCodegraph, "F7 codegraph"},
		{panelMemory, "F8 memory"},
		{panelSkills, "F9 skills"},
		{panelDoctrine, "F10 doctrine"},
		{panelCrossProject, "F11 xproject"},
		{panelInbox, "F12 inbox"},
	}
	var b strings.Builder
	for i, l := range labels {
		if l.key == m.activePanel {
			b.WriteString("[" + l.name + "]")
		} else {
			b.WriteString(" " + l.name + " ")
		}
		if i < len(labels)-1 {
			b.WriteString("  ")
		}
	}
	b.WriteString("    [q] quit")
	return b.String()
}

func parseFKey(m tea.KeyMsg) (panelKey, bool) {
	switch m.Type {
	case tea.KeyF1:
		return panelHelp, true
	case tea.KeyF2:
		return panelWorkforce, true
	case tea.KeyF3:
		return panelCost, true
	case tea.KeyF4:
		return panelAudit, true
	case tea.KeyF5:
		return panelHRA, true
	case tea.KeyF6:
		return panelConfirmations, true
	case tea.KeyF7:
		return panelCodegraph, true
	case tea.KeyF8:
		return panelMemory, true
	case tea.KeyF9:
		return panelSkills, true
	case tea.KeyF10:
		return panelDoctrine, true
	case tea.KeyF11:
		return panelCrossProject, true
	case tea.KeyF12:
		return panelInbox, true
	}
	switch string(m.Runes) {
	case "F1":
		return panelHelp, true
	case "F2":
		return panelWorkforce, true
	case "F3":
		return panelCost, true
	case "F4":
		return panelAudit, true
	case "F5":
		return panelHRA, true
	case "F6":
		return panelConfirmations, true
	case "F7":
		return panelCodegraph, true
	case "F8":
		return panelMemory, true
	case "F9":
		return panelSkills, true
	case "F10":
		return panelDoctrine, true
	case "F11":
		return panelCrossProject, true
	case "F12":
		return panelInbox, true
	}
	return panelHelp, false
}
