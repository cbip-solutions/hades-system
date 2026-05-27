// go:build !cgo
//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package store

import "context"

type NodeBM25 struct {
	NodeID string
	Score  float64
}

func (s *Store) LexicalSearchNodeIDs(_ context.Context, _ string, _ int) ([]NodeBM25, error) {
	return nil, ErrCGODisabled
}
