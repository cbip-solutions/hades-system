// SPDX-License-Identifier: MIT
// Package rag implements hybrid retrieval (FTS5 + sqlite-vec) over a
// project's codebase + docs. Per design v1.2 §6.5 and verified R3
// (sqlite-vec via ncruces wasm bindings — no CGO).
package rag

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type IndexConfig struct {
	DBPath     string
	ChunkLines int
	EmbedDim   int
}

type Index struct{}

func Open(cfg IndexConfig) (*Index, error) {
	return nil, zerrors.ErrNotImplementedPlan14
}

func (i *Index) Reindex(projectPath string) error {
	return zerrors.ErrNotImplementedPlan14
}

func (i *Index) Update(changedPaths []string) error {
	return zerrors.ErrNotImplementedPlan14
}

func (i *Index) Close() error {
	return zerrors.ErrNotImplementedPlan14
}
