// SPDX-License-Identifier: MIT
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

type Table struct {
	Headers []string
	Rows    [][]string

	ColAlign []lipgloss.Position

	Width int
}

func (t Table) Render() string {
	if len(t.Headers) == 0 || len(t.Rows) == 0 {
		return ""
	}

	colWidths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		colWidths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(colWidths) && len(cell) > colWidths[i] {
				colWidths[i] = len(cell)
			}
		}
	}

	if t.Width > 0 {
		total := 0
		for _, w := range colWidths {
			total += w + 2
		}
		if total > t.Width {

			for total > t.Width {
				maxIdx, maxW := 0, colWidths[0]
				for i, w := range colWidths {
					if w > maxW {
						maxIdx, maxW = i, w
					}
				}
				if maxW <= 4 {
					break
				}
				colWidths[maxIdx]--
				total--
			}
		}
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(palette.ColorTitle)
	cellStyle := lipgloss.NewStyle().Foreground(palette.ColorTitle)
	borderStyle := lipgloss.NewStyle().Foreground(palette.ColorBorder)
	_ = borderStyle

	var b strings.Builder
	for i, h := range t.Headers {
		align := lipgloss.Left
		if i < len(t.ColAlign) {
			align = t.ColAlign[i]
		}
		styled := headerStyle.Width(colWidths[i]).Align(align).Render(truncate(h, colWidths[i]))
		b.WriteString(styled)
		if i < len(t.Headers)-1 {
			b.WriteString("  ")
		}
	}
	b.WriteString("\n")

	for _, row := range t.Rows {
		for i, cell := range row {
			if i >= len(colWidths) {
				break
			}
			align := lipgloss.Left
			if i < len(t.ColAlign) {
				align = t.ColAlign[i]
			}
			styled := cellStyle.Width(colWidths[i]).Align(align).Render(truncate(cell, colWidths[i]))
			b.WriteString(styled)
			if i < len(row)-1 {
				b.WriteString("  ")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if len(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return s[:w-1] + "…"
}
