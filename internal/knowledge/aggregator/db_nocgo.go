//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package aggregator

import (
	"context"
	"database/sql"
	"errors"
)

const DefaultDriver = "sqlite3"

var ErrCGODisabled = errors.New("aggregator: sqlite-vec requires CGO_ENABLED=1; degraded_mode active")

func LoadVecExtension() error {
	return ErrCGODisabled
}

const vecDimensions = 384

func Open(_ context.Context, _ string) (*sql.DB, error) {
	return nil, ErrCGODisabled
}

func Init(_ context.Context, _ *sql.DB) error {
	return ErrCGODisabled
}
