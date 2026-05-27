// SPDX-License-Identifier: MIT
// Package tui implements the hades-system dashboard TUI.
//
// Per spec §7.1 (k9s shape, calm-by-default) and R5 verification:
// - Use lipgloss WithStandardStyleName("dark") — never WithAutoStyle
// inside bubbletea (Glamour issue #405).
// - Hybrid render: Glamour for prose, custom lipgloss for tables /
// ASCII art (Glamour issues #109, #177, #486).
// - Strip YAML frontmatter pre-render (Glamour issue #205).
//
// Palette is spec §Q2 charcoal-monochrome-with-crimson. Color constants
// are delegated to the leaf internal/tui/palette package to avoid
// import cycles: components/ and views/ both import palette directly;
// the tui parent re-exports them here for backward compatibility with
// callers that use tui.ColorOK.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

var (
	ColorBg       = palette.ColorBg
	ColorWordmark = palette.ColorWordmark
	ColorDivider  = palette.ColorDivider

	ColorTitle  = palette.ColorTitle
	ColorMuted  = palette.ColorMuted
	ColorBorder = palette.ColorBorder
	ColorOK     = palette.ColorOK
	ColorWarn   = palette.ColorWarn
	ColorErr    = palette.ColorErr
	ColorAccent = palette.ColorAccent
	ColorRowAlt = palette.ColorRowAlt

	ColorSeverityLow  = palette.ColorSeverityLow
	ColorSeverityMid  = palette.ColorSeverityMid
	ColorSeverityHigh = palette.ColorSeverityHigh
)

var (
	TitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(ColorTitle)
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).Foreground(ColorTitle).
			BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).
			BorderForeground(ColorBorder)
	MutedStyle  = lipgloss.NewStyle().Foreground(ColorMuted)
	OKStyle     = lipgloss.NewStyle().Foreground(ColorOK)
	WarnStyle   = lipgloss.NewStyle().Foreground(ColorWarn)
	ErrStyle    = lipgloss.NewStyle().Foreground(ColorErr)
	AccentStyle = lipgloss.NewStyle().Foreground(ColorAccent)
)
