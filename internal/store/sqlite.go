// SPDX-License-Identifier: MIT
// Package store wraps SQLite for hades-ctld.
//
// Uses ncruces/go-sqlite3 (pure-Go via wasm, no CGO) per invariant
// (auditable; zero supply chain via CGO toolchain) and verified by
// design v1.2 R3.
//
// This package owns the schema and exposes one Go file per table with
// typed CRUD. release establishes the full schema (all ~16 tables) and
// type signatures; most CRUD bodies are stubs returning
// errors.ErrNotImplementedPlan{N} (subsequent plans fill them in). The
// EXCEPTION is events.go, which is fully implemented in release because
// the daemon's batched event writer needs it from day 1.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

type Store struct {
	db   *sql.DB
	path string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3_ncruces", path)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA temp_store = MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	return &Store{db: db, path: path}, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Path() string { return s.path }

func DefaultPath() (string, error) {
	dir, err := defaultDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}

func defaultDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "hades-system"), nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", fmt.Errorf("$HOME not set")
	}
	return filepath.Join(home, ".local", "share", "hades-system"), nil
}
