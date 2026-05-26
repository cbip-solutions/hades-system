// SPDX-License-Identifier: MIT
package rag

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Embedder struct {
	Endpoint string
}

func NewEmbedder(endpoint string) *Embedder {
	return &Embedder{Endpoint: endpoint}
}

func (e *Embedder) Embed(text string) ([]float32, error) {
	return nil, zerrors.ErrNotImplementedPlan14
}
