//go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
)

type WorkspaceFederationDB struct {
	db           *sql.DB
	auditEmitter AuditEmitter
}

type Option func(*WorkspaceFederationDB)

func WithAuditEmitter(e AuditEmitter) Option {
	return func(w *WorkspaceFederationDB) {
		w.auditEmitter = e
	}
}

func (w *WorkspaceFederationDB) DB() *sql.DB { return w.db }

func Open(_ context.Context, _ string, _ ...Option) (*WorkspaceFederationDB, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) Init(_ context.Context) error { return ErrCGODisabled }

func (w *WorkspaceFederationDB) Close() error { return nil }

func federationBoundarySentinel() error { return nil }
