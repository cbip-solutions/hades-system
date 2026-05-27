// SPDX-License-Identifier: MIT
// Package projectctxadapter bridges *store.Store to projectctx.ProjectStore.
//
// This package exists outside internal/projectctx on purpose: invariant
// forbids the projectctx package from importing internal/store. The
// adapter must therefore live in a daemon-side package that pulls
// together *store.Store and projectctx.ProjectStore.
//
// Type translation strategy: each adapter method does a field-by-field
// copy between the projectctx-side types (projectctx.Project,
// projectctx.PathHistoryEntry) and the store-side types
// (store.ProjectAliasRow, store.PathHistoryRow). The two type sets are
// intentionally identical in shape; keeping them separate buys two
// things:
// 1. The projectctx package never gains a transitive SQLite dependency
// so chaos / unit tests stay fast and run cross-platform.
// 2. Future store-side schema changes (e.g., adding a column) don't
// ripple into the projectctx package — the adapter absorbs them.
//
// Time precision: the projects_alias / path_history schema (migration
// 057) stores INTEGER unix-MILLISECONDS (UnixMilli), matching the release
// cost_ledger pattern. The adapter translates time.Time ↔ int64 ms via
// time.UnixMilli / t.UnixMilli(). Sub-millisecond precision is not
// preserved on the wire (operator-facing project lifecycle does not need
// it; the second-aligned test data round-trips losslessly).
//
// ArchivedAt translation: the store represents archive state as
// `archived_at int64` where 0 (NULL on the wire, COALESCE'd to 0 on
// scan) means "active" and a positive ms timestamp means "archived at
// that instant". The projectctx-side `Project.ArchivedAt *time.Time`
// uses a pointer so the JSON `omitempty` tag works correctly (
// HTTP handlers consume this shape). nil = active; non-nil = archived.
//
// Context cancellation: store-package functions take *sql.DB rather
// than (ctx, *sql.DB) — they predate release ctx-aware refactor. To
// honor the projectctx.ProjectStore contract that every method respect
// ctx.Done(), each adapter method does a defensive ctx.Err() check at
// entry and returns early with the cancellation error before touching
// the store. Long-running store operations cannot be interrupted
// mid-flight by this layer, but the contract holds for the
// pre-execution gate.
//
// invariant boundary enforcement: this file's import list contains
// both "github.com/cbip-solutions/hades-system/internal/projectctx" and
// "github.com/cbip-solutions/hades-system/internal/store". This is the ONLY
// permissible co-location of those two imports anywhere in the codebase.
// The compliance test in tests/compliance/inv_zen_122_inv_zen_031_plan7_packages_test.go
// (Task A-9) enforces this at test time.
package projectctxadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/projectctx"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type Adapter struct {
	s *store.Store
}

func New(s *store.Store) *Adapter {
	if s == nil {
		panic("projectctxadapter.New: store is nil")
	}
	return &Adapter{s: s}
}

var _ projectctx.ProjectStore = (*Adapter)(nil)

func rowToProject(r store.ProjectAliasRow) *projectctx.Project {
	var archivedAt *time.Time
	if r.ArchivedAt != 0 {
		t := time.UnixMilli(r.ArchivedAt).UTC()
		archivedAt = &t
	}
	return &projectctx.Project{
		ID:            projectctx.ProjectID(r.IDSha256),
		Alias:         projectctx.Alias(r.Alias),
		CanonicalPath: r.CanonicalPath,
		FirstSeenAt:   time.UnixMilli(r.FirstSeenAt).UTC(),
		LastSeenAt:    time.UnixMilli(r.LastSeenAt).UTC(),
		ArchivedAt:    archivedAt,
	}
}

func projectToRow(p *projectctx.Project) store.ProjectAliasRow {
	var archivedMs int64
	if p.ArchivedAt != nil {
		archivedMs = p.ArchivedAt.UnixMilli()
	}
	return store.ProjectAliasRow{
		IDSha256:      string(p.ID),
		Alias:         string(p.Alias),
		CanonicalPath: p.CanonicalPath,
		FirstSeenAt:   p.FirstSeenAt.UnixMilli(),
		LastSeenAt:    p.LastSeenAt.UnixMilli(),
		ArchivedAt:    archivedMs,
	}
}

