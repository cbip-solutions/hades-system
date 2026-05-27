// SPDX-License-Identifier: MIT
// Package knowledgetypes defines shared pure-Go types and interfaces for
// the knowledge aggregator subsystem.
//
// This package exists specifically to break the CGO driver conflict between
// internal/knowledge/aggregator (which via db.go imports mattn/go-sqlite3)
// and internal/store (which imports ncruces/go-sqlite3). Both drivers
// register the "sqlite3" SQL driver name; having both in the same binary
// causes a double-registration panic.
//
// By placing the PerProjectKnowledgeStore interface, ProjectHandle, and
// ProjectVault types here — in a package with NO CGO dependency —
// internal/daemon/knowledgeadapter can import this package (satisfying the
// interface contract) without importing internal/knowledge/aggregator. The
// compile-time interface satisfaction assertion in knowledgeadapter/adapter.go
// uses this package's interface type, keeping mattn out of the daemon and
// knowledgeadapter test binaries.
//
// Import topology (driver-conflict free):
//
// internal/knowledge/aggregator → knowledgetypes (no CGO)
// internal/daemon/knowledgeadapter → knowledgetypes (no CGO)
// cmd/zen-swarm-ctld → aggregator (mattn) + daemon (ncruces)
// mattn wins init() race; ncruces skips
//
// invariant: this package does NOT import internal/store.
// invariant: this package does NOT import net/http.
package knowledgetypes

import "context"

type ProjectVault interface{}

type ProjectHandle struct {
	ProjectID string `json:"project_id"`

	Alias string `json:"alias,omitempty"`

	VaultPath string `json:"vault_path"`
}

type PerProjectKnowledgeStore interface {
	ListAuthorizedProjects(ctx context.Context) ([]ProjectHandle, error)

	OpenProjectVault(ctx context.Context, projectID string) (ProjectVault, error)

	UpdateAuditChainAnchor(ctx context.Context, projectID, noteID, anchor string) error
}
