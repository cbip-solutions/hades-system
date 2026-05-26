// SPDX-License-Identifier: MIT
// Package daemon — server_p9_adapter.go (Plan 9 Phase H Task H-10).
//
// plan9Adapters is the dependency bundle every Phase H handler consumes.
// SetPlan9Adapters wires the bundle once at daemon boot; nil bundle
// means Plan 9 features are disabled (each route returns 503 with
// stable error code "plan9_<group>_unavailable").
//
// Pattern matches:
//   - Plan 4 N: SetPlan4Adapters (workforce + budget + audit_emit)
//   - Plan 5 N: SetPlan5OrchestratorService (orchestrator engine)
//   - Plan 7 I: SetPlan7Adapters (projectctx + quota + tmuxlife + ...)
//
// inv-zen-031: this file imports internal/daemon/handlers only; it never
// imports internal/audit/*, internal/knowledge/*, internal/adr,
// internal/research/cache, or internal/state/manifest directly. All
// substrate calls flow through the handler-package Ctx interfaces.
package daemon

import (
	"net/http"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type Plan9Adapters struct {
	Audit handlers.AuditCtxP9

	Knowledge handlers.KnowledgeAdapterP9

	ADR handlers.ADRCtx

	Research handlers.ResearchStoreP9

	State handlers.StateService
}

type plan9Adapters = Plan9Adapters

func (s *Server) SetPlan9Adapters(p *plan9Adapters) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plan9 = p
}

func (s *Server) plan9Read() *plan9Adapters {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan9
}

func (s *Server) registerPlan9Routes() {

	rl := func(endpoint string, h http.Handler) http.Handler {
		return handlers.RateLimitMiddleware(s, s.bucketRegistry, endpoint, h)
	}

	s.mux.Handle("POST /v1/audit-chain/verify-chain",
		rl("audit_p9_verify_chain", auditP9Wrapped(s, handlers.AuditP9VerifyChain)))
	s.mux.Handle("GET /v1/audit-chain/history",
		rl("audit_p9_history", auditP9Wrapped(s, handlers.AuditP9History)))
	s.mux.Handle("POST /v1/audit-chain/recover",
		rl("audit_p9_recover", auditP9Wrapped(s, handlers.AuditP9Recover)))
	s.mux.Handle("GET /v1/audit-chain/partition-seals",
		rl("audit_p9_partition_seals", auditP9Wrapped(s, handlers.AuditP9PartitionSeals)))
	s.mux.Handle("POST /v1/audit-chain/checkpoint",
		rl("audit_p9_checkpoint", auditP9Wrapped(s, handlers.AuditP9Checkpoint)))
	s.mux.Handle("GET /v1/audit-chain/cold-archive/list",
		rl("audit_p9_cold_list", auditP9Wrapped(s, handlers.AuditP9ColdArchiveList)))
	s.mux.Handle("POST /v1/audit-chain/cold-archive/restore",
		rl("audit_p9_cold_restore", auditP9Wrapped(s, handlers.AuditP9ColdArchiveRestore)))
	s.mux.Handle("POST /v1/audit-chain/witness/rotate",
		rl("audit_p9_witness_rotate", auditP9Wrapped(s, handlers.AuditP9WitnessRotate)))
	s.mux.Handle("GET /v1/audit-chain/witness/pubkey",
		rl("audit_p9_witness_pubkey", auditP9Wrapped(s, handlers.AuditP9WitnessPubkey)))
	s.mux.Handle("POST /v1/audit-chain/configure-s3",
		rl("audit_p9_configure_s3", auditP9Wrapped(s, handlers.AuditP9ConfigureS3)))

	s.mux.Handle("GET /v1/knowledge/query",
		rl("knowledge_p9_query", knowledgeP9Wrapped(s, handlers.KnowledgeP9Query)))
	s.mux.Handle("POST /v1/knowledge/promote",
		rl("knowledge_p9_promote", knowledgeP9Wrapped(s, handlers.KnowledgeP9Promote)))
	s.mux.Handle("POST /v1/knowledge/unpromote",
		rl("knowledge_p9_unpromote", knowledgeP9Wrapped(s, handlers.KnowledgeP9Unpromote)))
	s.mux.Handle("GET /v1/knowledge/list",
		rl("knowledge_p9_list", knowledgeP9Wrapped(s, handlers.KnowledgeP9List)))
	s.mux.Handle("POST /v1/knowledge/rebuild",
		rl("knowledge_p9_rebuild", knowledgeP9Wrapped(s, handlers.KnowledgeP9Rebuild)))

	s.mux.Handle("POST /v1/adr/propose",
		rl("adr_propose", adrWrapped(s, handlers.ADRPropose)))
	s.mux.Handle("GET /v1/adr/show",
		rl("adr_show", adrWrapped(s, handlers.ADRShow)))
	s.mux.Handle("GET /v1/adr/list",
		rl("adr_list", adrWrapped(s, handlers.ADRList)))
	s.mux.Handle("GET /v1/adr/graph",
		rl("adr_graph", adrWrapped(s, handlers.ADRGraphHandler)))
	s.mux.Handle("GET /v1/adr/history",
		rl("adr_history", adrWrapped(s, handlers.ADRHistoryHandler)))
	s.mux.Handle("POST /v1/adr/accept",
		rl("adr_accept", adrWrapped(s, handlers.ADRAccept)))
	s.mux.Handle("POST /v1/adr/reject",
		rl("adr_reject", adrWrapped(s, handlers.ADRReject)))
	s.mux.Handle("POST /v1/adr/supersede",
		rl("adr_supersede", adrWrapped(s, handlers.ADRSupersede)))
	s.mux.Handle("POST /v1/adr/index",
		rl("adr_index", adrWrapped(s, handlers.ADRIndex)))

	s.mux.Handle("GET /v1/research/history",
		rl("research_p9_history", researchP9Wrapped(s, handlers.ResearchP9History)))
	s.mux.Handle("GET /v1/research/cache/stats",
		rl("research_p9_cache_stats", researchP9Wrapped(s, handlers.ResearchP9CacheStats)))
	s.mux.Handle("POST /v1/research/cache/invalidate",
		rl("research_p9_cache_invalidate", researchP9Wrapped(s, handlers.ResearchP9CacheInvalidate)))
	s.mux.Handle("GET /v1/research/cache/list",
		rl("research_p9_cache_list", researchP9Wrapped(s, handlers.ResearchP9CacheList)))

	s.mux.Handle("GET /v1/state/show",
		rl("state_show", stateWrapped(s, handlers.StateShow)))
	s.mux.Handle("POST /v1/state/regenerate",
		rl("state_regenerate", stateWrapped(s, handlers.StateRegenerate)))
	s.mux.Handle("POST /v1/state/verify",
		rl("state_verify", stateWrapped(s, handlers.StateVerify)))
	s.mux.Handle("POST /v1/state/pin",
		rl("state_pin", stateWrapped(s, handlers.StatePin)))
	s.mux.Handle("GET /v1/state/history",
		rl("state_history", stateWrapped(s, handlers.StateHistory)))
}

