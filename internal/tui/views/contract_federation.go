// SPDX-License-Identifier: MIT
// Package views — contract_federation.go.
//
// F7 Code Graph panel "Contract Federation" sub-panel per spec §10.3
// + master C-14. Renders three sections sourced from the release
// workspace federation substrate,
// breaking-change events, and L10 dispatch
// decisions:
//
// - workspace roster : registered workspaces + members + policy
// - recent BREAKING : top-N breaking-change rows by detected_at
// (with Lore attribution preview per D7)
// - L10 dispatch log : top-N dispatch decisions by decided_at
// (Mode + DispatchedRepos + AuditID)
//
// Wired into F7 codegraph view as the fifth sub-panel mode
// (subPanelContractFederation, codegraph_keys.go). Operator opens
// with [F] from the F7 base layout.
//
// Data flow: ContractFederationClient seam (interface) — production
// adapts *client.Client REST methods (the client surface
// extended for via federation_recent.go + a new daemon REST
// handler); tests inject fakes directly. Refetch() runs one
// round-trip and delivers contractFederationDataMsg; Update applies
// it; View renders. lastErr fallback per spec §15 graceful
// degradation.
//
// Privacy / inv-hades-031 / inv-hades-129: this file consumes the daemon
// ONLY via internal/client; it does NOT import internal/caronte/*,
// does NOT touch net/http directly, and does NOT import the
// federation/coordinator packages (those live behind the daemon's
// narrow ContractFederationForDaemon + ContractCoordinatorForDaemon
// interfaces; the daemon REST routes mediate).
package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var contractFederationFetchTimeout = 2 * time.Second

type ContractFederationView struct {
	c ContractFederationClient

	workspaces []WorkspaceRow
	breaking   []BreakingChangeRow
	dispatch   []DispatchDecisionRow

	lastErr error
}

type WorkspaceRow struct {
	WorkspaceID string
	Members     []string
	Policy      string
}

type BreakingChangeRow struct {
	ChangeID       string
	BreakingKind   string
	Severity       string
	SourceEndpoint string
	LoreAuthor     string
	LoreCommitSHA  string
	DetectedAt     time.Time
}

type DispatchDecisionRow struct {
	ChangeID        string
	Mode            string
	DispatchedRepos []string
	AuditID         string
	DecidedAt       time.Time
}

type ContractFederationClient interface {
	ListWorkspaces(ctx context.Context) ([]WorkspaceRow, error)
	ListRecentBreakingChanges(ctx context.Context, limit int) ([]BreakingChangeRow, error)
	ListRecentDispatchDecisions(ctx context.Context, limit int) ([]DispatchDecisionRow, error)
}

type contractFederationDataMsg struct {
	workspaces []WorkspaceRow
	breaking   []BreakingChangeRow
	dispatch   []DispatchDecisionRow
	err        error
}

func NewContractFederationView(c ContractFederationClient) *ContractFederationView {
	return &ContractFederationView{c: c}
}

func (v *ContractFederationView) Init() tea.Cmd { return nil }

func (v *ContractFederationView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(contractFederationDataMsg); ok {
		v.workspaces = m.workspaces
		v.breaking = m.breaking
		v.dispatch = m.dispatch
		v.lastErr = m.err
		return v, nil
	}
	return v, nil
}

func (v *ContractFederationView) View() string {
	var b strings.Builder
	b.WriteString(panelHeader("CONTRACT FEDERATION"))
	b.WriteString("\n")

	if v.lastErr != nil {
		b.WriteString(errStyle.Render(
			"  x " + v.lastErr.Error() + "  (panel will retry on next tick)"))
		return b.String()
	}

	b.WriteString("  workspaces       :  ")
	if len(v.workspaces) == 0 {
		b.WriteString(mutedStyle.Render("(no workspaces registered)"))
	} else {
		parts := make([]string, 0, len(v.workspaces))
		for _, w := range v.workspaces {
			parts = append(parts, fmt.Sprintf("%s [%s] (%d members)",
				w.WorkspaceID, w.Policy, len(w.Members)))
		}
		b.WriteString(strings.Join(parts, "  "))
	}
	b.WriteString("\n")

	b.WriteString("  recent BREAKING  :  ")
	if len(v.breaking) == 0 {
		b.WriteString(mutedStyle.Render("(no recent breaking changes)"))
	} else {
		for i, bc := range v.breaking {
			if i > 0 {
				b.WriteString("\n                       ")
			}
			attribution := mutedStyle.Render("(no Lore evidence)")
			if bc.LoreAuthor != "" {
				shortSHA := bc.LoreCommitSHA
				if len(shortSHA) > 7 {
					shortSHA = shortSHA[:7]
				}
				attribution = fmt.Sprintf("%s@%s", bc.LoreAuthor, shortSHA)
			}
			b.WriteString(fmt.Sprintf("%s . %s . %s . %s . %s · %s",
				bc.ChangeID, bc.BreakingKind, bc.Severity, bc.SourceEndpoint, attribution,
				relativeTimeRender(bc.DetectedAt)))
		}
	}
	b.WriteString("\n")

	b.WriteString("  L10 dispatch     :  ")
	if len(v.dispatch) == 0 {
		b.WriteString(mutedStyle.Render("(no dispatch decisions yet)"))
	} else {
		for i, d := range v.dispatch {
			if i > 0 {
				b.WriteString("\n                       ")
			}
			repos := strings.Join(d.DispatchedRepos, ",")
			if repos == "" {
				repos = "(none)"
			}
			b.WriteString(fmt.Sprintf("%s . mode=%s . repos=[%s] . audit=%s · %s",
				d.ChangeID, d.Mode, repos, d.AuditID, relativeTimeRender(d.DecidedAt)))
		}
	}
	b.WriteString("\n\n  ")
	b.WriteString(mutedStyle.Render("[Esc] close subpanel    (data refreshes on F7 panel cadence)"))
	return b.String()
}

func (v *ContractFederationView) Refetch() tea.Cmd {
	if v.c == nil {
		return func() tea.Msg {
			return contractFederationDataMsg{}
		}
	}
	c := v.c
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), contractFederationFetchTimeout)
		defer cancel()
		var msg contractFederationDataMsg
		if ws, err := c.ListWorkspaces(ctx); err != nil {
			msg.err = err
		} else {
			msg.workspaces = ws
		}
		if bc, err := c.ListRecentBreakingChanges(ctx, 10); err != nil {
			if msg.err == nil {
				msg.err = err
			}
		} else {
			msg.breaking = bc
		}
		if dd, err := c.ListRecentDispatchDecisions(ctx, 10); err != nil {
			if msg.err == nil {
				msg.err = err
			}
		} else {
			msg.dispatch = dd
		}
		return msg
	}
}

func relativeTimeRender(t time.Time) string {
	if t.IsZero() {
		return "(unknown)"
	}
	d := time.Since(t)
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
