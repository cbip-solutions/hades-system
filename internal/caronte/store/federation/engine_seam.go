//go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte"
)

func (w *WorkspaceFederationDB) FederationGetWorkspace(ctx context.Context, workspaceID string) (caronte.FederationWorkspaceRow, error) {
	row, err := w.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return caronte.FederationWorkspaceRow{}, err
	}
	return caronte.FederationWorkspaceRow{
		WorkspaceID:   row.WorkspaceID,
		OwningProject: row.OwningProject,
		PolicyLocked:  row.PolicyLocked,
		CreatedAt:     row.CreatedAt,
		SchemaVersion: row.SchemaVersion,
	}, nil
}

func (w *WorkspaceFederationDB) FederationListWorkspaceMembers(ctx context.Context, workspaceID string) ([]caronte.FederationMemberRow, error) {
	rows, err := w.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]caronte.FederationMemberRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, caronte.FederationMemberRow{
			WorkspaceID:  r.WorkspaceID,
			ProjectID:    r.ProjectID,
			RegisteredAt: r.RegisteredAt,
		})
	}
	return out, nil
}

func (w *WorkspaceFederationDB) FederationGetWorkspacePolicy(ctx context.Context, workspaceID string) (string, error) {
	return w.GetWorkspacePolicy(ctx, workspaceID)
}

func (w *WorkspaceFederationDB) FederationListContractLinks(ctx context.Context, workspaceID string, limit int) ([]caronte.FederationLinkRow, error) {
	rows, err := w.ListContractLinks(ctx, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]caronte.FederationLinkRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, caronte.FederationLinkRow{
			CallID:       r.CallID,
			CallRepo:     r.CallRepo,
			EndpointID:   r.EndpointID,
			EndpointRepo: r.EndpointRepo,
			Confidence:   r.Confidence,
			WorkspaceID:  r.WorkspaceID,
			ResolvedAt:   r.ResolvedAt,
			LinkMethod:   r.LinkMethod,
		})
	}
	return out, nil
}

func (w *WorkspaceFederationDB) FederationListRecentBreakingChanges(ctx context.Context, workspaceID string, limit int) ([]caronte.FederationBreakingChangeRow, error) {
	rows, err := w.ListRecentBreakingChanges(ctx, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]caronte.FederationBreakingChangeRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapBreakingChange(r))
	}
	return out, nil
}

func (w *WorkspaceFederationDB) FederationGetBreakingChangeWithConsumers(ctx context.Context, changeID string) (caronte.FederationBreakingChangeRow, []caronte.FederationConsumerRow, error) {
	bc, consumers, err := w.GetBreakingChangeWithConsumers(ctx, changeID)
	if err != nil {
		return caronte.FederationBreakingChangeRow{}, nil, err
	}
	out := make([]caronte.FederationConsumerRow, 0, len(consumers))
	for _, c := range consumers {
		out = append(out, caronte.FederationConsumerRow{
			ChangeID: c.ChangeID,
			CallID:   c.CallID,
			CallRepo: c.CallRepo,
		})
	}
	return mapBreakingChange(bc), out, nil
}

func (w *WorkspaceFederationDB) FederationListWorkspaces(ctx context.Context) ([]caronte.FederationWorkspaceRow, error) {
	rows, err := w.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]caronte.FederationWorkspaceRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, caronte.FederationWorkspaceRow{
			WorkspaceID:   r.WorkspaceID,
			OwningProject: r.OwningProject,
			PolicyLocked:  r.PolicyLocked,
			CreatedAt:     r.CreatedAt,
			SchemaVersion: r.SchemaVersion,
		})
	}
	return out, nil
}

func mapBreakingChange(r BreakingChange) caronte.FederationBreakingChangeRow {
	return caronte.FederationBreakingChangeRow{
		ChangeID:       r.ChangeID,
		WorkspaceID:    r.WorkspaceID,
		EndpointID:     r.EndpointID,
		EndpointRepo:   r.EndpointRepo,
		Kind:           r.Kind,
		Detail:         r.Detail,
		DetectedAt:     r.DetectedAt,
		DetectorID:     r.DetectorID,
		LoreAuthor:     r.LoreAuthor,
		LoreCommitSHA:  r.LoreCommitSHA,
		LoreADRRefs:    r.LoreADRRefs,
		LoreSupersedes: r.LoreSupersedes,
	}
}
