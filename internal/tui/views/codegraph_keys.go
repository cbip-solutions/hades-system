// SPDX-License-Identifier: MIT
// Package views — codegraph_keys.go.
//
// Sub-panel state machine + Update key handlers for the F7 Code
// Graph panel. Split from codegraph.go to keep render + key paths
// readable independently. Both files compile as one package.
//
// Privacy / invariant: the [C] cross-project key checks
// doctrineMode; "capa-firewall" disables the federated query
// (daemon-side enforcement double-anchored at retrieval boundary).
package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type subPanelMode int

const (
	subPanelNone subPanelMode = iota
	subPanelQuery
	subPanelImpact
	subPanelWiki
	subPanelCrossProject
	subPanelContractFederation
)

type codegraphSubPanelMsg struct {
	mode subPanelMode
	body string
	err  error
}

func (v *CodegraphView) SetDoctrineMode(mode string) {
	v.doctrineMode = mode
}

func (v *CodegraphView) DoctrineMode() string {
	return v.doctrineMode
}

func (v *CodegraphView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		return v.handleKey(m)
	case codegraphDataMsg:
		v.symbols = m.symbols
		v.callers = m.callers
		v.community = m.community
		v.churn = m.churn
		v.blastRad = m.blastRad
		v.lastIndex = m.lastIndex
		v.coreness = m.coreness
		v.sccID = m.sccID
		v.cyclic = m.cyclic
		v.coChangePeers = m.coChangePeers
		v.lastErr = m.err
		return v, nil
	case codegraphSubPanelMsg:
		v.subPanelData = m.body
		if m.err != nil {
			v.lastErr = m.err
		}
		return v, nil
	case contractFederationDataMsg:

		if v.contractFederation != nil {
			_, cmd := v.contractFederation.Update(m)
			return v, cmd
		}
		return v, nil
	}
	return v, nil
}

func (v *CodegraphView) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Type == tea.KeyEsc {
		v.subPanel = subPanelNone
		v.subPanelInput = ""
		v.subPanelData = ""
		return v, nil
	}

	if v.subPanel == subPanelQuery {

		switch m.Type {
		case tea.KeyEnter:
			query := v.subPanelInput
			v.subPanelInput = ""
			return v, v.dispatchQuery(query)
		case tea.KeyBackspace:
			if len(v.subPanelInput) > 0 {
				v.subPanelInput = v.subPanelInput[:len(v.subPanelInput)-1]
			}
			return v, nil
		case tea.KeyRunes:
			v.subPanelInput += string(m.Runes)
			return v, nil
		}
		return v, nil
	}

	switch strings.ToLower(string(m.Runes)) {
	case "q":
		v.subPanel = subPanelQuery
		v.subPanelInput = ""
		v.subPanelData = ""
		return v, nil
	case "i":
		if v.currentFile == "" {
			return v, nil
		}
		v.subPanel = subPanelImpact
		v.subPanelData = "(loading impact preview…)"
		return v, v.dispatchImpact()
	case "w":
		if v.community.ID == "" {
			return v, nil
		}
		v.subPanel = subPanelWiki
		v.subPanelData = "(loading community wiki…)"
		return v, v.dispatchWiki()
	case "c":
		if v.doctrineMode == "capa-firewall" {

			v.subPanel = subPanelNone
			v.subPanelData = "[C] disabled by capa-firewall doctrine"
			return v, nil
		}
		v.subPanel = subPanelCrossProject
		v.subPanelData = "(loading cross-project hits…)"
		return v, v.dispatchCrossProject()
	case "f":

		v.subPanel = subPanelContractFederation
		v.subPanelData = ""
		return v, v.dispatchContractFederation()
	}
	return v, nil
}

func (v *CodegraphView) dispatchContractFederation() tea.Cmd {
	if v.contractFederation == nil {
		return nil
	}
	return v.contractFederation.Refetch()
}

func (v *CodegraphView) dispatchQuery(query string) tea.Cmd {
	if v.c == nil {
		return func() tea.Msg {
			return codegraphSubPanelMsg{
				mode: subPanelQuery,
				body: "(no daemon — test mode result for query: " + query + ")",
			}
		}
	}
	return func() tea.Msg {
		ctx, cancel := newQueryContext()
		defer cancel()
		resp, err := v.c.CodegraphQuery(ctx, client.CodegraphQueryRequest{Query: query})
		if err != nil {
			return codegraphSubPanelMsg{mode: subPanelQuery, err: err}
		}
		return codegraphSubPanelMsg{mode: subPanelQuery, body: renderQueryHits(resp)}
	}
}

func (v *CodegraphView) dispatchImpact() tea.Cmd {
	if v.c == nil {
		return func() tea.Msg {
			return codegraphSubPanelMsg{
				mode: subPanelImpact,
				body: "(no daemon — impact preview unavailable)",
			}
		}
	}
	file := v.currentFile
	return func() tea.Msg {
		ctx, cancel := newQueryContext()
		defer cancel()

		resp, err := v.c.Impact(ctx, client.ImpactRequest{Symbol: file})
		if err != nil {
			return codegraphSubPanelMsg{mode: subPanelImpact, err: err}
		}
		return codegraphSubPanelMsg{mode: subPanelImpact, body: renderImpact(resp)}
	}
}

