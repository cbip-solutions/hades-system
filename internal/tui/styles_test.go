package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPaletteSpecQ2(t *testing.T) {
	cases := []struct {
		name string
		got  lipgloss.Color
		want string
	}{

		{"ColorBg", ColorBg, "#0d0d0d"},
		{"ColorWordmark", ColorWordmark, "#e0e0e0"},
		{"ColorAccent", ColorAccent, "#c41e3a"},
		{"ColorMuted", ColorMuted, "#999999"},
		{"ColorOK", ColorOK, "#10b981"},
		{"ColorWarn", ColorWarn, "#ffa726"},
		{"ColorDivider", ColorDivider, "#555555"},

		{"ColorTitle", ColorTitle, "#e0e0e0"},
		{"ColorBorder", ColorBorder, "#555555"},
		{"ColorErr", ColorErr, "#ef4444"},
		{"ColorRowAlt", ColorRowAlt, "#1a1a1a"},

		{"ColorSeverityLow", ColorSeverityLow, "#10b981"},
		{"ColorSeverityMid", ColorSeverityMid, "#ffa726"},
		{"ColorSeverityHigh", ColorSeverityHigh, "#ef4444"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Errorf("%s = %q, want %q (spec §Q2)", tc.name, string(tc.got), tc.want)
		}
	}
}
