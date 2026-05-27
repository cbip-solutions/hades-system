//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"database/sql"
)

type Store struct {
	db *sql.DB
}

func (s *Store) DB() *sql.DB { return s.db }

func Open(_ context.Context, _ *sql.DB) (*Store, error) {
	return nil, ErrCGODisabled
}

func (s *Store) Init(_ context.Context) error { return ErrCGODisabled }

func (s *Store) UpsertNode(_ context.Context, _ Node) error { return ErrCGODisabled }

func (s *Store) UpsertNodeVector(_ context.Context, _ string, _ []float32) error {
	return ErrCGODisabled
}

func (s *Store) GetNode(_ context.Context, _ string) (Node, error) {
	return Node{}, ErrCGODisabled
}

func (s *Store) ListNodesByKind(_ context.Context, _ NodeKind) ([]Node, error) {
	return nil, ErrCGODisabled
}

func (s *Store) UpdateNodeStructure(_ context.Context, _ string, _, _ int, _ string) error {
	return ErrCGODisabled
}

func (s *Store) ContentHashFor(_ context.Context, _ string) (string, error) {
	return "", ErrCGODisabled
}

func (s *Store) UpsertEdge(_ context.Context, _ Edge) error { return ErrCGODisabled }

func (s *Store) ListEdgesByTarget(_ context.Context, _ string, _ EdgeKind) ([]Edge, error) {
	return nil, ErrCGODisabled
}

func (s *Store) ListEdgesBySource(_ context.Context, _ string, _ EdgeKind) ([]Edge, error) {
	return nil, ErrCGODisabled
}

func (s *Store) UpsertCoChange(_ context.Context, _ CoChange) error { return ErrCGODisabled }

func (s *Store) GetCoChange(_ context.Context, _, _ string, _ int) (CoChange, error) {
	return CoChange{}, ErrCGODisabled
}

func (s *Store) UpsertChurn(_ context.Context, _ Churn) error { return ErrCGODisabled }

func (s *Store) GetChurn(_ context.Context, _ string, _ int) (Churn, error) {
	return Churn{}, ErrCGODisabled
}

func (s *Store) ListCoChangeForFile(_ context.Context, _ string, _ int) ([]CoChange, error) {
	return nil, ErrCGODisabled
}

func (s *Store) UpsertADRLink(_ context.Context, _ ADRLink) error { return ErrCGODisabled }

func (s *Store) SetADRLinkStale(_ context.Context, _, _ string, _ LinkKind, _ bool) error {
	return ErrCGODisabled
}

func (s *Store) ListADRLinksForNode(_ context.Context, _ string) ([]ADRLink, error) {
	return nil, ErrCGODisabled
}

func (s *Store) UpsertLoreTrailer(_ context.Context, _ LoreTrailer) error { return ErrCGODisabled }

func (s *Store) ListLoreTrailersForNode(_ context.Context, _ string) ([]LoreTrailer, error) {
	return nil, ErrCGODisabled
}

func (s *Store) GetNodeByPosition(_ context.Context, _ string, _ int) (string, bool, error) {
	return "", false, ErrCGODisabled
}

func (s *Store) DeleteNodesByFile(_ context.Context, _ string) (int, error) {
	return 0, ErrCGODisabled
}
