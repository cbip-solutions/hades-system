// SPDX-License-Identifier: MIT
// Package palette declares the HADES TUI color constants.
//
// This package is a LEAF — it has NO imports from internal/tui or any
// sub-package. Both the parent internal/tui package AND
// internal/tui/components/* AND internal/tui/views/* import this
// package to get the shared Color* constants. This architecture avoids
// the import cycle that would arise if components/* imported the parent
// tui package (cycle: tui → views → components → tui).
//
// The leaf palette/ sub-package is the cycle-free canonical home for
// all Color* constants.
//
// Constant NAMES are stable (e.g., ColorOK, ColorTitle, ColorBorder)
// and their VALUES bind to spec §design choice charcoal-monochrome-with-crimson.
package palette

import "github.com/charmbracelet/lipgloss"

var (
	ColorBg       = lipgloss.Color("#0d0d0d")
	ColorWordmark = lipgloss.Color("#e0e0e0")
	ColorDivider  = lipgloss.Color("#555555")

	ColorTitle  = lipgloss.Color("#e0e0e0")
	ColorMuted  = lipgloss.Color("#999999")
	ColorBorder = lipgloss.Color("#555555")
	ColorOK     = lipgloss.Color("#10b981")
	ColorWarn   = lipgloss.Color("#ffa726")
	ColorErr    = lipgloss.Color("#ef4444")
	ColorAccent = lipgloss.Color("#c41e3a")
	ColorRowAlt = lipgloss.Color("#1a1a1a")

	ColorSeverityLow  = lipgloss.Color("#10b981")
	ColorSeverityMid  = lipgloss.Color("#ffa726")
	ColorSeverityHigh = lipgloss.Color("#ef4444")
)
