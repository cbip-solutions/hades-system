// SPDX-License-Identifier: MIT
// Package parser is the Caronte L1 layer: it turns source files into
// store.Node rows via tree-sitter (smacker binding, CGO) and per-language
// tags.scm queries, then writes them through Phase A's store. It supports
// incremental re-parse (Tree.Edit), error-tolerant extraction (ERROR/MISSING
// nodes do not break the surrounding symbols), an LRU live-tree cache, an
// XXH64 content-hash skip, and a file-save watcher (debounced, CPU-budgeted).
//
// Boundary (inv-zen-230, inherited from Phase A): this package and all of
// internal/caronte NEVER import internal/store. It depends only on
// internal/caronte/store (the Caronte per-project store), the standard
// library, and the vendored tree-sitter + xxhash libs. The DB *sql.DB is
// opened by caronteadapter and injected into store.Store; the parser receives
// a *store.Store, never a path.
//
// CGO the parse core (parser.go, lang_go.go, incremental.go, indexer.go) is
// //go:build cgo because the smacker tree-sitter binding and Phase A's store
// are CGO. The !cgo build variant (parser_nocgo.go, indexer_nocgo.go) returns
// ErrCGODisabled so the daemon cross-compiles (GOOS=linux CGO_ENABLED=0) and
// degrades gracefully. The hash, query-embed, doctrine-seam, errors, and
// watcher files are CGO-agnostic.
package parser

import (
	"fmt"

	"github.com/cespare/xxhash/v2"
)

func ContentHash(s string) string {
	return fmt.Sprintf("%016x", xxhash.Sum64String(s))
}
