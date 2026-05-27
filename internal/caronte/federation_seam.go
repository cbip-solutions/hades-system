// SPDX-License-Identifier: MIT
// internal/caronte/federation_seam.go.
//
// FederationStore is the narrow seam interface the engine's federation
// ops consume. The concrete *federation.WorkspaceFederationDB satisfies it (via
// the methods shipped at internal/caronte/store/federation/*.go).
// Declaring the interface here (NOT importing federation directly) keeps the
// engine package free of the federation package import (DECISION 7: seam types
// belong to the OWNING subpackage, but the engine consumes only what it needs
// via a narrow interface — wires the concrete *WorkspaceFederationDB
// through Deps.FederationDB at the composition root in ).
//
// invariant / invariant / invariant preserved: federation is a sibling
// subpackage of caronte/store; the engine sees only the read methods, not the
// constructor or the open boundary.
package caronte

import "context"

type FederationStore interface {
	FederationGetWorkspace(ctx context.Context, workspaceID string) (FederationWorkspaceRow, error)

	FederationListWorkspaceMembers(ctx context.Context, workspaceID string) ([]FederationMemberRow, error)

	FederationGetWorkspacePolicy(ctx context.Context, workspaceID string) (string, error)

	FederationListContractLinks(ctx context.Context, workspaceID string, limit int) ([]FederationLinkRow, error)

	FederationListRecentBreakingChanges(ctx context.Context, workspaceID string, limit int) ([]FederationBreakingChangeRow, error)

	FederationGetBreakingChangeWithConsumers(ctx context.Context, changeID string) (FederationBreakingChangeRow, []FederationConsumerRow, error)

	FederationListWorkspaces(ctx context.Context) ([]FederationWorkspaceRow, error)
}

type FederationWorkspaceRow struct {
	WorkspaceID   string
	OwningProject string
	PolicyLocked  bool
	CreatedAt     int64
	SchemaVersion int
}

type FederationMemberRow struct {
	WorkspaceID  string
	ProjectID    string
	RegisteredAt int64
}

type FederationLinkRow struct {
	CallID       string
	CallRepo     string
	EndpointID   string
	EndpointRepo string
	Confidence   string
	WorkspaceID  string
	ResolvedAt   int64
	LinkMethod   string
}

type FederationBreakingChangeRow struct {
	ChangeID       string
	WorkspaceID    string
	EndpointID     string
	EndpointRepo   string
	Kind           string
	Detail         string
	DetectedAt     int64
	DetectorID     string
	LoreAuthor     string
	LoreCommitSHA  string
	LoreADRRefs    string
	LoreSupersedes string
}

type FederationConsumerRow struct {
	ChangeID string
	CallID   string
	CallRepo string
}
