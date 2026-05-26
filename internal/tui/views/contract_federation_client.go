// SPDX-License-Identifier: MIT
// Package views — contract_federation_client.go (Plan 20 Phase J Task J-5).
//
// productionContractFederationClient adapts *client.Client to the
// ContractFederationClient seam consumed by ContractFederationView. Each
// method wraps the Phase I client surface (WorkspaceList +
// FederationListRecentBreakingChanges + FederationRecentDispatches —
// the latter two are Phase J client extensions that this adapter calls).
//
// inv-zen-129 enforced: this file uses ONLY *client.Client methods —
// never net/http directly. inv-zen-088 single-egress preserved: every
// round-trip proxies through the daemon.
package views

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/client"
)

type productionContractFederationClient struct {
	c *client.Client
}

func newProductionContractFederationClient(c *client.Client) ContractFederationClient {
	return &productionContractFederationClient{c: c}
}

func (p *productionContractFederationClient) ListWorkspaces(ctx context.Context) ([]WorkspaceRow, error) {
	listResp, err := p.c.WorkspaceList(ctx, client.WorkspaceListRequest{})
	if err != nil {
		return nil, err
	}
	rows := make([]WorkspaceRow, 0, len(listResp.Workspaces))
	for _, w := range listResp.Workspaces {
		policy := "permissive"
		if w.PolicyLocked {
			policy = "locked"
		}
		members := []string{}
		if memResp, mErr := p.c.WorkspaceMembers(ctx, client.WorkspaceMembersRequest{WorkspaceID: w.WorkspaceID}); mErr == nil {
			members = make([]string, 0, len(memResp.Members))
			for _, m := range memResp.Members {
				members = append(members, m.ProjectID)
			}
		}

		rows = append(rows, WorkspaceRow{
			WorkspaceID: w.WorkspaceID,
			Members:     members,
			Policy:      policy,
		})
	}
	return rows, nil
}

func (p *productionContractFederationClient) ListRecentBreakingChanges(ctx context.Context, limit int) ([]BreakingChangeRow, error) {
	resp, err := p.c.FederationRecentBreakingChanges(ctx, client.FederationRecentBreakingChangesRequest{Limit: limit})
	if err != nil {
		return nil, err
	}
	rows := make([]BreakingChangeRow, 0, len(resp.Changes))
	for _, bc := range resp.Changes {
		rows = append(rows, BreakingChangeRow{
			ChangeID:       bc.ChangeID,
			BreakingKind:   bc.Kind,
			Severity:       bc.Severity,
			SourceEndpoint: bc.EndpointID,
			LoreAuthor:     bc.LoreAuthor,
			LoreCommitSHA:  bc.LoreCommitSHA,
		})
	}
	return rows, nil
}

func (p *productionContractFederationClient) ListRecentDispatchDecisions(ctx context.Context, limit int) ([]DispatchDecisionRow, error) {
	resp, err := p.c.FederationRecentDispatches(ctx, client.FederationRecentDispatchesRequest{Limit: limit})
	if err != nil {
		return nil, err
	}
	rows := make([]DispatchDecisionRow, 0, len(resp.Decisions))
	for _, d := range resp.Decisions {
		rows = append(rows, DispatchDecisionRow{
			ChangeID:        d.ChangeID,
			Mode:            d.Mode,
			DispatchedRepos: d.DispatchedRepos,
			AuditID:         d.AuditID,
		})
	}
	return rows, nil
}
