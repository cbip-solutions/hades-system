//go:build cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

type WorkspaceFederationDB struct {
	db           *sql.DB
	closeOnce    sync.Once
	closeErr     error
	auditEmitter AuditEmitter
}

type Option func(*WorkspaceFederationDB)

func WithAuditEmitter(e AuditEmitter) Option {
	return func(db *WorkspaceFederationDB) {
		db.auditEmitter = e
	}
}

// DB returns the wrapped handle. Used by the per-table CRUD files
// (workspaces.go, members.go, links.go, breaking.go, consumers.go) to
// run ExecContext/QueryContext directly. Callers MUST NOT Close it
// (WorkspaceFederationDB.Close owns the lifecycle).
func (w *WorkspaceFederationDB) DB() *sql.DB { return w.db }

func federationBoundarySentinel() error { return nil }

// Open creates (or opens) the workspace.db at statePath using the
// mattn+sqlite-vec single-writer-WAL posture Plan 19 Phase A established
// (matches internal/knowledge/aggregator.Open exactly). Auto-creates the
// parent directory at 0o700, opens the handle, pings, materializes the
// C-2 schema via Init, then applies any caller-supplied Options (which
// include the inv-zen-269 audit-emitter wiring per review I2).
//
// Pre  statePath is non-empty (callers MUST call WorkspaceDBPath first
//
//	to resolve the canonical location; an empty statePath is a
//	composition-root bug → ErrEmptyStatePath).
//
// Post every C-2 table/index exists; FK enforcement is ON; PRAGMA WAL is
//
//	set; pool is sized for single-writer; every Option has been applied.
//
// Review I2: the audit-emitter wiring is an Option (WithAuditEmitter)
// rather than a post-construction setter — the field is written once
// during composition-root serial init and is read-only thereafter, so
// concurrent CRUD callers see a stable value without synchronization.
func Open(ctx context.Context, statePath string, opts ...Option) (*WorkspaceFederationDB, error) {
	if err := federationBoundarySentinel(); err != nil {
		return nil, err
	}
	if statePath == "" {
		return nil, ErrEmptyStatePath
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return nil, fmt.Errorf("caronte/store/federation: mkdir parent: %w", err)
	}

	sqlite_vec.Auto()
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL", statePath)
	raw, err := sql.Open(DefaultDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("caronte/store/federation: sql.Open: %w", err)
	}
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	if err := raw.PingContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("caronte/store/federation: ping: %w", err)
	}
	w := &WorkspaceFederationDB{db: raw}
	if err := w.Init(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	for _, opt := range opts {
		opt(w)
	}
	return w, nil
}

func (w *WorkspaceFederationDB) Init(ctx context.Context) error {
	if w.db == nil {
		return ErrEmptyDB
	}
	conn, err := w.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("caronte/store/federation: acquire conn: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA temp_store = MEMORY`); err != nil {
		return fmt.Errorf("caronte/store/federation: pragma temp_store: %w", err)
	}
	for i, stmt := range schemaStatements() {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("caronte/store/federation: ddl[%d]: %w", i, err)
		}
	}
	return nil
}

func (w *WorkspaceFederationDB) Close() error {
	w.closeOnce.Do(func() {
		if w.db != nil {
			w.closeErr = w.db.Close()
		}
	})
	return w.closeErr
}