func auditP9Wrapped(s *Server, fn func(handlers.AuditCtxP9) http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.plan9Read()
		if p == nil || p.Audit == nil {
			http.Error(w,
				`{"error":"audit substrate not configured","code":"plan9_audit_unavailable"}`,
				http.StatusServiceUnavailable)
			return
		}
		fn(p.Audit).ServeHTTP(w, r)
	})
}

func knowledgeP9Wrapped(s *Server, fn func(handlers.KnowledgeAdapterP9) http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.plan9Read()
		if p == nil || p.Knowledge == nil {
			http.Error(w,
				`{"error":"knowledge substrate not configured","code":"plan9_knowledge_unavailable"}`,
				http.StatusServiceUnavailable)
			return
		}
		fn(p.Knowledge).ServeHTTP(w, r)
	})
}

func adrWrapped(s *Server, fn func(handlers.ADRCtx) http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.plan9Read()
		if p == nil || p.ADR == nil {
			http.Error(w,
				`{"error":"adr substrate not configured","code":"plan9_adr_unavailable"}`,
				http.StatusServiceUnavailable)
			return
		}
		fn(p.ADR).ServeHTTP(w, r)
	})
}

func researchP9Wrapped(s *Server, fn func(handlers.ResearchStoreP9) http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.plan9Read()
		if p == nil || p.Research == nil {
			http.Error(w,
				`{"error":"research substrate not configured","code":"plan9_research_unavailable"}`,
				http.StatusServiceUnavailable)
			return
		}
		fn(p.Research).ServeHTTP(w, r)
	})
}

func stateWrapped(s *Server, fn func(handlers.StateService) http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := s.plan9Read()
		if p == nil || p.State == nil {
			http.Error(w,
				`{"error":"state substrate not configured","code":"plan9_state_unavailable"}`,
				http.StatusServiceUnavailable)
			return
		}
		fn(p.State).ServeHTTP(w, r)
	})
}
