//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type IndexReport struct {
	Written int
	Skipped int
	Partial bool
}

type Indexer struct{}

func NewIndexer(_ *Parser, _ *store.Store) *Indexer { return &Indexer{} }

func (ix *Indexer) IndexFile(_ context.Context, _ string, _ []byte) (IndexReport, error) {
	return IndexReport{}, ErrCGODisabled
}

func (ix *Indexer) ReindexIncremental(_ context.Context, _ string, _, _ []byte) (IndexReport, error) {
	return IndexReport{}, ErrCGODisabled
}

func (ix *Indexer) DropTree(_ string) {}

func (ix *Indexer) Delete(_ context.Context, _ string) (int, error) {
	return 0, ErrCGODisabled
}

func (ix *Indexer) Close() {}

type IndexerSink interface {
	Reindex(path string) error
	Delete(path string) error
}

type fileReader func(path string) ([]byte, error)

type degradedSink struct{}

func NewIndexerSink(_ *Parser, _ *store.Store, _ fileReader) IndexerSink { return &degradedSink{} }

func (degradedSink) Reindex(_ string) error { return ErrCGODisabled }
func (degradedSink) Delete(_ string) error  { return nil }
