// SPDX-License-Identifier: MIT
// Package views — codegraph.go.
//
// F7 Code Graph panel per design contract
// structural insight for the operator's currently-edited file:
//
// - symbols defined in current file
// - callers (last-30d touch count)
// - graph community ID + community-summary excerpt
// - recent churn (commits last 7d)
// - blast radius score
// - last KG index timestamp
//
// Key bindings (task):
//
// [Q] cypher-style free-text query
// [I] pre-merge impact preview
// [W] open community wiki for current file
// [C] cross-project cone
//
// Live data wiring routes via internal/client typed methods (Task
// C-4) — never net/http directly (invariant).
//
// C-1 ships skeleton, C-2 adds full layout per design contract,
// C-3 adds key handling + sub-panel state machine.
package views

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/tui/palette"
)

type CodegraphView struct {
	c           *client.Client
	currentFile string

	symbols   []symbolEntry
	callers   []callerEntry
	community communityInfo
	churn     churnInfo
	blastRad  float64
	lastIndex string

	coreness      int
	sccID         int
	cyclic        bool
	coChangePeers []coChangeEntry

	lastErr error

	subPanel      subPanelMode
	subPanelInput string
	subPanelData  string
	doctrineMode  string

	contractFederation *ContractFederationView
}

type symbolEntry struct {
	Name string
	Kind string
	Line int
}

type callerEntry struct {
	File     string
	Symbol   string
	Count30d int
}

type communityInfo struct {
	ID      string
	Summary string
}

type churnInfo struct {
	Commits7d int
	Authors   []string
}

type coChangeEntry struct {
	Path            string
	CouplingPercent float64
}

type codegraphDataMsg struct {
	symbols   []symbolEntry
	callers   []callerEntry
	community communityInfo
	churn     churnInfo
	blastRad  float64
	lastIndex string

	coreness      int
	sccID         int
	cyclic        bool
	coChangePeers []coChangeEntry
	err           error
}

func NewCodegraphView(c *client.Client) *CodegraphView {

	var cfClient ContractFederationClient
	if c != nil {
		cfClient = newProductionContractFederationClient(c)
	}
	return &CodegraphView{
		c:                  c,
		doctrineMode:       "capa-firewall",
		contractFederation: NewContractFederationView(cfClient),
	}
}

func (v *CodegraphView) SetCurrentFile(path string) {
	v.currentFile = path
}

func (v *CodegraphView) Init() tea.Cmd { return nil }

