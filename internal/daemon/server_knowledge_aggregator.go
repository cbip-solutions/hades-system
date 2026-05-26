// SPDX-License-Identifier: MIT
// Package daemon — server_knowledge_aggregator.go (Plan 9 Phase D-12).
//
// This file exposes two server-level methods for knowledge aggregator wiring:
//
//  1. NewAdapterForKnowledge — returns a *knowledgeadapter.Adapter backed
//     by the daemon's *sql.DB (s.store.DB()). Used by cmd/zen-swarm-ctld
//     main.go to wire the aggregator.PerProjectKnowledgeStore seam.
//
//  2. RegisterKnowledgeAggregator — mounts the five D-12 HTTP routes on the
//     Server's mux, given a fully-assembled *handlers.KnowledgeAggregatorHandlers.
//
// AggregatorBridge lives in internal/daemon/aggregatorbridge (NOT here) to
// avoid importing internal/knowledge/aggregator into the daemon package. That
// import would bring mattn/go-sqlite3 (CGO) into the daemon test binary
// alongside ncruces/go-sqlite3 (from internal/store), causing a
// double-registration panic on "sqlite3" driver name. Placing the bridge in
// its own sub-package keeps daemon tests clean; cmd/zen-swarm-ctld main.go
// is the binary-level import point where both drivers coexist.
//
// Driver-conflict context: internal/knowledge/aggregator imports
// mattn/go-sqlite3 (CGO) for sqlite-vec. internal/store imports
// ncruces/go-sqlite3 (pure-Go). Both register the "sqlite3" SQL driver.
// The cmd/zen-swarm-ctld binary links both once; only mattn's init() registers
// first, ncruces silently skips its second registration.
//
// Wiring in production (cmd/zen-swarm-ctld main.go):
//
//	adapter   := srv.NewAdapterForKnowledge()
//	agg, _    := aggregator.New(aggregator.Options{DB: aggDB, Embedder: emb, Store: adapter})
//	bridge    := aggregatorbridge.New(agg)
//	srv.RegisterKnowledgeAggregator(&handlers.KnowledgeAggregatorHandlers{Agg: bridge})
//
// Phase J carry-forward: embed_worker registration (SetRebuildChannel) is
// deferred until fsWatcher is available. Until then, AggEnqueueRebuild
// returns ErrAggWorkerNotStarted and the handler degrades to HTTP 202.
//
// inv-zen-031: this file does NOT import internal/knowledge/aggregator.
//
//	The daemon package is inv-zen-031 compliant: it imports internal/store
//	but NOT internal/knowledge/aggregator directly.
package daemon

import (
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/knowledgeadapter"
)

func (s *Server) NewAdapterForKnowledge() *knowledgeadapter.Adapter {
	return knowledgeadapter.NewAdapterFromDB(s.store.DB())
}

// RegisterKnowledgeAggregator mounts the five Plan 9 D-12 aggregator routes:
//
//	POST /v1/knowledge/aggregator/query
//	POST /v1/knowledge/aggregator/promote
//	POST /v1/knowledge/aggregator/unpromote
//	GET  /v1/knowledge/aggregator/list
//	POST /v1/knowledge/aggregator/rebuild
//
// h.Agg MUST be a fully-assembled handlers.AggregatorService implementation.
// The canonical implementation for production is aggregatorbridge.AggregatorBridge
// (see internal/daemon/aggregatorbridge/bridge.go). Tests may substitute any
// struct implementing handlers.AggregatorService.
func (s *Server) RegisterKnowledgeAggregator(h *handlers.KnowledgeAggregatorHandlers) {
	handlers.RegisterKnowledgeAggregatorRoutes(s.mux, h)
}
