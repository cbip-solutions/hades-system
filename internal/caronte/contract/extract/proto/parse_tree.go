// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package proto

import (
	"context"
	"runtime"
	"sync"
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
)

// parsedSources tracks the source bytes a particular *parser.Tree was parsed
// from so the C-4 Endpoints(tree, file) entry can re-derive content for
// `node.Content` calls — smacker trees do not carry source bytes; the API
// surface that takes (tree, file) needs a side-channel to recover the source.
//
// The map is keyed by the tree pointer cast to uintptr (NOT the *parser.Tree
// itself) so the map entry holds NO Go-reachable pointer to the tree — that
// way the runtime.AddCleanup hook attached at parseTree time can fire when
// the tree becomes unreachable. (A direct `map[*parser.Tree][]byte` would
// keep the tree reachable via the map key, defeating the cleanup.)
//
// Lifetime parseTree inserts the (uintptr(tree) → source) pair and attaches
// a runtime.AddCleanup hook to the tree that deletes the entry once the tree
// is unreachable. Callers SHOULD still defer tree.Close() to release the
// underlying tree-sitter resources promptly; the cleanup is the safety net
// for the map entry (Close() on *sitter.Tree does NOT call back into Go, so
// it cannot trigger deregistration on its own). We use AddCleanup (Go 1.24+)
// instead of SetFinalizer because smacker's *sitter.Tree owns a cache map of
// *Node values that hold a back-pointer to the *Tree — a finalizer on a
// member of a reference cycle is documented not to run, whereas AddCleanup
// has no such restriction.
//
// Production callers SHOULD use EndpointsFromBytes — it carries source as a
// parameter and never consults this map. The map exists only so the registry-
// driven Resolve+Endpoints test path (and any future caller that wants a
// tree-only C-4 entry) keeps working.
var parsedSources struct {
	sync.RWMutex
	m map[uintptr][]byte
}

func init() {
	parsedSources.m = make(map[uintptr][]byte)
}

func (e *Extractor) parseTree(ctx context.Context, content []byte) (*parser.Tree, error) {
	p := sitter.NewParser()
	defer p.Close()
	p.SetLanguage(e.lang)
	t, err := p.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}

	key := uintptr(unsafe.Pointer(t))
	parsedSources.Lock()
	parsedSources.m[key] = content
	parsedSources.Unlock()

	runtime.AddCleanup(t, func(deadKey uintptr) {
		parsedSources.Lock()
		delete(parsedSources.m, deadKey)
		parsedSources.Unlock()
	}, key)
	return t, nil
}

func lookupParsedSource(tree *parser.Tree) ([]byte, bool) {
	key := uintptr(unsafe.Pointer(tree))
	parsedSources.RLock()
	defer parsedSources.RUnlock()
	b, ok := parsedSources.m[key]
	return b, ok
}
