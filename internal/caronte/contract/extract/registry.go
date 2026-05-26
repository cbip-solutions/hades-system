// SPDX-License-Identifier: MIT
package extract

import (
	"fmt"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/caronte/parser"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

// RouteExtractor is the Plan 20 cross-language API-extraction contract (master
// C-4, FROZEN). Each concrete extractor lives in a per-framework subpackage
// under internal/caronte/contract/extract/ and is added to a Registry at
// daemon composition time. The registry's Resolve(file, content) capability-
// detects ALL matching extractors for a file — one file may match multiple
// (e.g., a NestJS controller annotated with @nestjs/swagger decorators matches
// both the structural NestJS extractor and the swagger artifact extractor; the
// Phase F linker merges their endpoints at distinct confidence tiers).
//
// Methods (master C-4):
//   - Language returns the source language this extractor parses (one of the 5
//     constants). The registry uses (Language, framework) tuples as collision
//     keys: at most one extractor per tuple is registered.
//   - Frameworks returns the framework labels this extractor handles (e.g.,
//     {"chi","gin","echo","stdlib"} for a Go-HTTP extractor that covers all
//     four routers). The registry reserves every (Language, framework) tuple
//     in the slice for this extractor; a second Register call that collides
//     on ANY tuple returns ErrDuplicateExtractor.
//   - Detect returns true iff this file (path + content) is in-scope for this
//     extractor. Capability detection is content-aware so a Go file can be
//     routed to chi vs gin via package-import sniff; a TypeScript file can be
//     routed to nextjs vs nestjs via decorator presence.
//   - Endpoints extracts the server-side route definitions from a parsed tree
//     and returns []store.APIEndpoint rows ready for store.UpsertAPIEndpoint
//     (Phase B CRUD). Each row carries ExtractorID populated by the extractor
//     so per-row provenance is traceable.
//   - Calls extracts the client-side call sites from a parsed tree and returns
//     []store.APICall rows. Same provenance discipline as Endpoints.
//   - StubArtifacts returns gRPC client-side generated-stub references (e.g.,
//     the *.pb.go / *.pb.ts package + service + RPC trio) used by the Phase F
//     linker for the highest-confidence exact_proto_import tier. Non-gRPC
//     extractors return an empty slice; this is the contract Phase D's proto
//     extractor consumes and Phase E's typescript extractors return empty.
//
// Boundary inheritance: extractor concrete impls may import the smacker
// tree-sitter binding (CGO) via the *parser.Tree alias — that is correct; they
// MUST NOT import internal/store (boundary inv-zen-230 + inv-zen-271) and MUST
// NOT import internal/caronte/store/federation (Phase A is workspace-scoped;
// extractors are per-repo).
type RouteExtractor interface {
	Language() Language

	Frameworks() []string

	// Detect returns true iff this (file, content) is in-scope for this
	// extractor. Capability-detected per call — the registry invokes Detect
	// on every registered extractor during Resolve and accumulates matches.
	//
	// MUST be pure + non-blocking + non-reentrant w.r.t. the Registry — Resolve
	// snapshots the candidate extractor set under RLock then RELEASES the lock
	// BEFORE invoking Detect on each candidate. Consequence: a blocking or
	// re-entrant Detect serializes the calling goroutine's Resolve, NOT the
	// registry mutex; concurrent Register / Resolve from other goroutines
	// proceeds unimpeded. Detect implementations MUST treat file + content as
	// read-only and MUST NOT call back into the same Registry (the snapshot
	// pattern intentionally does not protect against re-entry — a Detect that
	// re-enters Resolve risks lock-ordering bugs against any future write path).
	Detect(file string, content []byte) bool

	Endpoints(tree *parser.Tree, file string) ([]store.APIEndpoint, error)

	Calls(tree *parser.Tree, file string) ([]store.APICall, error)

	StubArtifacts(file string, content []byte) []StubReference
}

type langFrameworkKey struct {
	lang      Language
	framework string
}

type Registry struct {
	mu      sync.RWMutex
	byName  map[string]RouteExtractor
	byTuple map[langFrameworkKey]string
}

func NewRegistry() *Registry {
	return &Registry{
		byName:  make(map[string]RouteExtractor),
		byTuple: make(map[langFrameworkKey]string),
	}
}

func (r *Registry) Register(name string, e RouteExtractor) error {
	if name == "" {
		return fmt.Errorf("%w: registration name is empty", ErrDuplicateExtractor)
	}
	if e == nil {
		return fmt.Errorf("%w: extractor %q is nil", ErrDuplicateExtractor, name)
	}
	frameworks := e.Frameworks()
	if len(frameworks) == 0 {
		return fmt.Errorf("%w: extractor %q declares zero frameworks (Language=%s)",
			ErrDuplicateExtractor, name, e.Language())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("%w: name %q already registered", ErrDuplicateExtractor, name)
	}
	lang := e.Language()

	for _, fw := range frameworks {
		key := langFrameworkKey{lang: lang, framework: fw}
		if prev, exists := r.byTuple[key]; exists {
			return fmt.Errorf("%w: (Language=%s, framework=%s) already owned by %q",
				ErrDuplicateExtractor, lang, fw, prev)
		}
	}

	r.byName[name] = e
	for _, fw := range frameworks {
		r.byTuple[langFrameworkKey{lang: lang, framework: fw}] = name
	}
	return nil
}

// Resolve returns every registered RouteExtractor whose Detect predicate
// returns true for the given (file, content) pair. The result is an empty
// non-nil slice when no extractor matches (so callers can range without a
// nil-check); ordering is the registration order of byName iteration which is
// unspecified by the Go runtime — callers MUST treat the result as a SET, not
// a sequence (Phase F's linker iterates and dedupes by extractor identity).
//
// One file may match multiple extractors — this is intentional. A NestJS
// controller annotated with @nestjs/swagger decorators matches both the
// structural NestJS extractor and the swagger artifact extractor; the linker
// merges their outputs at distinct confidence tiers (master C-5).
//
// Capability-detect is content-aware: Detect receives the file contents so an
// extractor can sniff package imports, decorator markers, or filesystem cues
// without re-reading the file. Detect must be cheap (the registry calls every
// registered extractor's Detect on every Resolve invocation).
//
// Locking Resolve takes RLock only long enough to snapshot the candidate
// extractor slice; the lock is RELEASED BEFORE the per-candidate Detect calls
// run. This guarantees a blocking, slow, or accidentally re-entrant Detect
// implementation cannot stall concurrent Register / Resolve goroutines on the
// registry mutex — the cost of a misbehaving Detect is paid by the calling
// goroutine only. See the RouteExtractor.Detect doc-comment for the contract
// extractors must uphold.
func (r *Registry) Resolve(file string, content []byte) []RouteExtractor {

	r.mu.RLock()
	candidates := make([]RouteExtractor, 0, len(r.byName))
	for _, e := range r.byName {
		candidates = append(candidates, e)
	}
	r.mu.RUnlock()

	out := make([]RouteExtractor, 0, len(candidates))
	for _, e := range candidates {
		if e.Detect(file, content) {
			out = append(out, e)
		}
	}
	return out
}

var (
	defaultRegistry     *Registry
	defaultRegistryOnce sync.Once
)

func Default() *Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = NewRegistry()
	})
	return defaultRegistry
}