func (v *CodegraphView) dispatchWiki() tea.Cmd {
	if v.c == nil {
		return func() tea.Msg {
			return codegraphSubPanelMsg{
				mode: subPanelWiki,
				body: "(no daemon — wiki unavailable)",
			}
		}
	}
	communityID := v.community.ID
	return func() tea.Msg {
		ctx, cancel := newQueryContext()
		defer cancel()
		resp, err := v.c.Wiki(ctx, client.WikiRequest{Module: communityID})
		if err != nil {
			return codegraphSubPanelMsg{mode: subPanelWiki, err: err}
		}
		return codegraphSubPanelMsg{mode: subPanelWiki, body: resp.Markdown}
	}
}

func (v *CodegraphView) dispatchCrossProject() tea.Cmd {
	if v.c == nil {
		return func() tea.Msg {
			return codegraphSubPanelMsg{
				mode: subPanelCrossProject,
				body: "(no daemon — cross-project hits unavailable)",
			}
		}
	}
	var symbol string
	if len(v.symbols) > 0 {
		symbol = v.symbols[0].Name
	}
	if symbol == "" {
		symbol = v.currentFile
	}
	return func() tea.Msg {
		ctx, cancel := newQueryContext()
		defer cancel()
		req := client.KnowledgeQueryRequest{
			CodeSymbol:   symbol,
			CrossProject: true,
			Limit:        25,
		}
		rows, err := v.c.KnowledgeQuery(ctx, req)
		if err != nil {
			return codegraphSubPanelMsg{mode: subPanelCrossProject, err: err}
		}
		var b strings.Builder
		for _, r := range rows {
			b.WriteString(fmt.Sprintf("  %s/%s — score=%.2f\n",
				r.ProjectAlias, r.FilePath, r.Score))
		}
		body := b.String()
		if body == "" {
			body = "(no cross-project hits for symbol " + symbol + ")"
		}
		return codegraphSubPanelMsg{mode: subPanelCrossProject, body: body}
	}
}

func renderQueryHits(r *client.CodegraphQueryResponse) string {
	if r == nil || len(r.Hits) == 0 {
		return "(no matches)"
	}
	var b strings.Builder
	for _, h := range r.Hits {
		b.WriteString(fmt.Sprintf("  %s  (%s) @ %s:%d\n", h.Symbol, h.Kind, h.File, h.Line))
	}
	return b.String()
}

func renderImpact(r *client.ImpactResponse) string {
	if r == nil {
		return "(no impact data)"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("  symbol:       %s\n", r.Symbol))
	b.WriteString(fmt.Sprintf("  blast radius: %s (score=%d)\n", r.BlastRadius, r.Score))
	if len(r.AffectedFiles) == 0 {
		b.WriteString("  affected:     (none)\n")
	} else {
		b.WriteString("  affected:\n")
		for _, f := range r.AffectedFiles {
			b.WriteString("    - " + f + "\n")
		}
	}
	return b.String()
}

var newQueryContext = func() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (v *CodegraphView) Refetch() tea.Cmd {
	if v.c == nil || v.currentFile == "" {
		return nil
	}
	file := v.currentFile
	return func() tea.Msg {
		ctx, cancel := newQueryContext()
		defer cancel()
		resp, err := v.c.CodegraphFile(ctx, file)
		if err != nil {
			return codegraphDataMsg{err: err}
		}
		syms := make([]symbolEntry, 0, len(resp.Symbols))
		for _, s := range resp.Symbols {
			syms = append(syms, symbolEntry{Name: s.Name, Kind: s.Kind, Line: s.Line})
		}
		callers := make([]callerEntry, 0, len(resp.Callers))
		for _, c := range resp.Callers {
			callers = append(callers, callerEntry{
				File: c.File, Symbol: c.Symbol, Count30d: c.Count30d,
			})
		}

		peers := make([]coChangeEntry, 0, len(resp.CoChangePeers))
		for _, p := range resp.CoChangePeers {
			peers = append(peers, coChangeEntry{
				Path:            p.Path,
				CouplingPercent: p.CouplingPercent,
			})
		}
		return codegraphDataMsg{
			symbols: syms,
			callers: callers,
			community: communityInfo{
				ID:      resp.CommunityID,
				Summary: resp.CommunitySummary,
			},
			churn: churnInfo{
				Commits7d: resp.Commits7d,
				Authors:   resp.Authors,
			},
			blastRad:  resp.BlastRadiusScore,
			lastIndex: resp.LastIndexedRFC3339,

			coreness:      resp.Coreness,
			sccID:         resp.SCCID,
			cyclic:        resp.Cyclic,
			coChangePeers: peers,
		}
	}
}

var _ = lipgloss.NewStyle
