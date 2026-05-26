// SPDX-License-Identifier: MIT
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

type ASCIIDiagram struct {
	Lines []string
}

func (d ASCIIDiagram) Render() string {
	style := lipgloss.NewStyle().Foreground(palette.ColorMuted)
	return style.Render(strings.Join(d.Lines, "\n"))
}

func IsBoxDrawing(s string) bool {
	for _, r := range s {
		if (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F) {
			return true
		}
	}
	return false
}
