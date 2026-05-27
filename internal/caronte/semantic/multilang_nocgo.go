//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type MultiLangMode string

const (
	ModeSCIP MultiLangMode = "scip"

	ModeHeuristic MultiLangMode = "heuristic"
)

type MultiLangStats struct {
	Language       string
	Mode           MultiLangMode
	SCIPEdges      int
	HeuristicEdges int
	LLMHintEdges   int
	Unresolved     int
}

type MultiLangOpts struct {
	MaxTailSites  int
	EnableLLMTail bool
}

type MultiLangResolver struct{}

func NewMultiLangResolver(_ *store.Store, _ SCIPRunner, _ CaronteDispatcher, _ MultiLangOpts) *MultiLangResolver {
	return &MultiLangResolver{}
}

func (r *MultiLangResolver) ResolveLanguage(_ context.Context, _, _, _ string) (MultiLangStats, error) {
	return MultiLangStats{}, ErrCGODisabled
}
