// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package parser

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type IndexReport struct {
	Written int
	Skipped int
	Partial bool
}

type Indexer struct {
	parser *Parser
	store  *store.Store
}

func NewIndexer(p *Parser, s *store.Store) *Indexer {
	return &Indexer{parser: p, store: s}
}

func (ix *Indexer) IndexFile(ctx context.Context, filePath string, src []byte) (IndexReport, error) {
	res, err := ix.parser.ParseFile(ctx, filePath, src)
	if err != nil {
		return IndexReport{}, fmt.Errorf("caronte/parser: index %s: %w", filePath, err)
	}
	return ix.writeNodes(ctx, res)
}

func (ix *Indexer) ReindexIncremental(ctx context.Context, filePath string, oldSrc, newSrc []byte) (IndexReport, error) {
	res, err := ix.parser.ParseFileIncremental(ctx, filePath, oldSrc, newSrc)
	if err != nil {
		return IndexReport{}, fmt.Errorf("caronte/parser: reindex %s: %w", filePath, err)
	}
	return ix.writeNodes(ctx, res)
}

func (ix *Indexer) writeNodes(ctx context.Context, res *ParseResult) (IndexReport, error) {
	rep := IndexReport{Partial: res.Partial}
	for _, n := range res.Nodes {
		prior, err := ix.store.ContentHashFor(ctx, n.NodeID)
		switch {
		case errors.Is(err, store.ErrNotFound):

		case err != nil:
			return rep, fmt.Errorf("caronte/parser: content-hash probe %s: %w", n.NodeID, err)
		case prior == n.ContentHash:

			rep.Skipped++
			continue
		}
		if err := ix.store.UpsertNode(ctx, n); err != nil {
			return rep, fmt.Errorf("caronte/parser: upsert %s: %w", n.NodeID, err)
		}
		rep.Written++
	}
	return rep, nil
}

func (ix *Indexer) DropTree(filePath string) {
	ix.parser.cache().drop(filePath)
}

func (ix *Indexer) Delete(ctx context.Context, filePath string) (int, error) {
	n, err := ix.store.DeleteNodesByFile(ctx, filePath)
	if err != nil {
		return 0, fmt.Errorf("caronte/parser: Indexer.Delete %s: %w", filePath, err)
	}
	ix.DropTree(filePath)
	return n, nil
}

func (ix *Indexer) Close() { ix.parser.CloseTrees() }

type IndexerSink interface {
	Reindex(path string) error
	Delete(path string) error
}

type fileReader func(path string) ([]byte, error)

type indexerSink struct {
	ix   *Indexer
	read fileReader
}

func NewIndexerSink(p *Parser, s *store.Store, read fileReader) IndexerSink {
	return &indexerSink{ix: NewIndexer(p, s), read: read}
}

func (s *indexerSink) Reindex(path string) error {
	src, err := s.read(path)
	if err != nil {
		return fmt.Errorf("caronte/parser: sink read %s: %w", path, err)
	}
	_, err = s.ix.ReindexIncremental(context.Background(), path, nil, src)
	return err
}

func (s *indexerSink) Delete(path string) error {
	_, err := s.ix.Delete(context.Background(), path)
	return err
}
