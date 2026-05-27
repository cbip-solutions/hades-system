// SPDX-License-Identifier: MIT
// Package knowledgeadapter is the invariant bridge between
// internal/daemon/knowledgeadapter and internal/knowledge/aggregator.
//
// The aggregator package (internal/knowledge/aggregator) must NOT import
// internal/store. This package satisfies the aggregator.PerProjectKnowledgeStore
// interface using the daemon's daemon-store *sql.DB (obtained via
// NewAdapterFromDB or the daemon-level convenience wrapper NewAdapter).
//
// Compliance tests/compliance/inv_zen_031_boundary_test.go enforces that
// the aggregator package never imports internal/store.
//
// The Adapter satisfies the aggregator.PerProjectKnowledgeStore interface:
//
// ListAuthorizedProjects — reads projects_alias (all active rows, no
// knowledge_aggregator_authorized column in
// schema; D-12 ships the defensive fallback that
// returns all active projects). The column seam
// is documented as a forward-compat hook for a
// future migration that adds per-project ACL.
// OpenProjectVault — opens (or returns cached) a *sql.DB for the
// per-project vault.db at
// <canonical_path>/.zen/vault.db. The DB is
// opened with WAL + FKs + FTS5. Cache is
// mutex-protected; second call to the same
// projectID returns the existing handle.
// UpdateAuditChainAnchor — writes the canonical audit chain anchor back
// into the per-project vault's
// knowledge_extension table after a Promote.
// If the table does not yet exist ( schema
// seam), the method creates it on first use
// (idempotent DDL). This lets D-12 ship without
// depending on a vault schema migration.
//
// Close drains the vault DB cache; callers (daemon Stop) MUST call Close
// after the aggregator is no longer in use to avoid fd leaks.
//
// Driver note:
// This package imports aggregator (which via db.go pulls in mattn/go-sqlite3,
// a CGO driver). The production daemon binary also uses ncruces/go-sqlite3
// (via internal/store), but this package does NOT import internal/store
// itself — only internal/daemon/server_knowledge_aggregator.go wires the
// two together (it is a binary-level package where both drivers coexist).
// Keeping internal/store out of knowledgeadapter means the adapter test
// binary only has mattn; ncruces is never pulled in.
//
// Constructor pattern:
// - NewAdapterFromDB(db *sql.DB) — used by tests and by the daemon glue
// in server_knowledge_aggregator.go (passes s.store.DB()).
//
// invariant: this package imports internal/knowledge/aggregator but NOT
//
// internal/store. The daemon glue file is the only place that calls
// s.store.DB() and forwards the *sql.DB here.
//
// invariant: this package does NOT import net/http.
package knowledgeadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/knowledge/knowledgetypes"
)

var _ knowledgetypes.PerProjectKnowledgeStore = (*Adapter)(nil)

type Adapter struct {
	daemonDB *sql.DB

	mu     sync.Mutex
	vaults map[string]*sql.DB
}

func NewAdapterFromDB(db *sql.DB) *Adapter {
	return &Adapter{
		daemonDB: db,
		vaults:   make(map[string]*sql.DB),
	}
}

func (a *Adapter) ListAuthorizedProjects(ctx context.Context) ([]knowledgetypes.ProjectHandle, error) {
	rows, err := a.daemonDB.QueryContext(ctx, `
		SELECT id_sha256, alias, canonical_path
		FROM   projects_alias
		WHERE  archived_at IS NULL
		ORDER  BY alias ASC
	`)
	if err != nil {

		if isNoSuchTable(err) {
			return []knowledgetypes.ProjectHandle{}, nil
		}
		return nil, fmt.Errorf("knowledgeadapter: ListAuthorizedProjects: %w", err)
	}
	defer rows.Close()

	var handles []knowledgetypes.ProjectHandle
	for rows.Next() {
		var h knowledgetypes.ProjectHandle
		var canonicalPath string
		if err := rows.Scan(&h.ProjectID, &h.Alias, &canonicalPath); err != nil {

			return nil, fmt.Errorf("knowledgeadapter: ListAuthorizedProjects scan: %w", err)
		}
		h.VaultPath = filepath.Join(canonicalPath, ".zen", "vault.db")
		handles = append(handles, h)
	}
	if err := rows.Err(); err != nil {

		return nil, fmt.Errorf("knowledgeadapter: ListAuthorizedProjects rows: %w", err)
	}
	if handles == nil {
		handles = []knowledgetypes.ProjectHandle{}
	}
	return handles, nil
}

