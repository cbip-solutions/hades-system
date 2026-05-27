// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package caronteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type Adapter struct {
	daemonDB *sql.DB

	mu  sync.Mutex
	dbs map[string]*sql.DB
}

func NewAdapterFromDB(db *sql.DB) *Adapter {
	sqlite_vec.Auto()
	return &Adapter{
		daemonDB: db,
		dbs:      make(map[string]*sql.DB),
	}
}

// OpenProjectDB returns a *sql.DB for the project's caronte.db, opening
// (and creating the file +.zen/ dir) on first call and returning the
// cached handle thereafter. The caller (caronte.store.Open via the engine)
// MUST NOT Close the returned handle — Close() owns the lifecycle.
//
// This is the C-7 contract: the single file-opening point. The DSN +
// single-writer-WAL pool sizing mirror aggregator.Open exactly.
func (a *Adapter) OpenProjectDB(ctx context.Context, projectID string) (*sql.DB, error) {
	a.mu.Lock()
	if db, ok := a.dbs[projectID]; ok {
		a.mu.Unlock()
		return db, nil
	}
	a.mu.Unlock()

	dbPath, err := a.resolveProjectPath(ctx, projectID)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("caronteadapter: mkdir .zen for %q: %w", projectID, err)
	}
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL", dbPath)
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("caronteadapter: open caronte.db for %q: %w", projectID, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("caronteadapter: ping caronte.db for %q: %w", projectID, err)
	}

	a.mu.Lock()
	if existing, ok := a.dbs[projectID]; ok {

		a.mu.Unlock()
		_ = db.Close()
		return existing, nil
	}
	a.dbs[projectID] = db
	a.mu.Unlock()
	return db, nil
}

func (a *Adapter) resolveProjectPath(ctx context.Context, projectID string) (string, error) {
	var canonicalPath string
	err := a.daemonDB.QueryRowContext(ctx,
		`SELECT canonical_path FROM projects_alias WHERE id_sha256 = ?`, projectID,
	).Scan(&canonicalPath)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("caronteadapter: project %q not found in projects_alias", projectID)
	}
	if err != nil {
		return "", fmt.Errorf("caronteadapter: resolveProjectPath %q: %w", projectID, err)
	}
	return filepath.Join(canonicalPath, ".zen", "caronte.db"), nil
}

// Close closes all cached caronte.db handles and clears the cache. Safe to
// call multiple times; the daemon Stop path MUST call it to avoid fd leaks.
func (a *Adapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var errs []error
	for id, db := range a.dbs {
		if err := db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("caronte.db %s: %w", id, err))
		}
	}
	a.dbs = make(map[string]*sql.DB)
	if len(errs) > 0 {
		return fmt.Errorf("caronteadapter: Close: %d handle(s) failed: %v", len(errs), errs)
	}
	return nil
}
