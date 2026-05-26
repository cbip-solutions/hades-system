//go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import (
	"context"
	"database/sql"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type BreakingChange struct {
	ChangeID       string
	WorkspaceID    string
	EndpointID     string
	EndpointRepo   string
	Kind           string
	Detail         string
	DetectedAt     int64
	DetectorID     string
	LoreAuthor     string
	LoreCommitSHA  string
	LoreADRRefs    string
	LoreSupersedes string
}

func (w *WorkspaceFederationDB) InsertBreakingChange(_ context.Context, _ BreakingChange) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) InsertBreakingChangeTx(_ context.Context, _ *sql.Tx, _ BreakingChange) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) GetBreakingChange(_ context.Context, _ string) (BreakingChange, error) {
	return BreakingChange{}, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListBreakingChangesByEndpoint(_ context.Context, _, _, _ string) ([]BreakingChange, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) DeleteBreakingChangesByWorkspace(_ context.Context, _ string) (int64, error) {
	return 0, ErrCGODisabled
}

func (w *WorkspaceFederationDB) GetBreakingChangeWithConsumers(_ context.Context, _ string) (BreakingChange, []BreakingChangeConsumer, error) {
	return BreakingChange{}, nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListRecentBreakingChanges(_ context.Context, _ string, _ int) ([]BreakingChange, error) {
	return nil, ErrCGODisabled
}

func ToStoreBreakingChange(_ BreakingChange) store.BreakingChange {
	return store.BreakingChange{}
}
