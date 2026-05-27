//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"errors"
)

// ErrCGORequired is returned from Chunker methods when CGO is disabled.
// Callers MUST handle this gracefully — typically by
// logging + skipping the chunk-and-embed pipeline (the package is then
// stored without chunk-level indexing, retrievable only at coarse package
// granularity).
var ErrCGORequired = errors.New("ecosystem: tree-sitter chunker requires CGO; rebuild with CGO_ENABLED=1")

var ErrUnknownLanguage = errors.New("ecosystem: unknown language (cgo disabled)")

type Language string

const (
	LangGo         Language = "go"
	LangPython     Language = "python"
	LangTypeScript Language = "typescript"
	LangRust       Language = "rust"
	LangMarkdown   Language = "markdown"
)

type Chunker struct{}

type ChunkerOptions struct {
	MinTokens       int
	MaxLeafTokens   int
	MaxParentTokens int
}

func NewChunker(_ ChunkerOptions) (*Chunker, error) {
	return nil, ErrCGORequired
}

func (c *Chunker) Chunk(_ context.Context, _ *PackageDoc) ([]Chunk, error) {
	return nil, ErrCGORequired
}

func (c *Chunker) Close() error {
	return nil
}

func approxTokens(s string) int {
	return (len(s) + 3) / 4
}
