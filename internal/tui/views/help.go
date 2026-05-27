// SPDX-License-Identifier: MIT
// Package views — help.go.
//
// Static help panel — F1..F12 panel inventory + key bindings.
package views

import (
	tea "github.com/charmbracelet/bubbletea"
)

type HelpView struct{}

func NewHelpView() *HelpView { return &HelpView{} }

func (v *HelpView) Init() tea.Cmd { return nil }

func (v *HelpView) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return v, nil }

func (v *HelpView) Refetch() tea.Cmd { return nil }

func (v *HelpView) View() string {
	header := panelHeader("HELP")
	body := mutedStyle.Render(`
  F1   help            this screen
  F2   workforce       active workers + worker hierarchy
  F3   cost            4-axis cost dashboard + augmentation
  F4   audit           Tessera-anchored event chain stream
  F5   hra             human-review-attention queue (L1-L4)
  F6   confirmations   pending confirmations
  F7   codegraph       caronte code structure (current file)
  F8   memory          memory inspector (per-project + global)
  F9   skills          Hermes Curator + hades skills browser
  F10  doctrine        active doctrine + amendments
  F11  xproject        cross-project switcher
  F12  inbox           notifications inbox

  [q] quit   [Esc] close subpanel`)
	return header + "\n" + body
}
