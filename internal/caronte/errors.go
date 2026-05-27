// SPDX-License-Identifier: MIT
// Package caronte is the L6 surface of the Caronte code-graph engine: the
// top-level Engine that assembles the six prior layers (store → parser →
// semantic → structure → evolution → intent) into a per-project,
// query-answering engine and satisfies research.GitnexusClient as a drop-in
// for the retired gitnexus subprocess (invariant).
//
// Boundary (invariant/230): this package and its subpackages NEVER import
// internal/store, internal/daemon/*, or internal/orchestrator. The daemon
// composition root (cmd/hades-ctld) injects the real implementations of
// the narrow seams the Engine declares in its Deps (the per-project DB opener
// from caronteadapter, the dispatcher *orchestrator.Orchestrator, the Jina
// embedder + BGE reranker, the audit emitter, the doctrine accessors). The
// engine references the seam types from their OWNING caronte subpackages
// (semantic.CaronteDispatcher, intent.CodeEmbedder, intent.Reranker) — it does
// NOT re-declare them (DECISION 7).
//
// Drop-in (invariant): *Engine satisfies research.GitnexusClient
// (CodeGraph + Close); the compile anchor in engine.go binds it. The daemon
// injects the engine where the gitnexus child went and refuses to boot
// (os.Exit(1)) if the engine cannot construct (bootstrap-required, generalizes
// invariant).
package caronte

import "errors"

var ErrEngineClosed = errors.New("caronte: engine closed")

var ErrProjectUnavailable = errors.New("caronte: project unavailable")

var ErrDegraded = errors.New("caronte: degraded result")

var ErrFederationUnavailable = errors.New("caronte: federation db unavailable")
