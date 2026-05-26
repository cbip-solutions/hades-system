// SPDX-License-Identifier: MIT
package rag

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type Hit struct {
	Path    string
	Content string
	Score   float32
	Mode    string
}

type Mode string

const (
	ModeFTS Mode = "fts"

	ModeSemantic Mode = "semantic"

	ModeHybrid Mode = "hybrid"
)

func (i *Index) Search(query string, mode Mode, limit int) ([]Hit, error) {
	return nil, zerrors.ErrNotImplementedPlan14
}