// OpenProjectVault returns a *sql.DB (typed as knowledgetypes.ProjectVault,
// an empty interface) for the per-project vault.db at VaultPath.
//
// The DB is opened once and cached by projectID; concurrent callers
// receive the same handle without reopening the file. WAL mode + FK
// enforcement are set on first open. The caller MUST NOT close the
// returned DB — Close() drains all cached handles on daemon shutdown.
//
// Type-assertion contract: the returned ProjectVault is always *sql.DB.
// Callers that need a real *sql.DB (e.g. the aggregator's Promote
// pipeline) type-assert: db, ok := vault.(*sql.DB).
func (a *Adapter) OpenProjectVault(ctx context.Context, projectID string) (knowledgetypes.ProjectVault, error) {
	a.mu.Lock()
	if db, ok := a.vaults[projectID]; ok {
		a.mu.Unlock()
		return db, nil
	}
	a.mu.Unlock()

	vaultPath, err := a.resolveVaultPath(ctx, projectID)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3_ncruces", vaultPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {

		return nil, fmt.Errorf("knowledgeadapter: OpenProjectVault %q: open: %w", projectID, err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("knowledgeadapter: OpenProjectVault %q: ping: %w", projectID, err)
	}

	if err := ensureVaultSchema(ctx, db); err != nil {

		db.Close()
		return nil, fmt.Errorf("knowledgeadapter: OpenProjectVault %q: schema: %w", projectID, err)
	}

	a.mu.Lock()
	if existing, ok := a.vaults[projectID]; ok {

		a.mu.Unlock()
		db.Close()
		return existing, nil
	}
	a.vaults[projectID] = db
	a.mu.Unlock()

	return db, nil
}

func (a *Adapter) UpdateAuditChainAnchor(ctx context.Context, projectID, noteID, anchor string) error {
	vault, err := a.OpenProjectVault(ctx, projectID)
	if err != nil {
		return fmt.Errorf("knowledgeadapter: UpdateAuditChainAnchor: open vault: %w", err)
	}
	db := vault.(*sql.DB)

	if err := ensureKnowledgeExtension(ctx, db); err != nil {
		return fmt.Errorf("knowledgeadapter: UpdateAuditChainAnchor: ensure schema: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO knowledge_extension (project_id, note_id, audit_chain_anchor)
		VALUES (?, ?, ?)
		ON CONFLICT(project_id, note_id)
		DO UPDATE SET audit_chain_anchor = excluded.audit_chain_anchor
	`, projectID, noteID, anchor)
	if err != nil {
		return fmt.Errorf("knowledgeadapter: UpdateAuditChainAnchor: upsert: %w", err)
	}
	return nil
}

func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var errs []error
	for id, db := range a.vaults {
		if err := db.Close(); err != nil {

			errs = append(errs, fmt.Errorf("vault %s: %w", id, err))
		}
	}
	// Clear the cache so subsequent calls do not re-use closed handles.
	a.vaults = make(map[string]*sql.DB)

	if len(errs) > 0 {

		return fmt.Errorf("knowledgeadapter: Close: %d vault(s) failed: %v", len(errs), errs)
	}
	return nil
}

func (a *Adapter) resolveVaultPath(ctx context.Context, projectID string) (string, error) {
	var canonicalPath string
	err := a.daemonDB.QueryRowContext(ctx, `
		SELECT canonical_path FROM projects_alias WHERE id_sha256 = ?
	`, projectID).Scan(&canonicalPath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("knowledgeadapter: project %q not found in projects_alias", projectID)
	}
	if err != nil {
		return "", fmt.Errorf("knowledgeadapter: resolveVaultPath %q: %w", projectID, err)
	}
	return filepath.Join(canonicalPath, ".zen", "vault.db"), nil
}

func ensureVaultSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS knowledge_extension (
			project_id          TEXT NOT NULL,
			note_id             TEXT NOT NULL,
			audit_chain_anchor  TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (project_id, note_id)
		);
	`)
	return err
}

func ensureKnowledgeExtension(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS knowledge_extension (
			project_id          TEXT NOT NULL,
			note_id             TEXT NOT NULL,
			audit_chain_anchor  TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (project_id, note_id)
		);
	`)
	return err
}

func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}

	return errors.As(err, new(interface{ Error() string })) &&
		containsSubstr(err.Error(), "no such table")
}

func containsSubstr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
