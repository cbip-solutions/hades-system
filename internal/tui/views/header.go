// SPDX-License-Identifier: MIT
// Package views — header.go.
//
// Shared per-panel header helper. releaseb's TUI brand pass prefixes
// every panel title with "HADES · " (spec §Q8 panel-header brand pass
// + spec §Q2 tagline separator convention).
//
// Single source of truth for the per-panel HADES wordmark prefix; if
// the design evolves (e.g., glyph variant, color shift), update here
// and propagation is automatic across all 12 panel View() methods.
package views

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

func panelHeader(title string) string {
	wordmark := lipgloss.NewStyle().Bold(true).Foreground(palette.ColorAccent).Render("HADES")
	sep := lipgloss.NewStyle().Foreground(palette.ColorDivider).Render(" · ")
	titleRendered := lipgloss.NewStyle().Bold(true).Foreground(palette.ColorTitle).Render(title)
	return wordmark + sep + titleRendered
}
