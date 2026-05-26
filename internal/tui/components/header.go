// SPDX-License-Identifier: MIT
// Package components provides reusable TUI primitives for the dashboard.
//
// Per R5 verification, table and ascii_diagram are CUSTOM-built (not
// Glamour) because Glamour has known issues with code-in-tables (#109,
// #177), table width (#486), and frontmatter (#205). prose.go wraps
// Glamour for the cases where it works well.
//
// All color literals delegate to the shared palette constants in
// internal/tui/palette per spec §Q2 charcoal-monochrome-with-crimson.
// The HADES corner glyph (BidentCornerGlyph) lives in this file.
package components

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

func HeaderRender(title, statusLine string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(palette.ColorTitle)
	border := lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).BorderForeground(palette.ColorBorder)
	return border.Render(titleStyle.Render(title) + "\n" + lipgloss.NewStyle().
		Foreground(palette.ColorMuted).Render(statusLine))
}

const bidentSmallAsset = "⡇ ⢸\n⠹⡆⡸\n ⠿⠇"

func BidentCornerGlyph() string {
	style := lipgloss.NewStyle().Foreground(palette.ColorAccent)
	return style.Render(bidentSmallAsset)
}