func (v *CodegraphView) View() string {
	headerBase := panelHeader("CODE GRAPH")
	if v.currentFile == "" {
		return headerBase + "\n" + mutedStyle.Render(
			"(no file selected — open a file in your editor or press [Q] to query)")
	}
	header := headerBase + " — " + v.currentFile

	if v.lastErr != nil {

		return header + "\n" + errStyle.Render(
			"  ✗ "+v.lastErr.Error()+"  (panel will retry on next tick)")
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")

	b.WriteString("  symbols       :  ")
	if len(v.symbols) == 0 {
		b.WriteString(mutedStyle.Render("(empty — file has no exported symbols)"))
	} else {
		parts := make([]string, 0, len(v.symbols))
		for _, s := range v.symbols {
			parts = append(parts, formatSymbol(s))
		}
		b.WriteString(strings.Join(parts, "  "))
	}
	b.WriteString("\n")

	b.WriteString("  callers (30d) :  ")
	if len(v.callers) == 0 {
		b.WriteString(mutedStyle.Render("(no callers in last 30d)"))
	} else {
		parts := make([]string, 0, len(v.callers))
		for _, c := range v.callers {
			parts = append(parts, fmt.Sprintf("%s (×%d)", c.File, c.Count30d))
		}
		b.WriteString(strings.Join(parts, "  "))
	}
	b.WriteString("\n")

	b.WriteString("  community     :  ")
	if v.community.ID == "" {
		b.WriteString(mutedStyle.Render("(file not yet partitioned into a graph community)"))
	} else {
		b.WriteString(fmt.Sprintf("%s (%s) — see [W] for wiki", v.community.ID, v.community.Summary))
	}
	b.WriteString("\n")

	b.WriteString("  recent churn  :  ")
	if v.churn.Commits7d == 0 {
		b.WriteString(mutedStyle.Render("(0 commits last 7d)"))
	} else {
		authors := strings.Join(v.churn.Authors, ", ")
		b.WriteString(fmt.Sprintf("%d commits last 7d (authors: %s)", v.churn.Commits7d, authors))
	}
	b.WriteString("\n")

	b.WriteString("  blast radius  :  ")
	severity, severityStyle := blastRadiusSeverity(v.blastRad)
	b.WriteString(severityStyle.Render(fmt.Sprintf("%.2f (%s)", v.blastRad, severity)))
	b.WriteString("\n")

	b.WriteString("  coreness      :  ")
	if v.coreness == 0 && v.sccID == 0 {
		b.WriteString(mutedStyle.Render("(not yet resolved)"))
	} else {
		cyc := ""
		if v.cyclic {
			cyc = " — cyclic (mutual-call SCC)"
		}
		b.WriteString(fmt.Sprintf("%d (k-core)   SCC #%d%s", v.coreness, v.sccID, cyc))
	}
	b.WriteString("\n")

	b.WriteString("  co-change     :  ")
	if len(v.coChangePeers) == 0 {
		b.WriteString(mutedStyle.Render("(no co-change peers — insufficient history or isolated)"))
	} else {
		parts := make([]string, 0, len(v.coChangePeers))
		for _, p := range v.coChangePeers {
			parts = append(parts, fmt.Sprintf("%s (%.0f%%)", p.Path, p.CouplingPercent))
		}
		b.WriteString(strings.Join(parts, "  "))
	}
	b.WriteString("\n")

	b.WriteString("  last indexed  :  ")
	if v.lastIndex == "" {
		b.WriteString(mutedStyle.Render("(never)"))
	} else {
		b.WriteString(v.lastIndex)
	}
	b.WriteString("\n\n")

	b.WriteString(mutedStyle.Render("  [Q] query    [I] impact    [W] wiki    [C] cross-project    [F] federation"))

	if v.subPanel != subPanelNone {
		b.WriteString("\n\n")
		switch v.subPanel {
		case subPanelQuery:
			b.WriteString(lipgloss.NewStyle().Bold(true).Render("QUERY:"))
			b.WriteString("\n  > " + v.subPanelInput + "_")
			if v.subPanelData != "" {
				b.WriteString("\n\n" + v.subPanelData)
			}
		case subPanelImpact:
			b.WriteString(lipgloss.NewStyle().Bold(true).Render("IMPACT PREVIEW:"))
			b.WriteString("\n" + v.subPanelData)
		case subPanelWiki:
			b.WriteString(lipgloss.NewStyle().Bold(true).Render("COMMUNITY WIKI:"))
			b.WriteString("\n" + v.subPanelData)
		case subPanelCrossProject:
			b.WriteString(lipgloss.NewStyle().Bold(true).Render("CROSS-PROJECT HITS:"))
			b.WriteString("\n" + v.subPanelData)
		case subPanelContractFederation:

			if v.contractFederation != nil {
				b.WriteString(v.contractFederation.View())
			}
		}
		b.WriteString("\n  " + mutedStyle.Render("[Esc] close subpanel"))
	} else if v.subPanelData != "" {

		b.WriteString("\n\n  " + mutedStyle.Render(v.subPanelData))
	}

	return b.String()
}

func formatSymbol(s symbolEntry) string {
	if s.Kind == "func" || s.Kind == "method" {
		return s.Name + "(...)"
	}
	return s.Name
}

func blastRadiusSeverity(score float64) (string, lipgloss.Style) {
	switch {
	case score < 0.4:
		return "low", lipgloss.NewStyle().Foreground(palette.ColorSeverityLow)
	case score < 0.7:
		return "medium", lipgloss.NewStyle().Foreground(palette.ColorSeverityMid)
	default:
		return "high", lipgloss.NewStyle().Foreground(palette.ColorSeverityHigh)
	}
}

var (
	mutedStyle = lipgloss.NewStyle().Foreground(palette.ColorMuted)
	errStyle   = lipgloss.NewStyle().Foreground(palette.ColorErr)
)
