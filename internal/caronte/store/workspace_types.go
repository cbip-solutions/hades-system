// SPDX-License-Identifier: MIT
package store

// FederatedQuery is a capa-firewall-gated cross-project query over a Workspace.
// matching that gives it teeth. Scope MUST name only projects on the workspace
// roster (UnauthorizedProjects are rejected, never silently dropped).
type FederatedQuery struct {
	Kind          string
	Scope         []string
	NormalizedKey string
	Limit         int
}

type FederatedResult struct {
	ProjectID string
	Kind      string
	Link      *ContractLink
}

type ContractLink struct {
	CallID       string
	CallRepo     string
	EndpointID   string
	EndpointRepo string
	Confidence   string
	WorkspaceID  string

	ResolvedAt int64
	LinkMethod string
}

type WorkspacePolicy interface {
	PrivacyLocked() bool
}
