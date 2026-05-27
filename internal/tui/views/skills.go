// SPDX-License-Identifier: MIT
// Package views — skills.go.
//
// Skills browser surfaced via Hermes probe. The skills registry lives
// in the Hermes plugin (ADR-0080) which the daemon probes via
// /v1/hermes/probe?check=skills. + may add a richer
// /v1/hermes/skills endpoint; this panel renders whatever the probe
// reports today + degrades cleanly when Hermes is offline.
package views

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

type skillsDataMsg struct {
	probe *client.HermesProbeResp
	err   error
}

type SkillsView struct {
	c       *client.Client
	probe   *client.HermesProbeResp
	lastErr error
}

func NewSkillsView(c *client.Client) *SkillsView { return &SkillsView{c: c} }

func (v *SkillsView) Init() tea.Cmd { return v.Refetch() }

func (v *SkillsView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(skillsDataMsg); ok {
		v.probe = m.probe
		v.lastErr = m.err
	}
	return v, nil
}

func (v *SkillsView) Refetch() tea.Cmd {
	if v.c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		probe, err := v.c.HermesProbe(ctx, "skills")
		if err != nil {
			return skillsDataMsg{err: err}
		}
		return skillsDataMsg{probe: probe}
	}
}

func (v *SkillsView) View() string {
	header := panelHeader("SKILLS BROWSER")
	if v.lastErr != nil {
		return header + "\n" + errStyle.Render("  ✗ "+v.lastErr.Error())
	}
	if v.probe == nil {
		return header + "\n" + mutedStyle.Render("(loading Hermes probe…)")
	}
	statusStyle := mutedStyle
	switch v.probe.Status {
	case "ok":
		statusStyle = lipgloss.NewStyle().Foreground(palette.ColorOK)
	case "warn":
		statusStyle = lipgloss.NewStyle().Foreground(palette.ColorWarn)
	case "fail":
		statusStyle = errStyle
	}
	body := "  hermes status:  " + statusStyle.Render(v.probe.Status) + "\n"
	if v.probe.Detail != "" {
		body += "  detail:         " + v.probe.Detail + "\n"
	}
	body += "\n  " + mutedStyle.Render("(skill registry surfaced via Hermes plugin per ADR-0080)")
	return header + "\n" + body
}
