//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package intent

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type ADRLinker struct{}

func NewADRLinker(_ *store.Store, _ string) *ADRLinker { return &ADRLinker{} }

func (*ADRLinker) IndexAndLink(_ context.Context) error { return ErrCGODisabled }

type StalenessChecker struct{}

func NewStalenessChecker(_ *store.Store, _ string, _ GitProber) *StalenessChecker {
	return &StalenessChecker{}
}

func (*StalenessChecker) Recompute(_ context.Context) error { return ErrCGODisabled }

type SemanticIndexer struct{}

func NewSemanticIndexer(_ *store.Store, _ CodeEmbedder, _ Reranker, _ IntentParams) (*SemanticIndexer, error) {
	return nil, ErrCGODisabled
}

type Engine struct{}

func NewEngine(_ *store.Store, _ *SemanticIndexer, _ map[string]string) *Engine { return &Engine{} }

func (*Engine) GetWhy(_ context.Context, subject string) (WhyAnswer, error) {
	return WhyAnswer{Subject: subject, Degraded: true}, nil
}
