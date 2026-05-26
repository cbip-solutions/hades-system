//go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"
)

type BreakingChangeConsumer struct {
	ChangeID string
	CallID   string
	CallRepo string
}

func (w *WorkspaceFederationDB) InsertBreakingChangeConsumer(_ context.Context, _ BreakingChangeConsumer) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) InsertBreakingChangeConsumerTx(_ context.Context, _ *sql.Tx, _ BreakingChangeConsumer) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListBreakingChangeConsumers(_ context.Context, _ string) ([]BreakingChangeConsumer, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) DeleteConsumersByChange(_ context.Context, _ string) (int64, error) {
	return 0, ErrCGODisabled
}
