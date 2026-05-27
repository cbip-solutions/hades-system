// go:build !cgo
//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package store

import "context"

type NodeDistance struct {
	NodeID   string
	Distance float64
}

func (s *Store) KNNNodeIDs(_ context.Context, _ []float32, _ int) ([]NodeDistance, error) {
	return nil, ErrCGODisabled
}