func rowToHistory(r store.PathHistoryRow) projectctx.PathHistoryEntry {
	return projectctx.PathHistoryEntry{
		ProjectID:   projectctx.ProjectID(r.IDSha256),
		Path:        r.Path,
		FirstSeenAt: time.UnixMilli(r.FirstSeenAt).UTC(),
		LastSeenAt:  time.UnixMilli(r.LastSeenAt).UTC(),
	}
}

func historyToRow(e *projectctx.PathHistoryEntry) store.PathHistoryRow {
	return store.PathHistoryRow{
		IDSha256:    string(e.ProjectID),
		Path:        e.Path,
		FirstSeenAt: e.FirstSeenAt.UnixMilli(),
		LastSeenAt:  e.LastSeenAt.UnixMilli(),
	}
}

func (a *Adapter) GetByAlias(ctx context.Context, alias projectctx.Alias) (*projectctx.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("projectctxadapter.GetByAlias: %w", err)
	}
	r, err := store.GetProjectAliasByAlias(a.s.DB(), string(alias))
	if err != nil {
		return nil, fmt.Errorf("projectctxadapter.GetByAlias: %w", err)
	}
	if r == nil {
		return nil, nil
	}
	return rowToProject(*r), nil
}

func (a *Adapter) GetByID(ctx context.Context, id projectctx.ProjectID) (*projectctx.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("projectctxadapter.GetByID: %w", err)
	}
	r, err := store.GetProjectAliasByID(a.s.DB(), string(id))
	if err != nil {
		return nil, fmt.Errorf("projectctxadapter.GetByID: %w", err)
	}
	if r == nil {
		return nil, nil
	}
	return rowToProject(*r), nil
}

func (a *Adapter) Insert(ctx context.Context, p *projectctx.Project) error {
	if p == nil {
		return errors.New("projectctxadapter.Insert: project is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("projectctxadapter.Insert: %w", err)
	}
	if err := store.InsertProjectAlias(a.s.DB(), projectToRow(p)); err != nil {
		return fmt.Errorf("projectctxadapter.Insert: %w", err)
	}
	return nil
}

func (a *Adapter) UpdateLastSeen(ctx context.Context, alias projectctx.Alias, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("projectctxadapter.UpdateLastSeen: %w", err)
	}
	if err := store.UpdateProjectAliasLastSeen(a.s.DB(), string(alias), t.UnixMilli()); err != nil {
		return fmt.Errorf("projectctxadapter.UpdateLastSeen: %w", err)
	}
	return nil
}

func (a *Adapter) AppendPathHistory(ctx context.Context, e *projectctx.PathHistoryEntry) error {
	if e == nil {
		return errors.New("projectctxadapter.AppendPathHistory: entry is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("projectctxadapter.AppendPathHistory: %w", err)
	}
	if err := store.InsertPathHistory(a.s.DB(), historyToRow(e)); err != nil {
		return fmt.Errorf("projectctxadapter.AppendPathHistory: %w", err)
	}
	return nil
}

func (a *Adapter) GetPathHistory(ctx context.Context, alias projectctx.Alias) ([]projectctx.PathHistoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("projectctxadapter.GetPathHistory: %w", err)
	}
	rows, err := store.QueryPathHistoryByAlias(a.s.DB(), string(alias))
	if err != nil {
		return nil, fmt.Errorf("projectctxadapter.GetPathHistory: %w", err)
	}
	out := make([]projectctx.PathHistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToHistory(r))
	}
	return out, nil
}

func (a *Adapter) Archive(ctx context.Context, alias projectctx.Alias) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("projectctxadapter.Archive: %w", err)
	}
	if err := store.ArchiveProjectAlias(a.s.DB(), string(alias), time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("projectctxadapter.Archive: %w", err)
	}
	return nil
}

func (a *Adapter) Remove(ctx context.Context, alias projectctx.Alias) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("projectctxadapter.Remove: %w", err)
	}
	if err := store.DeleteProjectAlias(a.s.DB(), string(alias)); err != nil {
		return fmt.Errorf("projectctxadapter.Remove: %w", err)
	}
	return nil
}

func (a *Adapter) List(ctx context.Context, includeArchived bool) ([]projectctx.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("projectctxadapter.List: %w", err)
	}
	rows, err := store.ListProjectAliases(a.s.DB(), includeArchived)
	if err != nil {
		return nil, fmt.Errorf("projectctxadapter.List: %w", err)
	}
	out := make([]projectctx.Project, 0, len(rows))
	for _, r := range rows {
		out = append(out, *rowToProject(r))
	}
	return out, nil
}
