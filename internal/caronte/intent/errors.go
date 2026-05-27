// SPDX-License-Identifier: MIT
// Package intent is Caronte's L5 get_why layer (spec §10, decision D2
// maximum). It answers "why does this code exist?" by merging three intent
// sources behind GetWhy(symbol|file): (1) ADR-linking (explicit references +
// a caronte-intent.toml coverage manifest + semantic similarity), (2)
// staleness (a linked ADR is flagged when its linked code's content_hash
// changes after the ADR's last git-touch — invariant), and (3) semantic
// correlation (Jina-code 1536-d embeddings of ADR/spec chunks + code-node
// text, KNN-retrieved from code_node_vec and BGE-reranked). GetWhy also
// surfaces Lore git-trailers (populated by ; read here via
// store.ListLoreTrailersForNode).
//
// Boundary: this package and its callers never import
// internal/store; it operates over the *store.Store (whose *sql.DB
// is opened only by internal/daemon/caronteadapter). It declares three
// narrow seams — CodeEmbedder (master C-6), Reranker, GitProber — and the
// daemon wires the real embedder/reranker + an os/exec git prober at
// the composition root; tests inject deterministic fakes.
//
// invariant: the embed path makes NO web calls. The CodeEmbedder interface
// has no Forward/HTTP method — the real impl (ecosystem.JinaCodeEmbeddings)
// is a local stdin/stdout MPS subprocess. Embeddings do NOT route through the
// dispatcher.
//
// CGO the semantic path calls store.UpsertNodeVector + store.KNNNodeIDs
// (sqlite-vec, CGO-only), so adrlink.go/staleness.go/semantic.go/getwhy.go
// are //go:build cgo with a !cgo degraded stub (intent_nocgo.go). The value
// types + seams (this file, types.go, manifest.go) are CGO-agnostic.
package intent

import "errors"

var ErrCGODisabled = errors.New("caronte/intent: semantic path requires CGO_ENABLED=1; degraded_mode active")

var ErrEmptyStore = errors.New("caronte/intent: nil *store.Store injected")

var ErrNoEmbedder = errors.New("caronte/intent: nil CodeEmbedder injected")
