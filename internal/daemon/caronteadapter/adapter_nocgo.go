// go:build !cgo
//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package caronteadapter

import (
	"context"
	"database/sql"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type Adapter struct {
	daemonDB *sql.DB
}

func NewAdapterFromDB(db *sql.DB) *Adapter {
	return &Adapter{daemonDB: db}
}

func (a *Adapter) OpenProjectDB(_ context.Context, _ string) (*sql.DB, error) {
	return nil, store.ErrCGODisabled
}

func (a *Adapter) Close() error { return nil }
